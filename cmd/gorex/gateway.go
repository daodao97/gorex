//go:build gorex_cli && (darwin || linux)

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"gorex/internal/rex"
	"gorex/internal/terminal/screen"
)

type gatewayState struct {
	PID   int  `json:"pid"`
	Ready bool `json:"ready"`
	// Returned only over the user's private management socket. Public status
	// output deliberately strips this credential.
	Link string `json:"link,omitempty"`
}

func gatewaySocket() string {
	path := filepath.Join(rex.Dir(), "gateway.sock")
	if len(path) < 100 {
		return path
	}
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(os.TempDir(), fmt.Sprintf("gorex-gateway-%d-%x.sock", os.Getuid(), sum[:8]))
}

func gatewayRequest(ctx context.Context, op string) (gatewayState, error) {
	var state gatewayState
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", gatewaySocket())
	if err != nil {
		return state, err
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	if at, ok := ctx.Deadline(); ok && at.Before(deadline) {
		deadline = at
	}
	conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(op); err != nil {
		return state, err
	}
	err = json.NewDecoder(conn).Decode(&state)
	return state, err
}

func startGateway(ctx context.Context, output, diagnostics io.Writer, linkFile string, qr bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := screen.Load(); err != nil {
		return err
	}
	state, err := gatewayRequest(ctx, "status")
	if err != nil {
		if err := rex.StartBackground([]string{"-gateway", linkFile}, rex.Dir(), "gateway.log"); err != nil {
			return err
		}
	}
	startup, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	for {
		if err == nil && state.Ready {
			if err := saveLink(linkFile, state.Link); err != nil {
				return err
			}
			err := printConnectionLink(output, state.Link, qr)
			if err == nil {
				fmt.Fprintf(diagnostics, "GoRex running in background · gateway PID %d\n", state.PID)
			}
			return err
		}
		select {
		case <-startup.Done():
			return fmt.Errorf("gateway startup did not complete; see %s: %w", filepath.Join(rex.Dir(), "gateway.log"), startup.Err())
		case <-time.After(100 * time.Millisecond):
			state, err = gatewayRequest(startup, "status")
		}
	}
}

func stopGateway(ctx context.Context, output io.Writer) error {
	if _, err := gatewayRequest(ctx, "stop"); err != nil {
		// Stopping an already-stopped gateway is harmless and never starts one.
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ECONNREFUSED) {
			_, err = fmt.Fprintln(output, "Gateway is not running; sessions were preserved.")
			return err
		}
		return err
	}
	stopping, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		if _, err := gatewayRequest(stopping, "status"); err != nil {
			_, err = fmt.Fprintln(output, "Gateway stopped; sessions continue in the background.")
			return err
		}
		select {
		case <-stopping.Done():
			return errors.New("gateway is still stopping; session server was preserved")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// The lock covers startup, serving and cleanup, including foreground/systemd
// operation. Concurrent start commands cannot publish competing identities.
func runGateway(ctx context.Context, output, diagnostics io.Writer, linkFile string, qr bool) error {
	if err := os.MkdirAll(rex.Dir(), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(rex.Dir(), "gateway.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("gateway is already running")
	}
	path := gatewaySocket()
	os.Remove(path) // Only the lock owner may replace a stale owned socket.
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	state := gatewayState{PID: os.Getpid()}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				var op string
				if json.NewDecoder(io.LimitReader(conn, 1024)).Decode(&op) != nil || (op != "status" && op != "stop") {
					return
				}
				mu.Lock()
				current := state
				mu.Unlock()
				json.NewEncoder(conn).Encode(current)
				if op == "stop" {
					cancel()
				}
			}()
		}
	}()
	err = serve(ctx, output, diagnostics, linkFile, qr, func(link string) { mu.Lock(); state.Link, state.Ready = link, true; mu.Unlock() })
	if ctx.Err() != nil {
		return nil
	}
	return err
}
