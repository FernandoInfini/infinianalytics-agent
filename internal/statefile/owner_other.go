//go:build !windows

package statefile

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// fileOwner is path's owner. It follows symlinks: with DynamicUser=
// /var/lib/infinianalytics-agent is root's link to the service user's
// /var/lib/private/infinianalytics-agent, and the directory is what counts.
func fileOwner(path string) (uid, gid int, ok bool) {
	info, err := os.Stat(path)
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
