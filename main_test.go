package main

import (
	"testing"

	"github.com/rene-roid/kanshi/internal/config"
)

func TestSettingsToSave(t *testing.T) {
	got := settingsToSave(map[string]string{config.KeyDocker: "false"}, false)
	if len(got) != 1 || got[config.KeyDocker] != "false" {
		t.Errorf("without reset = %v", got)
	}
	got = settingsToSave(map[string]string{config.KeyDocker: "false"}, true)
	if len(got) != len(config.Settings) {
		t.Fatalf("with reset = %v, want every setting", got)
	}
	for _, key := range config.SettingKeys() {
		want := ""
		if key == config.KeyDocker {
			want = "false"
		}
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
	if _, ok := got[config.KeyMachineID]; ok {
		t.Error("reset must not touch the machine id")
	}
}
