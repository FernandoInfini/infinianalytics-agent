//go:build !windows

package statefile

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// fileOwner is path's owner, without following a symlink.
func fileOwner(path string) (uid, gid int, ok bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

// userName is uid's name, else the number: a systemd dynamic user is only
// known by name while its service runs.
func userName(uid int) string {
	id := strconv.Itoa(uid)
	if u, err := user.LookupId(id); err == nil {
		return u.Username
	}
	return id
}
