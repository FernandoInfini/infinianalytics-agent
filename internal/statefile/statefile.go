// Package statefile writes the files the agent keeps in its state directory:
// agent.env, state.json, stop_reason, machine-id.
//
// On Linux the service runs as a throwaway systemd user (DynamicUser=yes) that
// owns that directory, while `install`, `enroll` and the unit's ExecStop run
// as root. A file root creates there is root's, mode 0600, and the service can
// no longer read it. systemd does not fix that on restart: it only re-owns the
// directory's contents when the directory itself has the wrong owner. So every
// write here gives the file the owner it had or, for a new file, the owner of
// its directory.
package statefile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Variables so tests can play root.
var (
	euid    = os.Geteuid
	chown   = os.Lchown
	ownerOf = fileOwner
)

// Write replaces path with data atomically: a temporary file in the same
// directory gets the data, its owner and perm, and is then renamed over path,
// so nobody ever sees half a file or one owned by the wrong user.
//
// Run as root, the file keeps the owner it had; a new one gets the owner of
// its directory. Anyone else cannot give files away, and has no need to.
func Write(path string, data []byte, perm fs.FileMode) error {
	uid, gid, give := -1, -1, false
	if euid() == 0 {
		uid, gid, give = ownerOf(path)
		if !give {
			uid, gid, give = ownerOf(filepath.Dir(path))
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // nothing left to remove once renamed
	err = fill(tmp, data, perm, uid, gid, give)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func fill(f *os.File, data []byte, perm fs.FileMode, uid, gid int, give bool) error {
	if _, err := f.Write(data); err != nil {
		return err
	}
	if give {
		if err := chown(f.Name(), uid, gid); err != nil {
			return err
		}
	}
	// Windows only has a read-only bit; the ACL is what protects a file there.
	if err := f.Chmod(perm); err != nil && runtime.GOOS != "windows" {
		return err
	}
	return f.Sync()
}

// Repair gives path - and everything under it, for a directory - to the owner
// of the directory holding it, wherever the two differ. It is how `install`
// heals files an earlier version wrote as root. Symlinks are left alone. Only
// root can give files away, so for anyone else this does nothing, as it does
// for a path that does not exist. It returns the paths it changed.
func Repair(path string) ([]string, error) {
	if euid() != 0 {
		return nil, nil
	}
	uid, gid, ok := ownerOf(filepath.Dir(path))
	if !ok {
		return nil, nil
	}
	var fixed []string
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == path && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if u, g, ok := ownerOf(p); !ok || (u == uid && g == gid) {
			return nil
		}
		if err := chown(p, uid, gid); err != nil {
			return err
		}
		fixed = append(fixed, p)
		return nil
	})
	return fixed, err
}

// ReadError says why path could not be read. For a permission problem it
// names the file's owner and who is asking, which is what it takes to see a
// root-owned file in a directory the service user owns.
func ReadError(path string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	if errors.Is(err, fs.ErrPermission) {
		if uid, _, ok := ownerOf(path); ok {
			return fmt.Errorf("cannot read %s: %w (owner %s, running as uid %d)", path, err, userName(uid), euid())
		}
	}
	return fmt.Errorf("cannot read %s: %w", path, err)
}
