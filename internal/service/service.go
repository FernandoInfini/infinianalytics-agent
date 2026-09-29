// Package service installs the agent as a system service - a systemd unit on
// Linux, a Windows service registered with the service control manager - and
// runs it under the latter.
package service

import (
	"context"
	"fmt"
	"strings"
)

const (
	Name        = "infinianalytics-agent"
	DisplayName = "InfiniAnalytics Agent"
	Description = "Sends this server's CPU, memory, disk, network and container metrics to InfiniAnalytics."
)

// RunFunc runs the agent until ctx ends. Once ctx is done, stopReason says
// why the process is stopping when the platform knows ("" otherwise) - on
// Windows the service control manager tells a system shutdown from a stop.
type RunFunc func(ctx context.Context, stopReason func() string) error

// UnitFile renders the systemd unit. dockerGroup adds the docker group, which
// only works where that group exists (systemd refuses to start otherwise).
func UnitFile(binary, configPath string, dockerGroup bool) string {
	groups := ""
	if dockerGroup {
		groups = "# In the docker group so it can read container stats and events.\nSupplementaryGroups=docker\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# InfiniAnalytics server agent - installed by `%s install`.\n", Name)
	fmt.Fprintf(&b, "# Settings: %s (see agent.env.example).\n", configPath)
	fmt.Fprintf(&b, `[Unit]
Description=%s
Documentation=https://analytics.infini.es
Wants=network-online.target
# Ordered after the network so that on shutdown it stops *before* the network
# goes down, while it can still report why.
After=network-online.target docker.service

[Service]
ExecStart=%s run
StateDirectory=%s
StateDirectoryMode=0700
Environment=IA_AGENT_CONFIG=%s
Restart=on-failure
RestartSec=5
TimeoutStopSec=20

# A throwaway user: the agent only reads /proc, /sys and the Docker API.
DynamicUser=yes
%sNoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
LockPersonality=yes
MemoryDenyWriteExecute=yes

# Stay out of the way of what the server is actually for.
Environment=GOMAXPROCS=1 GOMEMLIMIT=40MiB
CPUQuota=20%%
MemoryMax=128M

[Install]
WantedBy=multi-user.target
`, DisplayName, binary, Name, configPath, groups)
	return b.String()
}
