package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Stop reasons, as the agent names them.
const (
	reasonService        = "service"
	reasonSystemShutdown = "system_shutdown"
)

// IsService reports that the service control manager started this process.
func IsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// Run hands the process to the service control manager.
func Run(run RunFunc) error {
	return svc.Run(Name, &handler{run: run})
}

type handler struct{ run RunFunc }

// Execute is the service main. PRESHUTDOWN is accepted on purpose: Windows
// sends it before the network is torn down, which is the agent's chance to
// tell the backend "system_shutdown". SHUTDOWN, sent later, is the fallback.
func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPreShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reason atomic.Value
	reason.Store("")
	done := make(chan error, 1)
	go func() {
		done <- h.run(ctx, func() string { return reason.Load().(string) })
	}()
	status <- svc.Status{State: svc.Running, Accepts: accepts}

	stop := func(r string) {
		if ctx.Err() != nil {
			return // already stopping
		}
		reason.Store(r)
		status <- svc.Status{State: svc.StopPending, WaitHint: 15000}
		cancel()
	}
	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case c := <-requests:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop:
				stop(reasonService)
			case svc.PreShutdown, svc.Shutdown:
				stop(reasonSystemShutdown)
			}
		}
	}
}

// Install registers binary as an automatic-start service running `run`, with
// restart-on-failure, and starts it. Re-running it points an existing service
// at binary and restarts it (an upgrade).
func Install(binary, _ string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("install needs an elevated (administrator) prompt: %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(Name); err == nil {
		cfg, err := s.Config()
		if err == nil {
			cfg.BinaryPathName = windows.EscapeArg(binary) + " run"
			err = s.UpdateConfig(cfg)
		}
		if err == nil {
			_ = stopService(s)
			err = s.Start()
		}
		s.Close()
		return err
	}
	s, err := m.CreateService(Name, binary, mgr.Config{
		DisplayName: DisplayName,
		Description: Description,
		StartType:   mgr.StartAutomatic,
	}, "run")
	if err != nil {
		return err
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 2 * time.Minute},
	}, uint32((24 * time.Hour).Seconds()))
	return s.Start()
}

// Uninstall stops and deletes the service. agent.env and the spool stay in
// %ProgramData%\InfiniAnalytics Agent.
func Uninstall() error {
	return withService(func(s *mgr.Service) error {
		_ = stopService(s)
		return s.Delete()
	})
}

func Start() error { return withService(func(s *mgr.Service) error { return s.Start() }) }
func Stop() error  { return withService(stopService) }

func withService(fn func(*mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("needs an elevated (administrator) prompt: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", Name)
	}
	defer s.Close()
	return fn(s)
}

func stopService(s *mgr.Service) error {
	st, err := s.Control(svc.Stop)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for st.State != svc.Stopped {
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for the service to stop")
		}
		time.Sleep(300 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			return err
		}
	}
	return nil
}
