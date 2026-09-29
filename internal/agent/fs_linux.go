package agent

import "syscall"

// Filesystems that are images rather than storage: always 100 % "used" by
// construction (an ISO, a squashfs snap or Docker Desktop tool image).
var imageFilesystemTypes = map[int64]bool{
	0x9660:     true, // iso9660
	0x73717368: true, // squashfs
	0x28cd3d45: true, // cramfs
	0xe0f5e1e2: true, // erofs
}

// readOnlyMount reports that path lives on an image filesystem. It goes by the
// filesystem TYPE, not the mount's read-only flag: the container image sees
// the host through a read-only bind mount (/hostfs:ro), where every disk would
// look read-only, and a data disk the kernel re-mounted read-only after
// errors is exactly the kind of disk that must still be reported.
// A variable so tests can stub it.
var readOnlyMount = func(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return imageFilesystemTypes[int64(st.Type)]
}
