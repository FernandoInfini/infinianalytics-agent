//go:build !windows

package agent

import (
	"context"
	"errors"
	"net"
)

func dialPipe(context.Context, string) (net.Conn, error) {
	return nil, &net.OpError{Op: "dial", Net: "npipe", Err: errors.New("named pipes only exist on Windows")}
}
