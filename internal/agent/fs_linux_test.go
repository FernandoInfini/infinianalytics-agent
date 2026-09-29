package agent

import "testing"

func TestReadOnlyMountOnAWritableDir(t *testing.T) {
	if readOnlyMount(t.TempDir()) {
		t.Fatal("a temp dir is writable, so it cannot be on a read-only mount")
	}
	if readOnlyMount("/nonexistent/path") {
		t.Fatal("an unreadable path is reported, not skipped")
	}
}
