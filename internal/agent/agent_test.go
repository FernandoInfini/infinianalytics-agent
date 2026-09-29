package agent

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rene-roid/kanshi/internal/dockerstats"
)

func TestContainerKeys(t *testing.T) {
	list := []dockerstats.Container{
		{Name: "api", FullName: "infini-api-1", Project: "infini"},
		{Name: "worker", FullName: "infini-worker-1", Project: "infini"},
		{Name: "worker", FullName: "infini-worker-2", Project: "infini"},
		{Name: "loose", FullName: "loose"},
	}
	got := containerKeys(list)
	want := []string{"infini/api", "infini/infini-worker-1", "infini/infini-worker-2", "loose"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPickKeepsBusiestRunningThenStopped(t *testing.T) {
	list := []dockerstats.Container{
		{FullName: "idle", State: "running", CPU: 1},
		{FullName: "dead", State: "exited"},
		{FullName: "hot", State: "running", CPU: 90},
		{FullName: "fat", State: "running", CPU: 5, MemPercent: 70},
	}
	keys := containerKeys(list)
	got, gotKeys := pick(list, keys, 3)
	if len(got) != 3 || gotKeys[0] != "hot" || gotKeys[1] != "fat" || gotKeys[2] != "idle" {
		t.Fatalf("picked %v", gotKeys)
	}
	_, all := pick(list, keys, 10)
	if all[3] != "dead" {
		t.Fatalf("stopped containers fill the room left: %v", all)
	}
}

func TestParseStopReason(t *testing.T) {
	cases := map[string]string{
		"":                                  StopService,
		"123 myapp.service stop running\n":  StopService,
		"1 reboot.target start waiting\n":   StopReboot,
		"7 kexec.target start waiting\n":    StopReboot,
		"2 poweroff.target start waiting\n": StopShutdown,
		"3 halt.target start waiting\n":     StopShutdown,
		"9 shutdown.target start waiting\n1 reboot.target start waiting": StopReboot,
		"4 reboot.target stop waiting\n":                                 StopService,
	}
	for in, want := range cases {
		if got := ParseStopReason(in); got != want {
			t.Errorf("ParseStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBackoffDoublesToTheCap(t *testing.T) {
	b := newBackoff(10*time.Second, time.Minute)
	b.jitter = func() float64 { return 0.5 } // no jitter
	var got []time.Duration
	for range 5 {
		got = append(got, b.next())
	}
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("backoff = %v, want %v", got, want)
		}
	}
	b.reset()
	if d := b.next(); d != 10*time.Second {
		t.Fatalf("after reset = %v", d)
	}
}

func TestClassify(t *testing.T) {
	cases := map[int]Outcome{
		200: OutcomeOK, 0: OutcomeRetry, 500: OutcomeRetry, 503: OutcomeRetry, 429: OutcomeRetry,
		408: OutcomeRetry, 401: OutcomeRevoked, 410: OutcomeRevoked, 413: OutcomeTooLarge,
		400: OutcomeDrop, 422: OutcomeDrop, 404: OutcomeDrop,
	}
	for status, want := range cases {
		if got := Classify(status); got != want {
			t.Errorf("Classify(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestPushSendsGzipWithKeyAndParsesTheAnswer(t *testing.T) {
	var got Batch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/servers/metrics/" || r.Header.Get("X-Agent-Key") != "iak_k" ||
			r.Header.Get("Content-Encoding") != "gzip" {
			http.Error(w, "bad request shape", 400)
			return
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewDecoder(zr).Decode(&got)
		w.Write([]byte(`{"accepted_seq": 7, "next_push_s": 15}`))
	}))
	defer srv.Close()

	p := NewPusher(srv.URL+"/", "iak_k", "test")
	resp, outcome, err := p.Push(context.Background(), Batch{SchemaVersion: 1, Seq: 7, ServerID: "s", Samples: []HostSample{{N: 5}}})
	if err != nil || outcome != OutcomeOK {
		t.Fatalf("push = %v, %v", outcome, err)
	}
	if resp.NextPushS != 15 || got.Seq != 7 || len(got.Samples) != 1 {
		t.Fatalf("resp = %+v, got = %+v", resp, got)
	}
}

func TestPushSurfacesTheBackendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		w.Write([]byte(`{"detail": "This server was deleted from the dashboard.", "code": "server_deleted"}`))
	}))
	defer srv.Close()
	_, outcome, err := NewPusher(srv.URL, "k", "test").Push(context.Background(), Batch{})
	if outcome != OutcomeRevoked || err == nil || err.Error() != "HTTP 410: server_deleted: This server was deleted from the dashboard." {
		t.Fatalf("outcome = %v, err = %v", outcome, err)
	}
}

