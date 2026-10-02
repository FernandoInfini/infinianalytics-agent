package service

import "testing"

func TestStayedUp(t *testing.T) {
	running := parseUnitState("ActiveState=active\nSubState=running\nResult=success\nMainPID=4242\nNRestarts=0\n")
	if running != (unitState{"active", "running", "success", "4242", "0"}) {
		t.Fatalf("parsed %+v", running)
	}
	if !stayedUp(running, running) {
		t.Error("the same running process should count as up")
	}
	// What a crash loop looks like between two tries, and after the next one.
	for _, show := range []string{
		"ActiveState=activating\nSubState=auto-restart\nResult=exit-code\nMainPID=0\nNRestarts=1\n",
		"ActiveState=active\nSubState=running\nResult=success\nMainPID=4300\nNRestarts=1\n",
		"ActiveState=failed\nSubState=failed\nResult=exit-code\nMainPID=0\nNRestarts=5\n",
		"ActiveState=inactive\nSubState=dead\nResult=success\nMainPID=0\nNRestarts=0\n",
	} {
		if now := parseUnitState(show); stayedUp(running, now) {
			t.Errorf("%s counted as up", now)
		}
	}
	// systemd before 235 has no NRestarts; the PID still gives a restart away.
	old := parseUnitState("ActiveState=active\nSubState=running\nResult=success\nMainPID=4242\n")
	if !stayedUp(old, old) {
		t.Error("a missing NRestarts should not count as a restart")
	}
}
