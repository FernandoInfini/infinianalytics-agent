//go:build !windows

package config

const defaultDockerHost = "unix:///var/run/docker.sock"

const isWindows = false

// DefaultDir is where agent.env, the spool and the agent's state live when
// nothing says otherwise. The systemd unit's StateDirectory= points here too.
func DefaultDir() string { return "/var/lib/infinianalytics-agent" }

// restrictFile is a no-op: SaveValues already created the file 0600.
func restrictFile(string) error { return nil }
