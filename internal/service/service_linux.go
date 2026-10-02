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

// Install writes the systemd unit for binary, enables and (re)starts it, and
// checks that it stays up.
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
	return checkStaysUp()
}

// settle is how long a fresh start must stay up for install to call it good.
// An agent that cannot start exits within a second, and Restart= waits
// RestartSec=5 before the next try, so a crash loop shows here as the
// auto-restart state or a new process.
const settle = 5 * time.Second

// checkStaysUp watches the unit for settle: `systemctl restart` returns as
// soon as the process is started, whether or not it then crashes.
func checkStaysUp() error {
	var first unitState
	deadline := time.Now().Add(settle)
	for {
		out, err := exec.Command("systemctl", "show", Name,
			"-p", "ActiveState", "-p", "SubState", "-p", "Result", "-p", "MainPID", "-p", "NRestarts").Output()
		if err != nil {
			return fmt.Errorf("systemctl show %s: %v", Name, err)
		}
		now := parseUnitState(string(out))
		if first == (unitState{}) {
			first = now
		}
		if !stayedUp(first, now) {
			return fmt.Errorf("the %s service does not stay up (%s). Last log lines:\n%s", Name, now, journalTail())
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// unitState is what `systemctl show` says about the unit.
type unitState struct {
	ActiveState, SubState, Result, MainPID, NRestarts string
}

func parseUnitState(show string) unitState {
	var s unitState
	for _, line := range strings.Split(show, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch key {
		case "ActiveState":
			s.ActiveState = value
		case "SubState":
			s.SubState = value
		case "Result":
			s.Result = value
		case "MainPID":
			s.MainPID = value
		case "NRestarts":
			s.NRestarts = value
		}
	}
	return s
}

func (s unitState) String() string {
	return fmt.Sprintf("%s/%s, result %s, %s restart(s)", s.ActiveState, s.SubState, s.Result, s.NRestarts)
}

// stayedUp: still running, as the same process, with no restart since first.
func stayedUp(first, now unitState) bool {
	return now.ActiveState == "active" && now.SubState == "running" &&
		now.MainPID != "0" && now.MainPID == first.MainPID && now.NRestarts == first.NRestarts
}

// journalTail is the unit's last log lines, for an install that failed.
func journalTail() string {
	out, err := exec.Command("journalctl", "--unit", Name, "--lines", "20", "--no-pager").CombinedOutput()
	if err != nil {
		return "(journalctl failed: " + strings.TrimSpace(string(out)) + ")"
	}
	return strings.TrimRight(string(out), "\n")
}

// Uninstall stops and removes the unit. The state directory (key, spool) is
// kept. To forget the server entirely delete /var/lib/private/infinianalytics-agent
// as well as /var/lib/infinianalytics-agent: with DynamicUser= the latter is
// only systemd's link to the former.
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