func TestDockerEventTranslation(t *testing.T) {
	d := &dockerEvents{images: map[string]string{"shop/web": "web:1"}}
	msg := func(action string, attrs map[string]string) dockerMessage {
		m := dockerMessage{Type: "container", Action: action, TimeNano: 1_700_000_000_000_000_000}
		m.Actor.Attributes = attrs
		return m
	}
	labels := func(extra map[string]string) map[string]string {
		out := map[string]string{"name": "shop-web-1", "com.docker.compose.project": "shop", "com.docker.compose.service": "web"}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	ev, ok := d.translate(msg("die", labels(map[string]string{"exitCode": "137", "image": "web:1"})))
	if !ok || ev.Type != EventContainerDie || ev.Data["exit_code"] != 137 || ev.Data["key"] != "shop/web" {
		t.Fatalf("die = %+v", ev)
	}
	if ev, _ := d.translate(msg("health_status: unhealthy", labels(nil))); ev.Data["status"] != "unhealthy" {
		t.Fatalf("health = %+v", ev)
	}
	// Same image: a plain start. New image: a deploy marker.
	if ev, _ := d.translate(msg("start", labels(map[string]string{"image": "web:1"}))); ev.Type != EventContainerStart {
		t.Fatalf("start = %+v", ev)
	}
	ev, _ = d.translate(msg("start", labels(map[string]string{"image": "web:2"})))
	if ev.Type != EventContainerImage || ev.Data["from"] != "web:1" || ev.Data["to"] != "web:2" {
		t.Fatalf("deploy = %+v", ev)
	}
	if _, ok := d.translate(msg("exec_start", labels(nil))); ok {
		t.Fatal("exec events are not lifecycle events")
	}
}

func TestBuildBatchMergesRecordsAndCarriesHostUntilDelivered(t *testing.T) {
	a := &Agent{version: "1.0", bootID: "b", host: HostInfo{Hostname: "h"}, cores: []float64{1, 2}}
	a.cfg.ServerID = "srv"
	recs := []Record{
		{Seq: 3, Samples: []HostSample{{N: 5}}, Events: []Event{{Type: EventBoot}}},
		{Seq: 4, Samples: []HostSample{{N: 5}}, Containers: []ContainerRow{{Key: "k"}}},
	}
	b := a.buildBatch(recs)
	if b.Seq != 4 || len(b.Samples) != 2 || len(b.Events) != 1 || len(b.Containers) != 1 || b.BootID != "b" {
		t.Fatalf("batch = %+v", b)
	}
	if b.Host == nil {
		t.Fatal("first batch carries the host identity")
	}
	// Not delivered yet: the next batch still carries it.
	if a.buildBatch(recs).Host == nil {
		t.Fatal("undelivered host identity must be sent again")
	}
	a.markHostSent(b)
	if a.buildBatch(recs).Host != nil {
		t.Fatal("delivered and unchanged: no host identity")
	}
	a.host.Hostname = "renamed"
	if a.buildBatch(recs).Host == nil {
		t.Fatal("a change is sent right away")
	}
}
