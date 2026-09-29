package agent

import (
	"context"
	"os/exec"
	"time"
)

// detectStopReason asks systemd why we are being stopped. Without systemd (or
// if it does not answer in time) this is an ordinary service stop.
func detectStopReason() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "list-jobs", "--no-legend", "--no-pager").Output()
	if err != nil {
		return StopService
	}
	return ParseStopReason(string(out))
}
