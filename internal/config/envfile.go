package config

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/rene-roid/kanshi/internal/statefile"
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
// comments included - exactly as it was. An empty value removes the key, so
// the built-in default applies again. The file and its directory are created
// if needed, the write goes through a rename so a crash cannot leave half a
// file behind, and the result is readable by its owner only: it holds the
// agent key. Written as root, it stays the service user's (see statefile).
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
	kept := lines[:0]
	for _, line := range lines {
		k, _, ok := parseLine(strings.TrimRight(line, "\r"))
		v, wanted := values[k]
		switch {
		case !ok || !wanted:
			kept = append(kept, line)
		case v == "":
			// Removed: the default applies again.
		case !replaced[k]:
			kept = append(kept, k+"="+v)
			replaced[k] = true
		default:
			kept = append(kept, "# "+line) // a later duplicate would win; neutralise it
		}
	}
	lines = kept
	for _, k := range order {
		if v, ok := values[k]; ok && v != "" && !replaced[k] {
			lines = append(lines, k+"="+v)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := statefile.Write(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return restrict(path)
}

// restrict locks the saved file down (0600 / an owner-only ACL). A variable so
// tests on Windows, which do not run elevated, can keep reading what they wrote.
var restrict = restrictFile
