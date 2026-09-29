package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
)

const unitPath = "/etc/systemd/system/" + Name + ".service"

// IsService is false on Linux: systemd runs the same `run` command a person
// would, and stops it with SIGTERM.
func IsService() bool { return false }

// Run is only meaningful on Windows.
func Run(RunFunc) error { return errors.New("not running under the Windows service control manager") }

// Install writes the systemd unit for binary, enables and (re)starts it.
func Install(binary, configPath string) error {
	if os.Geteuid() != 0 {
		return errors.New("install needs root (sudo)")
	}
	_, err := user.LookupGroup("docker")
	if err := os.WriteFile(unitPath, []byte(UnitFile(binary, configPath, err == nil)), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", Name}, {"restart", Name}} {
		if err := systemctl(args...); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall stops and removes the unit. The state directory (key, spool) is
// kept; delete /var/lib/infinianalytics-agent to forget the server entirely.
func Uninstall() error {
	if os.Geteuid() != 0 {
		return errors.New("uninstall needs root (sudo)")
	}
	_ = systemctl("disable", "--now", Name)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return systemctl("daemon-reload")
}

func Start() error { return systemctl("start", Name) }
func Stop() error  { return systemctl("stop", Name) }

func systemctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
