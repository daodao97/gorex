//go:build gorex_cli && (darwin || linux)

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gorex/internal/remote"
	"gorex/internal/rex"
)

// Exercise the packaged CLI executable, its private helpers and real encrypted
// transport against only an owned directory and disposable shell.
func TestCLIServerLifecycle(t *testing.T) {
	if os.Getenv("GOREX_CLI_E2E") != "1" {
		t.Skip("set GOREX_CLI_E2E=1 for live encrypted CLI transport")
	}
	dir := t.TempDir()
	t.Setenv("GOREX_DIR", filepath.Join(dir, "state"))
	binary := os.Getenv("GOREX_CLI_BINARY")
	if binary == "" {
		binary = filepath.Join(dir, "gorex")
		build := exec.Command("go", "build", "-tags", "gorex_cli", "-o", binary, ".")
		build.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build CLI: %v\n%s", err, output)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		stopGateway(cleanup, io.Discard)
		if client, err := localClient(cleanup); err == nil {
			client.Shutdown()
			client.Close()
		}
	})
	// On macOS avoid registering even a disposable launchd job. Linux also
	// exercises the public serve command spawning its own detached daemon.
	if runtime.GOOS == "darwin" {
		server := exec.Command(binary, "-server")
		server.Stdout, server.Stderr = io.Discard, io.Discard
		if err := server.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { server.Process.Kill(); server.Wait() })
		waitLocal(t, ctx).Close()
	}
	start := func() (*exec.Cmd, <-chan error, string) {
		args := []string{"serve", "--data-dir", rex.Dir()}
		if runtime.GOOS == "darwin" {
			args = append(args, "--foreground")
		}
		cmd := exec.Command(binary, args...)
		cmd.Stderr = io.Discard
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		t.Cleanup(func() { cmd.Process.Kill() })
		line := make(chan string, 1)
		go func() {
			scan := bufio.NewScanner(out)
			if scan.Scan() {
				line <- scan.Text()
			} else {
				line <- ""
			}
		}()
		select {
		case link := <-line:
			// Drain the required output before Wait closes StdoutPipe, especially
			// for the background start command which returns immediately.
			go func() { done <- cmd.Wait() }()
			if _, err := remote.ParseLink(link); err != nil {
				t.Fatal("CLI did not produce a valid connection code")
			}
			if runtime.GOOS == "linux" {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal("background start command failed", err)
					}
				case <-ctx.Done():
					t.Fatal("background start command did not return")
				}
				state, err := gatewayRequest(ctx, "status")
				if err != nil || !state.Ready || state.PID == cmd.Process.Pid {
					t.Fatal("gateway did not stay alive independently of the start command")
				}
				repeated := exec.Command(binary, "serve", "--data-dir", rex.Dir())
				repeated.Stdout, repeated.Stderr = io.Discard, io.Discard
				if err := repeated.Run(); err != nil {
					t.Fatal("repeated background start failed", err)
				}
				again, err := gatewayRequest(ctx, "status")
				if err != nil || again.PID != state.PID {
					t.Fatal("repeated start replaced the resident gateway")
				}
				status := exec.Command(binary, "status", "--data-dir", rex.Dir())
				data, err := status.Output()
				var decoded struct {
					Gateway gatewayState `json:"gateway"`
				}
				if err != nil || json.Unmarshal(data, &decoded) != nil || decoded.Gateway.PID != state.PID || !decoded.Gateway.Ready || strings.Contains(string(data), "gorex://") {
					t.Fatal("CLI status lost service state or exposed credentials")
				}
				info, err := os.Stat(gatewaySocket())
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("gateway management socket is not private")
				}
				log, _ := os.ReadFile(filepath.Join(rex.Dir(), "gateway.log"))
				if strings.Contains(string(log), "gorex://") {
					t.Fatal("background service leaked its connection code into logs")
				}
			}
			return cmd, done, link
		case <-ctx.Done():
			t.Fatal("CLI gateway startup timed out")
		}
		return nil, nil, ""
	}
	stop := func(cmd *exec.Cmd, done <-chan error) {
		if runtime.GOOS == "linux" {
			stop := exec.Command(binary, "stop", "--data-dir", rex.Dir())
			stop.Stdout, stop.Stderr = io.Discard, io.Discard
			if err := stop.Run(); err != nil {
				t.Fatal("background gateway did not stop cleanly", err)
			}
			return
		}
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal("CLI gateway did not stop cleanly", err)
			}
		case <-ctx.Done():
			t.Fatal("CLI gateway shutdown timed out")
		}
	}
	cmd, done, link := start()
	local := waitLocal(t, ctx)
	t.Cleanup(func() { local.Shutdown(); local.Close() })
	hello, err := local.Hello()
	if err != nil {
		t.Fatal(err)
	}
	client, dispose, err := remote.Connect(ctx, link)
	if err != nil {
		t.Fatal("desktop protocol cannot connect to CLI", err)
	}
	t.Cleanup(dispose)
	t.Cleanup(func() { client.Close() })
	if _, err := client.HelloFrom(rex.DeviceInfo{Name: "Fixture desktop", OS: "macOS"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(); err != nil {
		t.Fatal(err)
	}
	session, err := client.Create(rex.CreateOptions{Command: []string{"/bin/sh"}, Dir: dir, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	stream := client.LockStream(session.ID, "fixture-desktop", "Fixture desktop", 100, 30)
	t.Cleanup(func() { stream.Close() })
	if _, err := stream.Write([]byte("printf '\\nCLI_''ROUNDTRIP_OK\\n'\r")); err != nil {
		t.Fatal(err)
	}
	received := make(chan bool, 1)
	go func() {
		var output strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := stream.Read(buf)
			output.Write(buf[:n])
			if strings.Contains(output.String(), "CLI_ROUNDTRIP_OK") {
				received <- true
				return
			}
			if err != nil {
				received <- false
				return
			}
		}
	}()
	select {
	case ok := <-received:
		if !ok {
			t.Fatal("shell input/output failed")
		}
	case <-ctx.Done():
		t.Fatal("shell input/output timed out")
	}
	active, err := client.List()
	if err != nil || len(active) != 1 || active[0].Cols != 100 || active[0].Rows != 30 || active[0].SizeLock != "fixture-desktop" {
		t.Fatal("CLI did not adapt to the desktop's active viewport")
	}
	select {
	case <-stream.CloseAndReleaseSize():
	case <-ctx.Done():
		t.Fatal("desktop size release timed out")
	}
	client.Close()
	dispose()
	stop(cmd, done)
	before, err := local.List()
	if err != nil || len(before) != 1 || before[0].ID != session.ID || before[0].Exited {
		t.Fatal("stopping CLI ended its session")
	}
	cmd, done, reconnectedLink := start()
	if reconnectedLink != link {
		t.Fatal("CLI restart changed saved connection credentials")
	}
	phone, closePhone, err := remote.Connect(ctx, reconnectedLink)
	if err != nil {
		t.Fatal("cannot reconnect to CLI", err)
	}
	defer phone.Close()
	defer closePhone()
	after, err := phone.List()
	if err != nil || len(after) != 1 || after[0].ID != session.ID || after[0].PID != before[0].PID {
		t.Fatal("reconnection did not preserve shell PID and session ID")
	}
	newHello, err := phone.Hello()
	if err != nil || newHello.PID != hello.PID {
		t.Fatal("CLI gateway restart replaced session daemon")
	}
	viewer := phone.LockStream(session.ID, "fixture-phone", "Fixture phone", 48, 18)
	if _, err := viewer.Read(make([]byte, 4096)); err != nil {
		t.Fatal("reconnected phone cannot read saved screen", err)
	}
	adapted, err := phone.List()
	if err != nil || len(adapted) != 1 || adapted[0].Cols != 48 || adapted[0].Rows != 18 || adapted[0].PID != session.PID {
		t.Fatal("phone viewport did not resize the same CLI session")
	}
	select {
	case <-viewer.CloseAndReleaseSize():
	case <-ctx.Done():
		t.Fatal("phone size release timed out")
	}
	phone.Close()
	closePhone()
	stop(cmd, done)
	info, err := os.Stat(filepath.Join(rex.Dir(), "connect.txt"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("CLI connection file is not private")
	}
}

func waitLocal(t *testing.T, ctx context.Context) *rex.Client {
	t.Helper()
	for {
		conn, err := net.DialTimeout("unix", rex.SocketPath(), time.Second)
		if err == nil {
			return rex.NewClient(conn, func(ctx context.Context) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", rex.SocketPath())
			})
		}
		select {
		case <-ctx.Done():
			t.Fatal(fmt.Errorf("fixture daemon not ready: %w", ctx.Err()))
		case <-time.After(20 * time.Millisecond):
		}
	}
}
