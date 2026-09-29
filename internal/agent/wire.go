// Package agent samples this machine, keeps what it could not deliver in an
// on-disk spool, and pushes 10-second summaries to the InfiniAnalytics
// ingestion API.
//
// The payload is wire contract v1, whose JSON Schema lives in the backend
// repository (docs/wire-v1.json). Field names here are that contract.
package agent

import "time"

// SchemaVersion is the wire contract version this agent speaks.
const SchemaVersion = 1

// Stat is a reading summarised over a window. A nil pointer is omitted.
type Stat struct {
	Avg *float64 `json:"avg,omitempty"`
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// HostInfo is the machine's identity, sent when it changes and hourly.
type HostInfo struct {
	Hostname  string `json:"hostname,omitempty"`
	OS        string `json:"os,omitempty"`
	OSVersion string `json:"os_version,omitempty"`
	Arch      string `json:"arch,omitempty"`
	CPUCount  int    `json:"cpu_count,omitempty"`
	MemTotal  uint64 `json:"mem_total,omitempty"`
}

// HostSample is one host window.
type HostSample struct {
	TS         time.Time `json:"ts"`
	DurS       int       `json:"dur_s"`
	N          int       `json:"n"`
	CPU        Stat      `json:"cpu"`
	CPUCoreMax *float64  `json:"cpu_core_max,omitempty"`
	MemPct     Stat      `json:"mem_pct"`
	MemUsed    Stat      `json:"mem_used"`
	SwapPct    Stat      `json:"swap_pct"`
	NetRX      Stat      `json:"net_rx"`
	NetTX      Stat      `json:"net_tx"`
	DiskRead   Stat      `json:"disk_read"`
	DiskWrite  Stat      `json:"disk_write"`
	Load1      *float64  `json:"load1,omitempty"`
	TempMax    *float64  `json:"temp_max,omitempty"`
	UptimeS    *int64    `json:"uptime_s,omitempty"`
}

// FilesystemRow is one mount at one reading.
type FilesystemRow struct {
	TS    time.Time `json:"ts"`
	Mount string    `json:"mount"`
	Label string    `json:"label,omitempty"`
	Total uint64    `json:"total"`
	Used  uint64    `json:"used"`
}

// ContainerRow is one container window.
type ContainerRow struct {
	TS       time.Time `json:"ts"`
	Key      string    `json:"key"`
	Name     string    `json:"name,omitempty"`
	Image    string    `json:"image,omitempty"`
	State    string    `json:"state,omitempty"`
	Health   string    `json:"health,omitempty"`
	N        int       `json:"n"`
	CPU      Stat      `json:"cpu"`
	MemUsed  Stat      `json:"mem_used"`
	MemLimit *float64  `json:"mem_limit,omitempty"`
	PIDs     *float64  `json:"pids,omitempty"`
	NetRX    *float64  `json:"net_rx,omitempty"`
	NetTX    *float64  `json:"net_tx,omitempty"`
	BlkRead  *float64  `json:"blk_read,omitempty"`
	BlkWrite *float64  `json:"blk_write,omitempty"`
}

// Event is something that happened at a point in time.
type Event struct {
	TS   time.Time      `json:"ts"`
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// Event types.
const (
	EventBoot             = "boot"
	EventStopping         = "stopping"
	EventContainerStart   = "container_start"
	EventContainerStop    = "container_stop"
	EventContainerDie     = "container_die"
	EventContainerOOM     = "container_oom"
	EventContainerRestart = "container_restart"
	EventContainerHealth  = "container_health"
	EventContainerImage   = "container_image"
	EventFSMount          = "fs_mount"
	EventFSUnmount        = "fs_unmount"
	EventAgentUpdate      = "agent_update"
)

// Stop reasons carried by a stopping event.
const (
	StopReboot         = "reboot"
	StopShutdown       = "shutdown"
	StopSystemShutdown = "system_shutdown"
	StopService        = "service"
)

// Batch is one POST /v1/servers/metrics/ body.
type Batch struct {
	SchemaVersion int             `json:"schema_version"`
	AgentVersion  string          `json:"agent_version"`
	ServerID      string          `json:"server_id"`
	Seq           uint64          `json:"seq"`
	BootID        string          `json:"boot_id"`
	SentAt        time.Time       `json:"sent_at"`
	Host          *HostInfo       `json:"host,omitempty"`
	Cores         []float64       `json:"cores,omitempty"`
	Samples       []HostSample    `json:"samples"`
	Filesystems   []FilesystemRow `json:"filesystems,omitempty"`
	Containers    []ContainerRow  `json:"containers,omitempty"`
	Events        []Event         `json:"events,omitempty"`
}

// PushResponse is the 200 answer.
type PushResponse struct {
	AcceptedSeq uint64 `json:"accepted_seq"`
	NextPushS   int    `json:"next_push_s"`
}

// Record is one closed window as kept in the spool. Several are merged into
// one Batch when pushing, oldest first.
type Record struct {
	Seq         uint64          `json:"seq"`
	Samples     []HostSample    `json:"samples,omitempty"`
	Filesystems []FilesystemRow `json:"filesystems,omitempty"`
	Containers  []ContainerRow  `json:"containers,omitempty"`
	Events      []Event         `json:"events,omitempty"`
}

func ptr[T any](v T) *T { return &v }
