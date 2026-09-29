//go:build !linux

package agent

// detectStopReason: on Windows the service control manager says why (see
// internal/service), so a stop that reaches here without a reason is a
// console Ctrl+C or a service stop.
func detectStopReason() string { return StopService }
