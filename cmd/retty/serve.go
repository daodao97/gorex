//go:build retty_cli && (darwin || linux)

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"retty/internal/remote"
	"retty/internal/rex"
	"retty/internal/terminal/screen"
)

const usage = `Retty CLI — Reconnect TTY / Relay TTY.
Connect a desktop or phone to this server's terminals.

Usage:
  retty serve [--data-dir PATH] [--link-file PATH] [--foreground]
  retty link [--data-dir PATH] [--link-file PATH]
  retty status [--data-dir PATH]
  retty stop [--data-dir PATH]
  retty version

serve starts the encrypted gateway in the background and prints a retty:// link.
The command returns; the gateway stays available after the terminal closes.
Use --foreground when running under systemd or another service supervisor.
Paste it into Retty desktop's connection settings, or open it on your phone.
Stopping the gateway preserves the background session server and its shells.
link prints the saved connection link; status reads the gateway/session server.
Data defaults to RETTY_DIR or the user config directory's RettyServer folder.
The link is a full-access credential; connect.txt is saved with mode 0600.
An interactive terminal also displays a QR code for phone scanning.
Use --qr with serve/link to print a QR code even when output is redirected.
`

func run(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(output, usage)
		return err
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) != 1 {
			return errors.New("version takes no arguments")
		}
		_, err := fmt.Fprintf(output, "retty %s (%s)\n", version, commit)
		return err
	}
	command := args[0]
	if command != "serve" && command != "start" && command != "link" && command != "status" && command != "stop" {
		return fmt.Errorf("unknown command %q; run retty help", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	dir := flags.String("data-dir", "", "session and connection state directory")
	var linkFile string
	var foreground bool
	var qr bool
	if command == "serve" || command == "start" {
		flags.BoolVar(&foreground, "foreground", false, "run in the foreground (for systemd)")
	}
	if command != "status" && command != "stop" {
		flags.StringVar(&linkFile, "link-file", "", "connection link file (default: <data-dir>/connect.txt)")
		flags.BoolVar(&qr, "qr", false, "display QR code even with redirected output")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := setDataDir(*dir); err != nil {
		return err
	}
	if linkFile == "" {
		linkFile = filepath.Join(rex.Dir(), "connect.txt")
	}
	linkFile, err := filepath.Abs(linkFile)
	if err != nil {
		return err
	}
	switch command {
	case "stop":
		return stopGateway(ctx, output)
	case "link":
		data, err := os.ReadFile(linkFile)
		if err != nil {
			return fmt.Errorf("read connection link (run retty serve first): %w", err)
		}
		if _, err := remote.ParseLink(string(data)); err != nil {
			return errors.New("saved connection link is invalid")
		}
		return printConnectionLink(output, strings.TrimSpace(string(data)), qr)
	case "status":
		client, err := localClient(ctx)
		if err != nil {
			return fmt.Errorf("session server is not running: %w", err)
		}
		defer client.Close()
		hello, err := client.Hello()
		if err != nil {
			return err
		}
		sessions, err := client.List()
		if err != nil {
			return err
		}
		gateway, _ := gatewayRequest(ctx, "status")
		gateway.Link = ""
		return json.NewEncoder(output).Encode(struct {
			Gateway  gatewayState      `json:"gateway"`
			Hello    rex.Hello         `json:"server"`
			Sessions []rex.SessionInfo `json:"sessions"`
		}{gateway, hello, sessions})
	default:
		if foreground {
			return runGateway(ctx, output, diagnostics, linkFile, qr)
		}
		return startGateway(ctx, output, diagnostics, linkFile, qr)
	}
}

func setDataDir(dir string) error {
	if dir == "" {
		dir = os.Getenv("RETTY_DIR")
	}
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(base, "RettyServer")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	return os.Setenv("RETTY_DIR", abs)
}

func localClient(ctx context.Context) (*rex.Client, error) {
	return rex.ConnectDial(ctx, func(ctx context.Context) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", rex.SocketPath())
		if err == nil {
			conn.SetDeadline(time.Now().Add(5 * time.Second))
		}
		return conn, err
	})
}

func serve(ctx context.Context, output, diagnostics io.Writer, linkFile string, qr bool, ready func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Load the bundled emulator before starting a daemon or exposing a link.
	if err := screen.Load(); err != nil {
		return err
	}
	client, err := rex.Connect()
	if err != nil {
		return err
	}
	defer client.Close()
	hello, err := client.Hello()
	if err != nil {
		return err
	}
	if !rex.CompatibleProtocol(hello.Version) {
		return fmt.Errorf("incompatible session protocol %d; existing sessions were preserved", hello.Version)
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	bridge, err := remote.Start(startup, rex.SocketPath(), remote.Options{StateDir: rex.Dir()})
	cancel()
	if err != nil {
		return fmt.Errorf("start encrypted gateway: %w", err)
	}
	defer bridge.Close()
	if err := saveLink(linkFile, bridge.Link()); err != nil {
		return err
	}
	if err := printConnectionLink(output, bridge.Link(), qr); err != nil {
		return err
	}
	ready(bridge.Link())
	fmt.Fprintf(diagnostics, "Retty ready · %s · session server PID %d\nConnection link saved to %s\n", hello.Host.Name, hello.PID, linkFile)
	select {
	case <-ctx.Done():
		fmt.Fprintln(diagnostics, "Gateway stopped; sessions continue in the background.")
		return nil
	case <-client.Closed():
		return errors.New("session server disconnected")
	}
}

func saveLink(path, link string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".connect-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = fmt.Fprintln(f, link); err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	return os.Rename(f.Name(), path)
}
