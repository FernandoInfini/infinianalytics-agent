package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rene-roid/kanshi/internal/config"
	"github.com/rene-roid/kanshi/internal/roots"
	"github.com/rene-roid/kanshi/internal/vitals"
)

// EnrollResult is the backend's 201 answer.
type EnrollResult struct {
	ServerID      string `json:"server_id"`
	AgentKey      string `json:"agent_key"`
	PushURL       string `json:"push_url"`
	PushIntervalS int    `json:"push_interval_s"`
}

// HostIdentity is what this machine reports about itself. In a container
// (hostRoot set) the host's own hostname and OS release are read from it.
func HostIdentity(memTotal uint64, hostRoot string) HostInfo {
	hostname, _ := os.Hostname()
	if hostRoot != "" {
		if raw, err := os.ReadFile(filepath.Join(hostRoot, "etc", "hostname")); err == nil {
			if h := strings.TrimSpace(string(raw)); h != "" {
				hostname = h
			}
		}
	}
	return HostInfo{
		Hostname:  hostname,
		OS:        runtime.GOOS,
		OSVersion: osVersion(hostRoot),
		Arch:      runtime.GOARCH,
		CPUCount:  runtime.NumCPU(),
		MemTotal:  memTotal,
	}
}

// stableMachineID is the machine's own id, else one generated once and kept
// in the state directory - a container with no view of the host's
// /etc/machine-id still re-binds to the same server as long as its volume
// survives.
func stableMachineID(cfg config.Config) (string, error) {
	// Override for machines cloned from one image, which share
	// /etc/machine-id and would otherwise all enroll as the same server.
	if id := strings.TrimSpace(os.Getenv("IA_AGENT_MACHINE_ID")); id != "" {
		return id, nil
	}
	if id := machineID(cfg.HostRoot); id != "" {
		return id, nil
	}
	dir := cfg.StateDir
	if dir == "" {
		dir = filepath.Dir(cfg.File)
	}
	path := filepath.Join(dir, "machine-id")
	if raw, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id, nil
		}
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := "generated-" + hex.EncodeToString(buf)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("no machine id found and could not persist one in %s: %w", dir, err)
	}
	return id, nil
}

// Enroll trades a one-time code from the dashboard for this server's agent
// key, and saves the identity into cfg.File.
func Enroll(ctx context.Context, cfg config.Config, url, code, version string) (EnrollResult, error) {
	url = strings.TrimRight(url, "/")
	path := cfg.File
	id, err := stableMachineID(cfg)
	if err != nil {
		return EnrollResult{}, err
	}
	reader := vitals.New(roots.NewResolver([]string{"auto"}, cfg.HostRoot))
	host := HostIdentity(reader.Sample().Memory.Total, cfg.HostRoot)

	payload, _ := json.Marshal(map[string]any{
		"code":          code,
		"machine_id":    id,
		"hostname":      host.Hostname,
		"os":            host.OS,
		"os_version":    host.OSVersion,
		"arch":          host.Arch,
		"cpu_count":     host.CPUCount,
		"mem_total":     host.MemTotal,
		"agent_version": version,
	})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/servers/enroll/", bytes.NewReader(payload))
	if err != nil {
		return EnrollResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "infinianalytics-agent/"+version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return EnrollResult{}, fmt.Errorf("could not reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusCreated {
		var answer struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &answer)
		if answer.Detail == "" {
			answer.Detail = resp.Status
		}
		return EnrollResult{}, fmt.Errorf("enrollment refused: %s", answer.Detail)
	}
	var out EnrollResult
	if err := json.Unmarshal(raw, &out); err != nil || out.ServerID == "" || out.AgentKey == "" {
		return EnrollResult{}, errors.New("unexpected answer from the backend")
	}

	err = config.SaveValues(path, map[string]string{
		config.KeyURL:      url,
		config.KeyServerID: out.ServerID,
		config.KeyAgentKey: out.AgentKey,
	}, []string{config.KeyURL, config.KeyServerID, config.KeyAgentKey})
	if err != nil {
		return out, fmt.Errorf("enrolled as %s but could not save %s: %w", out.ServerID, path, err)
	}
	return out, nil
}
