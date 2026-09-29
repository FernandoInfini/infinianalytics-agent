package agent

import (
	"bufio"
	"strings"
)

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
