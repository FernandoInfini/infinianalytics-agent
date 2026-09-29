package config

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Docker Desktop's engine pipe.
const defaultDockerHost = "npipe:////./pipe/docker_engine"

const isWindows = true

// DefaultDir is %ProgramData%\InfiniAnalytics Agent: machine-wide, and where a
// service running as LocalSystem finds it.
func DefaultDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "InfiniAnalytics Agent")
}

// restrictFile drops inherited permissions so only SYSTEM and Administrators
// can read the file - it holds the agent key. icacls ships with every Windows
// since Vista, which is simpler than building a DACL by hand.
func restrictFile(path string) error {
	return exec.Command("icacls", path, "/inheritance:r",
		"/grant:r", "*S-1-5-18:F", // SYSTEM
		"/grant:r", "*S-1-5-32-544:F", // BUILTIN\Administrators
	).Run()
}
