package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/rene-roid/kanshi/internal/statefile"
)

const stopReasonFile = "stop_reason"

// RecordStopReason is the systemd unit's ExecStop, run as root just before
// the agent gets SIGTERM. The agent runs as a throwaway user whose systemctl
// goes through D-Bus, often already gone by the time a reboot stops services;
// root's talks to systemd directly. So ask here, and leave the answer in dir
// for the agent to pick up - in a file the agent owns, like the rest of dir.
func RecordStopReason(dir string) (string, error) {
	reason := detectStopReason()
	return reason, statefile.Write(filepath.Join(dir, stopReasonFile), []byte(reason+"\n"), 0o600)
}

// takeRecordedStopReason returns and removes what RecordStopReason left in
// dir, "" if nothing.
func takeRecordedStopReason(dir string) string {
	path := filepath.Join(dir, stopReasonFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	return strings.TrimSpace(string(raw))
}

// ParseStopReason reads `systemctl list-jobs --no-legend` as captured while
// the agent is being stopped. A reboot or power-off queues its target as a
// job before services are stopped, so the job list says why this stop is
// happening:
//
//	1 reboot.target   start waiting
//	2 poweroff.target start waiting
//
// Anything else - no target job at all - is someone stopping the service.
func ParseStopReason(jobs string) string {
	sc := bufio.NewScanner(strings.NewReader(jobs))
	reason := StopService
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		unit, kind := fields[1], fields[2]
		if kind != "start" {
			continue
		}
		switch unit {
		case "reboot.target", "kexec.target", "soft-reboot.target":
			return StopReboot
		case "poweroff.target", "halt.target":
			reason = StopShutdown
		}
	}
	return reason
}
