#!/usr/bin/env bash
# Builds the agent and a fake ingestion API (scripts/fakeingest), enrolls,
# runs the agent and checks, end to end:
#   1. a batch arrives about every 10 s;
#   2. while the backend answers 503 the windows pile up in the spool;
#   3. once it is back the spool drains and the history has no hole;
#   4. (Linux) a SIGTERM delivers a `stopping` event.
# Runs on Linux and on Windows (Git Bash) - CI is the only place the Windows
# build actually gets to run.
set -euo pipefail

port=${AGENT_SMOKE_PORT:-8199}
ext=
[ "${RUNNER_OS:-}" = Windows ] || [ "${OS:-}" = Windows_NT ] && ext=.exe
tmp=${RUNNER_TEMP:-$(mktemp -d)}/agent-smoke
rm -rf "$tmp" && mkdir -p "$tmp/ingest" "$tmp/agent"
agent=./agent-smoke$ext
fake=./fakeingest-smoke$ext

go build -o "$agent" .
go build -o "$fake" ./scripts/fakeingest

"$fake" -addr "127.0.0.1:$port" -dir "$tmp/ingest" > fake.log 2>&1 &
fake_pid=$!
agent_pid=
trap 'kill $fake_pid ${agent_pid:-} 2>/dev/null || true; echo "--- agent output"; cat agent.log 2>/dev/null || true' EXIT
sleep 1

cfg="$tmp/agent/agent.env"
export IA_AGENT_MACHINE_ID=smoke-machine IA_AGENT_DOCKER=false
"$agent" enroll SMOKE-CODE-0001 --url "http://127.0.0.1:$port" --config "$cfg"
grep -q '^IA_AGENT_KEY=iak_smoke$' "$cfg"

"$agent" run --config "$cfg" > agent.log 2>&1 &
agent_pid=$!

count() { [ -f "$1" ] && wc -l < "$1" | tr -d ' ' || echo 0; }

echo "--- 1. pushes every ~10 s"
sleep 35
batches=$(count "$tmp/ingest/batches.log")
echo "batches after 35 s: $batches"
[ "$batches" -ge 3 ] && [ "$batches" -le 5 ]

echo "--- 2. backend down: the spool grows"
touch "$tmp/ingest/down"
sleep 25
"$agent" status --config "$cfg" | tee status.txt
grep -Eq 'spool: +[1-9][0-9]* window' status.txt

echo "--- 3. backend back: the spool drains, no hole"
rm "$tmp/ingest/down"
# Drained = pushing ok with at most the window that just closed still queued:
# a new one lands every 10 s, so an empty spool is only a moment between pushes.
drained() { grep -q 'last push:.*(ok' status.txt && grep -Eq 'spool: +[01] window' status.txt; }
for _ in $(seq 1 40); do
  "$agent" status --config "$cfg" > status.txt
  drained && break
  sleep 1
done
cat status.txt
drained
sort -u "$tmp/ingest/samples.log" > samples.sorted
first=$(head -1 samples.sorted) last=$(tail -1 samples.sorted) n=$(count samples.sorted)
span=$(( ($(date -u -d "$last" +%s) - $(date -u -d "$first" +%s)) / 10 + 1 ))
echo "windows received: $n, span: $span"
[ "$n" -ge "$span" ]

if [ -z "$ext" ]; then
  echo "--- 4. SIGTERM reports why it stopped"
  kill -TERM "$agent_pid"
  wait "$agent_pid" || true
  agent_pid=
  grep -q '^stopping$' "$tmp/ingest/events.log"
fi
echo "smoke test passed"
