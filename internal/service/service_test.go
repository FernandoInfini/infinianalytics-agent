package service

import (
	"strings"
	"testing"
)

func TestUnitFile(t *testing.T) {
	unit := UnitFile("/usr/local/bin/infinianalytics-agent", "/var/lib/infinianalytics-agent/agent.env", true)
	for _, want := range []string{
		"ExecStart=/usr/local/bin/infinianalytics-agent run",
		"StateDirectory=infinianalytics-agent",
		"Environment=IA_AGENT_CONFIG=/var/lib/infinianalytics-agent/agent.env",
		"SupplementaryGroups=docker",
		"DynamicUser=yes",
		"After=network-online.target",
		"CPUQuota=20%\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit is missing %q:\n%s", want, unit)
		}
	}
	// kanshi needed this to size folders; the agent reads nothing it cannot
	// read unprivileged.
	if strings.Contains(unit, "CAP_DAC_READ_SEARCH") {
		t.Error("the agent must not ask for CAP_DAC_READ_SEARCH")
	}
	if strings.Contains(UnitFile("/x", "/y", false), "SupplementaryGroups") {
		t.Error("no docker group on this machine: systemd would refuse to start the unit")
	}
}
