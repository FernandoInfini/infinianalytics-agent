// Command infinianalytics-agent reports this server's health to InfiniAnalytics:
// CPU, memory, disks, network and Docker containers every 10 seconds, plus
// why the machine went away when it does (reboot, shutdown, crash).
//
//	infinianalytics-agent enroll <code> [--url URL]   link this server (code from the dashboard)
//	infinianalytics-agent install                     run it as a service (systemd / Windows)
//	infinianalytics-agent run                         run in the foreground (what the service runs)
//	infinianalytics-agent status                      last push, spool size
//	infinianalytics-agent uninstall | start | stop | version
//
// Host vitals are always sent. Disk space and Docker are modules, on by
// default: --disks off / --docker off (or IA_AGENT_DISKS / IA_AGENT_DOCKER).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/rene-roid/kanshi/internal/agent"
	"github.com/rene-roid/kanshi/internal/config"
	"github.com/rene-roid/kanshi/internal/service"
)

// version is stamped by the release build with -ldflags "-X main.version=…".
var version = "dev"

const defaultURL = "https://api.analytics.infini.es"

func main() {
	if service.IsService() {
		// Started by the Windows service control manager.
		if err := service.Run(runAgent("")); err != nil {
			logf("%v", err)
			os.Exit(1)
		}
		return
	}

	cmd := "run"
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", "", "settings file (default: "+config.FileName+" next to the program, then "+filepath.Join(config.DefaultDir(), config.FileName)+")")
	url := fs.String("url", "", "ingestion API base URL (enroll only; default "+defaultURL+")")
	modules := map[string]string{}
	fs.Var(moduleFlag{config.KeyDisks, modules}, "disks", "disk space readings: on or off")
	fs.Var(moduleFlag{config.KeyDocker, modules}, "docker", "Docker containers and events: on or off")
	fs.Usage = usage

	switch cmd {
	case "run":
		fs.Parse(args)
		// For this process only. The environment beats agent.env, so the
		// flags win over both.
		for key, value := range modules {
			os.Setenv(key, value)
		}
		resourceDefaults()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runAgent(*cfgPath)(ctx, func() string { return "" }); err != nil {
			fail(err)
		}
	case "enroll":
		code, rest := firstArg(args)
		fs.Parse(rest)
		if code == "" && fs.NArg() > 0 {
			code = fs.Arg(0)
		}
		if code == "" {
			fail(fmt.Errorf("usage: %s enroll <code> [--url URL] [--disks on|off] [--docker on|off]", exeName()))
		}
		enroll(code, *url, *cfgPath, modules)
	case "install":
		fs.Parse(args)
		cfg := config.Load(*cfgPath)
		if !cfg.Enrolled() {
			fail(fmt.Errorf("not enrolled yet: run `%s enroll <code>` first", exeName()))
		}
		saveModules(cfg.File, modules)
		exe, err := os.Executable()
		if err != nil {
			fail(err)
		}
		if err := service.Install(exe, cfg.File); err != nil {
			fail(err)
		}
		fmt.Printf("Installed and started the %s service.\n", service.Name)
	case "uninstall":
		must(service.Uninstall())
		fmt.Printf("Removed the %s service. Its settings and spool stay in %s.\n", service.Name, config.DefaultDir())
	case "start":
		must(service.Start())
	case "stop":
		must(service.Stop())
	case "note-stop":
		// The systemd unit's ExecStop, not for people.
		fs.Parse(args)
		dir := config.Load(*cfgPath).StateDir
		if dir == "" {
			dir = config.DefaultDir()
		}
		reason, err := agent.RecordStopReason(dir)
		must(err)
		logf("stopping (%s)", reason)
	case "status":
		fs.Parse(args)
		status(config.Load(*cfgPath))
	case "version", "--version", "-version":
		fmt.Println("infinianalytics-agent", version, runtime.GOOS+"/"+runtime.GOARCH)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

// runAgent is the service body: load settings, run until ctx ends.
func runAgent(cfgPath string) service.RunFunc {
	return func(ctx context.Context, stopReason func() string) error {
		cfg := config.Load(cfgPath)
		// The container image has no interactive step: a first start with
		// IA_AGENT_ENROLL_CODE enrolls, later starts find agent.env in the
		// state volume and ignore the (by then used) code.
		if code := strings.TrimSpace(os.Getenv("IA_AGENT_ENROLL_CODE")); code != "" && !cfg.Enrolled() {
			url := cfg.URL
			if url == "" {
				url = defaultURL
			}
			res, err := agent.Enroll(ctx, cfg, url, code, version)
			if err != nil {
				return err
			}
			logf("enrolled as server %s", res.ServerID)
			cfg = config.Load(cfgPath)
		}
		a, err := agent.New(cfg, version, logf)
		if err != nil {
			return err
		}
		a.StopReason = stopReason
		return a.Run(ctx)
	}
}

// moduleFlag is --disks / --docker. It takes a value, so both `--docker off`
// and `--docker=off` work, and only switches given on the command line are
// recorded.
type moduleFlag struct {
	key string
	set map[string]string
}

func (f moduleFlag) String() string { return "" }

func (f moduleFlag) Set(s string) error {
	on, ok := config.ParseSwitch(s)
	if !ok {
		return fmt.Errorf("want on or off, got %q", s)
	}
	f.set[f.key] = fmt.Sprint(on)
	return nil
}

// saveModules writes the switches given on the command line into agent.env,
// where the service finds them.
func saveModules(path string, modules map[string]string) {
	if len(modules) == 0 {
		return
	}
	if err := config.SaveValues(path, modules, []string{config.KeyDisks, config.KeyDocker}); err != nil {
		fail(fmt.Errorf("could not save the module settings to %s: %w", path, err))
	}
}

func enroll(code, url, cfgPath string, modules map[string]string) {
	cfg := config.Load(cfgPath)
	if url == "" {
		url = cfg.URL
	}
	if url == "" {
		url = defaultURL
	}
	res, err := agent.Enroll(context.Background(), cfg, url, code, version)
	if err != nil {
		fail(err)
	}
	saveModules(cfg.File, modules)
	fmt.Printf("Enrolled as server %s.\nSettings saved to %s.\n", res.ServerID, cfg.File)
	fmt.Printf("Next: `%s install` to run it as a service (or `%s run` to try it in the foreground).\n", exeName(), exeName())
}

func status(cfg config.Config) {
	fmt.Printf("config:     %s", cfg.File)
	if !cfg.FileLoaded {
		fmt.Print(" (not found)")
	}
	fmt.Println()
	if !cfg.Enrolled() {
		fmt.Println("enrolled:   no")
		return
	}
	fmt.Printf("server:     %s\nbackend:    %s\nmodules:    %s\n", cfg.ServerID, cfg.URL, agent.Modules(cfg))
	dir := cfg.StateDir
	if dir == "" {
		dir = config.DefaultDir()
	}
	st := agent.LoadState(dir)
	if st.LastPushAt.IsZero() {
		fmt.Println("last push:  never")
	} else {
		fmt.Printf("last push:  %s ago (%s", time.Since(st.LastPushAt).Round(time.Second), st.LastPushStatus)
		if st.LastPushCode != 0 {
			fmt.Printf(", HTTP %d", st.LastPushCode)
		}
		fmt.Println(")")
		if st.LastError != "" {
			fmt.Printf("last error: %s\n", st.LastError)
		}
		if !st.LastSuccessAt.IsZero() {
			fmt.Printf("last ok:    %s ago\n", time.Since(st.LastSuccessAt).Round(time.Second))
		}
	}
	if sp, err := agent.OpenSpool(filepath.Join(dir, "spool"), cfg.SpoolMaxAge, cfg.SpoolMaxBytes); err == nil {
		records, size, _ := sp.Pending()
		sp.Close()
		fmt.Printf("spool:      %d window(s) pending, %.1f KB\n", records, float64(size)/1024)
	}
}

func firstArg(args []string) (string, []string) {
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		return args[0], args[1:]
	}
	return "", args
}

func usage() {
	n := exeName()
	fmt.Fprintf(os.Stderr, `%s %s - InfiniAnalytics server agent

Usage:
  %s enroll <code> [--url URL]   link this server using a code from the dashboard
  %s install                     install and start the service (root / administrator)
  %s run                         run in the foreground
  %s status                      last push and pending spool
  %s uninstall | start | stop    manage the service
  %s version

Every command takes --config PATH (default: %s).

Modules (host vitals are always sent; these are on by default):
  --disks on|off                 disk space per filesystem
  --docker on|off                Docker containers and events
On enroll and install they are saved to the settings file, so the service
keeps them; on run they apply to that run only. The same switches are
IA_AGENT_DISKS and IA_AGENT_DOCKER in the environment or the settings file.
`, n, version, n, n, n, n, n, n, filepath.Join(config.DefaultDir(), config.FileName))
}

func exeName() string { return filepath.Base(os.Args[0]) }

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "infinianalytics-agent: "+format+"\n", args...)
}

func must(err error) {
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	logf("%v", err)
	os.Exit(1)
}

// resourceDefaults keeps a bare binary as small as the systemd unit makes it:
// one scheduler thread is plenty for a sample every 2 s, and a soft memory
// limit makes the GC work harder rather than grow. GOMAXPROCS / GOMEMLIMIT
// override either.
func resourceDefaults() {
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(1)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(40 << 20)
	}
}
