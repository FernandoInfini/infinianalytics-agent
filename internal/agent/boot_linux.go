package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// bootID is the kernel's own random id for this boot.
func bootID(State) string {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// machineID is systemd's stable machine identifier (dbus' on older systems).
// In a container the host's files are read under hostRoot.
func machineID(hostRoot string) string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if raw, err := os.ReadFile(filepath.Join(hostRoot, p)); err == nil {
			if id := strings.TrimSpace(string(raw)); id != "" {
				return id
			}
		}
	}
	return ""
}

// osVersion is PRETTY_NAME from os-release, e.g. "Debian GNU/Linux 12 (bookworm)".
func osVersion(hostRoot string) string {
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		f, err := os.Open(filepath.Join(hostRoot, p))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
				f.Close()
				return strings.Trim(v, `"'`)
			}
		}
		f.Close()
	}
	return runtime.GOOS
}
