package agent

import (
	"context"
	"net"
	"os"
	"syscall"
	"time"
)

const (
	errorPipeBusy        = 231
	fileFlagOverlapped   = 0x40000000
	securitySQOSPresent  = 0x00100000
	securityIdentifyOnly = 0x00010000
)

// dialPipe opens a named pipe as a net.Conn (overlapped, so the runtime poller
// parks goroutines instead of threads). Same as internal/dockerstats.
func dialPipe(ctx context.Context, path string) (net.Conn, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	for {
		h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
			syscall.OPEN_EXISTING, fileFlagOverlapped|securitySQOSPresent|securityIdentifyOnly, 0)
		if err == nil {
			return &pipeConn{File: os.NewFile(uintptr(h), path), addr: pipeAddr(path)}, nil
		}
		if err != syscall.Errno(errorPipeBusy) {
			return nil, &net.OpError{Op: "dial", Net: "npipe", Addr: pipeAddr(path), Err: err}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type pipeAddr string

func (a pipeAddr) Network() string { return "npipe" }
func (a pipeAddr) String() string  { return string(a) }

type pipeConn struct {
	*os.File
	addr pipeAddr
}

func (c *pipeConn) LocalAddr() net.Addr  { return c.addr }
func (c *pipeConn) RemoteAddr() net.Addr { return c.addr }
