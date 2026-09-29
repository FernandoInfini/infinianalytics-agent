package agent

// readOnlyMount: the Windows roots are fixed drives, which are writable; a
// read-only medium (a mounted ISO) is a CD-ROM drive and never a root.
var readOnlyMount = func(string) bool { return false }
