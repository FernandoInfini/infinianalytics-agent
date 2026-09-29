package config

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the settings file. It uses the same KEY=VALUE format as a
// Compose .env file, so the two are interchangeable.
const FileName = "agent.env"

// findFile picks the settings file: an explicit path (flag, then
// IA_AGENT_CONFIG), else agent.env next to the executable, else the system
// location (/var/lib/infinianalytics-agent on Linux, %ProgramData%\InfiniAnalytics
// Agent on Windows). When none exists it still returns where one should be
// created, so `enroll` has somewhere to save to.
func findFile(explicit string) (path string, exists bool) {
	if explicit == "" {
		explicit = strings.TrimSpace(os.Getenv("IA_AGENT_CONFIG"))
	}
	if explicit != "" {
		return explicit, isFile(explicit)
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), FileName); isFile(p) {
			return p, true
		}
	}
	p := filepath.Join(DefaultDir(), FileName)
	return p, isFile(p)
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// ReadFile parses a KEY=VALUE file. Blank lines and # comments are skipped, an
// "export " prefix is tolerated, and a value may be wrapped in single or double
// quotes.
func ReadFile(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		key, value, ok := parseLine(sc.Text())
		if ok {
			out[key] = value
		}
	}
	return out, sc.Err()
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if n := len(value); n >= 2 && (value[0] == '"' || value[0] == '\'') && value[n-1] == value[0] {
		value = value[1 : n-1]
	} else if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i]) // trailing comment on an unquoted value
	}
	return key, value, key != ""
}

// SaveValues sets the given keys in the file, keeping every other line -
// comments included - exactly as it was. The file and its directory are
// created if needed, the write goes through a rename so a crash cannot leave
// half a file behind, and the result is readable by its owner only: it holds
// the agent key.
func SaveValues(path string, values map[string]string, order []string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\r\n"), "\n")
	}
	replaced := map[string]bool{}
	for i, line := range lines {
		k, _, ok := parseLine(strings.TrimRight(line, "\r"))
		v, wanted := values[k]
		if !ok || !wanted {
			continue
		}
		if !replaced[k] {
			lines[i] = k + "=" + v
			replaced[k] = true
		} else {
			lines[i] = "# " + line // a later duplicate would win; neutralise it
		}
	}
	for _, k := range order {
		if v, ok := values[k]; ok && !replaced[k] {
			lines = append(lines, k+"="+v)
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agent-*.env")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !isWindows {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return restrict(path)
}

// restrict locks the saved file down (0600 / an owner-only ACL). A variable so
// tests on Windows, which do not run elevated, can keep reading what they wrote.
var restrict = restrictFile
