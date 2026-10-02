package statefile

// fileOwner: Windows files have no uid; the service runs as LocalSystem and
// install as an administrator, both of whom the file's ACL lets in.
func fileOwner(string) (uid, gid int, ok bool) { return 0, 0, false }

func userName(uid int) string { return "" }
