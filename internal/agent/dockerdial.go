package agent

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// dockerDialer connects to the daemon named by a DOCKER_HOST-style address,
// exactly like internal/dockerstats does for the stats calls. It is repeated
// here because that one is unexported and internal/dockerstats is shared with
// upstream kanshi, which this fork never edits; once upstream exports it
// (dockerstats.Dialer), this file should go.
func dockerDialer(host string) (func(ctx context.Context) (net.Conn, error), error) {
	scheme, addr, ok := strings.Cut(host, "://")
	if !ok {
		return nil, fmt.Errorf("unsupported DOCKER_HOST %q", host)
	}
	d := &net.Dialer{Timeout: 5 * time.Second}
	switch scheme {
	case "unix":
		return func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, "unix", addr) }, nil
	case "tcp":
		return func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }, nil
	case "npipe":
		pipe := strings.ReplaceAll(addr, "/", `\`)
		return func(ctx context.Context) (net.Conn, error) { return dialPipe(ctx, pipe) }, nil
	}
	return nil, fmt.Errorf("unsupported DOCKER_HOST scheme %q", scheme)
}
