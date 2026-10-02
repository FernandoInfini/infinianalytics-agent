package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSetting(t *testing.T) {
	cases := []struct {
		in, key, value string
		ok             bool
	}{
		{"IA_AGENT_DOCKER=off", KeyDocker, "off", true},
		{"IA_AGENT_FS_ROOTS=auto,data=/srv/data", "IA_AGENT_FS_ROOTS", "auto,data=/srv/data", true},
		{`IA_AGENT_FS_ROOTS=auto,D:\My Data`, "IA_AGENT_FS_ROOTS", `auto,D:\My Data`, true},
		{"IA_AGENT_CONTAINER_LIMIT=0", "IA_AGENT_CONTAINER_LIMIT", "0", true},
		{"IA_AGENT_DOCKER_HOST=tcp://10.0.0.5:2375", "IA_AGENT_DOCKER_HOST", "tcp://10.0.0.5:2375", true},
		{"IA_AGENT_SPOOL_MAX_AGE=", "IA_AGENT_SPOOL_MAX_AGE", "", true}, // back to the default
		{"IA_AGENT_DOCKER=maybe", "", "", false},
		{"IA_AGENT_DOCKER_CONCURRENCY=0", "", "", false},
		{"IA_AGENT_FS_INTERVAL=-5", "", "", false},
		{"IA_AGENT_DOCKER_HOST=/var/run/docker.sock", "", "", false},
		{"IA_AGENT_FS_ROOTS=auto,,/data", "", "", false},
		{"IA_AGENT_KEY=stolen", "", "", false}, // identity is not a setting
		{"IA_AGENT_WINDOW=30", "", "", false},
		{"nonsense", "", "", false},
	}
	for _, c := range cases {
		k, v, err := ParseSetting(c.in)
		if (err == nil) != c.ok || k != c.key || v != c.value {
			t.Errorf("ParseSetting(%q) = %q, %q, %v", c.in, k, v, err)
		}
	}
}

func TestSaveValuesEmptyRemoves(t *testing.T) {
	restrict = func(string) error { return nil }
	defer func() { restrict = restrictFile }()
	path := filepath.Join(t.TempDir(), FileName)
	orig := "IA_AGENT_KEY=k\nIA_AGENT_DOCKER=false\n# keep me\nIA_AGENT_CONTAINER_LIMIT=7\n"
	os.WriteFile(path, []byte(orig), 0o600)
	err := SaveValues(path, map[string]string{
		KeyDocker:                  "",
		"IA_AGENT_CONTAINER_LIMIT": "20",
		"IA_AGENT_SPOOL_MAX_MB":    "",
		"IA_AGENT_FS_ROOTS":        "auto,/data",
	}, SettingKeys())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	got := string(raw)
	want := "IA_AGENT_KEY=k\n# keep me\nIA_AGENT_CONTAINER_LIMIT=20\nIA_AGENT_FS_ROOTS=auto,/data\n"
	if got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, "SPOOL") {
		t.Error("an empty value must not be appended")
	}
}

func TestMachineIDFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(path, []byte("IA_AGENT_MACHINE_ID=clone-7\n"), 0o600)
	if got := Load(path).MachineID; got != "clone-7" {
		t.Errorf("MachineID = %q", got)
	}
	t.Setenv(KeyMachineID, "from-env")
	if got := Load(path).MachineID; got != "from-env" {
		t.Errorf("MachineID = %q, env should win", got)
	}
}
