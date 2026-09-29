package agent

import (
	"fmt"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

var procGetTickCount64 = syscall.NewLazyDLL("kernel32.dll").NewProc("GetTickCount64")

// bootTime is now minus the uptime counter.
func bootTime() time.Time {
	ms, _, _ := procGetTickCount64.Call()
	return time.Now().Add(-time.Duration(ms) * time.Millisecond).UTC().Round(time.Second)
}

// bootID: Windows has no boot id, so it is derived from the boot time. That
// can drift by a second between two computations, so the previous run's id is
// kept while the boot time it recorded is within 30 s of this one.
func bootID(prev State) string {
	bt := bootTime()
	if prev.BootID != "" && !prev.BootTime.IsZero() {
		if d := bt.Sub(prev.BootTime); d < 30*time.Second && d > -30*time.Second {
			return prev.BootID
		}
	}
	return "win-" + strconv.FormatInt(bt.Unix(), 10)
}

func machineID(string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return v
}

// osVersion is e.g. "Windows Server 2022 Standard 21H2 (build 20348)".
func osVersion(string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "windows"
	}
	defer k.Close()
	name, _, _ := k.GetStringValue("ProductName")
	display, _, _ := k.GetStringValue("DisplayVersion")
	build, _, _ := k.GetStringValue("CurrentBuild")
	out := name
	if display != "" {
		out += " " + display
	}
	if build != "" {
		out += fmt.Sprintf(" (build %s)", build)
	}
	return out
}
