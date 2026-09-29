package agent

import "syscall"

// stRdonly is ST_RDONLY from statvfs(3); Linux reports it in statfs's f_flags.
const stRdonly = 0x1

// readOnlyMount reports that the filesystem holding path is mounted read-only.
// A variable so tests can stub it.
var readOnlyMount = func(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return st.Flags&stRdonly != 0
}
