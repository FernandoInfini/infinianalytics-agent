package config

import (
	"fmt"
	"strconv"
	"strings"
)

// KeyMachineID overrides the machine id `enroll` reports. Saved by
// `enroll --machine-id`, so a later re-enroll of the same clone keeps it.
const KeyMachineID = "IA_AGENT_MACHINE_ID"

// Setting is one knob `--set KEY=VALUE` may write into agent.env and
// `--reset` puts back to its default. Identity (URL, server id, key, machine
// id) and paths are deliberately not settings: changing them is enrolling.
type Setting struct {
	Key   string
	check func(string) error
}

// Settings lists every adjustable setting, in the order they are written.
var Settings = []Setting{
	{KeyDisks, isSwitch},
	{KeyDocker, isSwitch},
	{"IA_AGENT_FS_ROOTS", isRootList},
	{"IA_AGENT_FS_INTERVAL", isPositiveNumber},
	{"IA_AGENT_CONTAINER_LIMIT", isCount(0)},
	{"IA_AGENT_DOCKER_CONCURRENCY", isCount(1)},
	{"IA_AGENT_DOCKER_HOST", isDockerHost},
	{"IA_AGENT_SPOOL_MAX_AGE", isPositiveNumber},
	{"IA_AGENT_SPOOL_MAX_MB", isCount(1)},
}

// SettingKeys is Settings' keys, in order.
func SettingKeys() []string {
	keys := make([]string, len(Settings))
	for i, s := range Settings {
		keys[i] = s.Key
	}
	return keys
}

// ParseSetting reads a `--set` argument. An empty value is valid: it means
// "back to the default" and removes the key from agent.env.
func ParseSetting(arg string) (key, value string, err error) {
	key, value, ok := strings.Cut(arg, "=")
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	if !ok || key == "" {
		return "", "", fmt.Errorf("want KEY=VALUE, got %q", arg)
	}
	for _, s := range Settings {
		if s.Key != key {
			continue
		}
		if strings.ContainsAny(value, "\r\n") {
			return "", "", fmt.Errorf("%s: the value cannot span lines", key)
		}
		if value != "" {
			if err := s.check(value); err != nil {
				return "", "", fmt.Errorf("%s: %w", key, err)
			}
		}
		return key, value, nil
	}
	return "", "", fmt.Errorf("%s is not an adjustable setting (one of: %s)", key, strings.Join(SettingKeys(), ", "))
}

func isSwitch(v string) error {
	if _, ok := ParseSwitch(v); !ok {
		return fmt.Errorf("want on or off, got %q", v)
	}
	return nil
}

func isPositiveNumber(v string) error {
	if n, err := strconv.ParseFloat(v, 64); err != nil || n <= 0 {
		return fmt.Errorf("want a number of seconds above 0, got %q", v)
	}
	return nil
}

func isCount(min int) func(string) error {
	return func(v string) error {
		if n, err := strconv.Atoi(v); err != nil || n < min {
			return fmt.Errorf("want a whole number of at least %d, got %q", min, v)
		}
		return nil
	}
}

func isDockerHost(v string) error {
	for _, scheme := range []string{"unix://", "npipe://", "tcp://"} {
		if strings.HasPrefix(v, scheme) && len(v) > len(scheme) {
			return nil
		}
	}
	return fmt.Errorf("want a unix://, npipe:// or tcp:// address, got %q", v)
}

func isRootList(v string) error {
	for _, entry := range strings.Split(v, ",") {
		if strings.TrimSpace(entry) == "" {
			return fmt.Errorf("empty entry in %q", v)
		}
	}
	return nil
}
