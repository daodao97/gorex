//go:build darwin || linux

package push

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"gorex/internal/rex"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

func serviceSocket(dir string) string {
	p := filepath.Join(dir, "push.sock")
	if len(p) < 100 {
		return p
	}
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(os.TempDir(), fmt.Sprintf("gorex-push-%d-%x.sock", os.Getuid(), sum[:8]))
}

type serviceRequest struct {
	Op           string           `json:"op"`
	Registration *Registration    `json:"registration,omitempty"`
	Desktop      *DesktopActivity `json:"desktop,omitempty"`
}
type serviceResponse struct {
	Error  string `json:"error,omitempty"`
	Status Status `json:"status"`
}

// Ensure starts only the notification worker; the existing session server and
// its PTYs are never restarted. The worker persists subscriptions across app exit.
func Ensure(dir, sessionSocket string) error {
	if _, err := Query(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := rex.StartBackground([]string{"-push-service", dir, sessionSocket}, dir, "push.log"); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Query(dir); err == nil {
			return nil
		}
		time.Sleep(30 * time.Millisecond)
	}
	return errors.New("notification worker did not start")
}
func call(dir string, req serviceRequest) (Status, error) {
	conn, err := net.DialTimeout("unix", serviceSocket(dir), 300*time.Millisecond)
	if err != nil {
		return Status{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	if err = json.NewEncoder(conn).Encode(req); err != nil {
		return Status{}, err
	}
	var response serviceResponse
	if err = json.NewDecoder(io.LimitReader(conn, 8192)).Decode(&response); err != nil {
		return Status{}, err
	}
	if response.Error != "" {
		return response.Status, errors.New(response.Error)
	}
	return response.Status, nil
}
func Query(dir string) (Status, error) { return call(dir, serviceRequest{Op: "status"}) }
func Register(dir string, r Registration) (Status, error) {
	return call(dir, serviceRequest{Op: "register", Registration: &r})
}
func ReportDesktop(dir string, activity DesktopActivity) error {
	_, err := call(dir, serviceRequest{Op: "desktop", Desktop: &activity})
	return err
}

func Run(dir, socket string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "push.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("notification worker already running")
	}
	s, err := newService(dir)
	if err != nil {
		return err
	}
	path := serviceSocket(dir)
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer os.Remove(path)
	os.Chmod(path, 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.configure()
	go s.poll(ctx, socket)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s.deliver(ctx, now)
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.configure()
			}
		}
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			var req serviceRequest
			var response serviceResponse
			if err := json.NewDecoder(io.LimitReader(conn, 16384)).Decode(&req); err != nil {
				return
			}
			switch req.Op {
			case "status":
			case "desktop":
				if req.Desktop == nil {
					response.Error = "missing desktop activity"
				} else if err := s.desktopActivity(*req.Desktop, time.Now()); err != nil {
					response.Error = err.Error()
				}
			case "register":
				if req.Registration == nil {
					response.Error = "missing notification registration"
				} else if err := s.register(*req.Registration); err != nil {
					response.Error = err.Error()
				}
			default:
				response.Error = "unknown notification operation"
			}
			response.Status = s.status()
			json.NewEncoder(conn).Encode(response)
		}()
	}
}
func (s *service) poll(ctx context.Context, socket string) {
	var client *rex.Client
	defer func() {
		if client != nil {
			client.Close()
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if client == nil {
				conn, err := net.DialTimeout("unix", socket, time.Second)
				if err != nil {
					continue
				}
				client = rex.NewClient(conn, nil)
			}
			hello, err := client.Hello()
			var sessions []rex.SessionInfo
			if err == nil {
				sessions, err = client.List()
			}
			if err != nil {
				client.Close()
				client = nil
				continue
			}
			s.observe(hello, sessions, now)
		}
	}
}
