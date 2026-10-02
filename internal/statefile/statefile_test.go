package statefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fakeRoot makes the package believe it runs as root, reads owners from the
// map (uid = gid, ok only for listed paths) and records every chown.
func fakeRoot(t *testing.T, owners map[string]int) *[]string {
	t.Helper()
	var given []string
	euid, chown, ownerOf = func() int { return 0 }, func(p string, uid, gid int) error {
		if uid != gid {
			t.Errorf("chown %s %d:%d, want the uid's own group", p, uid, gid)
		}
		given = append(given, p)
		owners[p] = uid
		return nil
	}, func(p string) (int, int, bool) {
		uid, ok := owners[p]
		return uid, uid, ok
	}
	t.Cleanup(func() { euid, chown, ownerOf = os.Geteuid, os.Lchown, fileOwner })
	return &given
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestWriteNewFileTakesTheDirectoryOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.env")
	owners := map[string]int{dir: 61234}
	given := fakeRoot(t, owners)
	if err := Write(path, []byte("IA_AGENT_KEY=k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(*given) != 1 {
		t.Fatalf("chowns = %v, want one (the temporary file)", *given)
	}
	if got := owners[(*given)[0]]; got != 61234 {
		t.Errorf("new file given to %d, want the directory's owner 61234", got)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "IA_AGENT_KEY=k\n" {
		t.Errorf("content = %q", raw)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", info.Mode().Perm())
		}
	}
	noTempFiles(t, dir)
}

func TestWriteKeepsTheFileOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.env")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owners := map[string]int{dir: 61234, path: 1001}
	given := fakeRoot(t, owners)
	if err := Write(path, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(*given) != 1 || owners[(*given)[0]] != 1001 {
		t.Errorf("chowns = %v, want the temporary file given to the existing owner 1001", *given)
	}
}

func TestWriteNotRootChownsNothing(t *testing.T) {
	dir := t.TempDir()
	given := fakeRoot(t, map[string]int{dir: 61234})
	euid = func() int { return 1000 }
	if err := Write(filepath.Join(dir, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(*given) != 0 {
		t.Errorf("chowns = %v, want none", *given)
	}
}

func TestWriteFailureKeepsTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.env")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeRoot(t, map[string]int{dir: 61234})
	chown = func(string, int, int) error { return fs.ErrPermission }
	if err := Write(path, []byte("new\n"), 0o600); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want the chown failure", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "old\n" {
		t.Errorf("content = %q, want the old file untouched", raw)
	}
	noTempFiles(t, dir)
}

func TestRepair(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "agent.env")
	state := filepath.Join(dir, "state.json")
	spool := filepath.Join(dir, "spool")
	seg := filepath.Join(spool, "00000000000000000001.seg")
	if err := os.MkdirAll(spool, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{env, state, seg} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	owners := map[string]int{dir: 61234, env: 0, state: 61234, spool: 61234, seg: 0}
	given := fakeRoot(t, owners)

	var fixed []string
	for _, p := range []string{env, state, spool, filepath.Join(dir, "stop_reason")} {
		got, err := Repair(p)
		if err != nil {
			t.Fatalf("Repair(%s): %v", p, err)
		}
		fixed = append(fixed, got...)
	}
	if want := []string{env, seg}; !slices.Equal(fixed, want) {
		t.Errorf("fixed %v, want %v", fixed, want)
	}
	if !slices.Equal(*given, fixed) {
		t.Errorf("chowned %v, reported %v", *given, fixed)
	}
	for _, p := range fixed {
		if owners[p] != 61234 {
			t.Errorf("%s owned by %d, want 61234", p, owners[p])
		}
	}

	euid = func() int { return 1000 }
	owners[env] = 0
	if got, _ := Repair(env); len(got) != 0 {
		t.Errorf("not root: fixed %v, want nothing", got)
	}
}

func TestReadErrorNamesOwnerAndUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.env")
	fakeRoot(t, map[string]int{path: 0})
	euid = func() int { return 61234 }
	err := ReadError(path, &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission})
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("%v should still be a permission error", err)
	}
	want := "cannot read " + path + ": permission denied (owner "
	if !strings.HasPrefix(err.Error(), want) || !strings.HasSuffix(err.Error(), ", running as uid 61234)") {
		t.Errorf("err = %q, want %q... running as uid 61234)", err, want)
	}

	err = ReadError(path, &fs.PathError{Op: "read", Path: path, Err: errors.New("is a directory")})
	if got := err.Error(); got != "cannot read "+path+": is a directory" {
		t.Errorf("err = %q", got)
	}
}

// TestWriteAsRoot does it for real. CI runs it with sudo; elsewhere it skips.
func TestWriteAsRoot(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	dir := t.TempDir()
	if err := os.Chown(dir, 61234, 61234); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.env")
	owner := func() int {
		uid, gid, _ := fileOwner(path)
		if uid != gid {
			t.Errorf("owner %d:%d", uid, gid)
		}
		return uid
	}

	if err := Write(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := owner(); got != 61234 {
		t.Errorf("new file owned by %d, want the directory's owner 61234", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	if err := os.Chown(path, 4242, 4242); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := owner(); got != 4242 {
		t.Errorf("rewritten file owned by %d, want its own owner 4242 kept", got)
	}

	if err := os.Chown(path, 0, 0); err != nil {
		t.Fatal(err)
	}
	if fixed, err := Repair(path); err != nil || len(fixed) != 1 {
		t.Fatalf("Repair = %v, %v", fixed, err)
	}
	if got := owner(); got != 61234 {
		t.Errorf("repaired file owned by %d, want 61234", got)
	}
	noTempFiles(t, dir)

	// The way systemd lays it out: a root-owned link to the real directory.
	link := filepath.Join(t.TempDir(), "infinianalytics-agent")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(link, 0, 0); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(dir, "spool")
	if err := os.Mkdir(spool, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(link, "agent.env"), filepath.Join(link, "spool")} {
		if _, err := Repair(p); err != nil {
			t.Fatalf("Repair(%s): %v", p, err)
		}
	}
	for _, p := range []string{path, spool} {
		if uid, _, _ := fileOwner(p); uid != 61234 {
			t.Errorf("through the link, %s was given to %d, want the real directory's owner 61234", p, uid)
		}
	}
	if err := Write(filepath.Join(link, "stop_reason"), []byte("service\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if uid, _, _ := fileOwner(filepath.Join(dir, "stop_reason")); uid != 61234 {
		t.Errorf("new file written through the link owned by %d, want 61234", uid)
	}
}
