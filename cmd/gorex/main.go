//go:build gorex_cli && (darwin || linux)

// GoRex CLI exposes the existing session server through its encrypted bridge.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gorex/internal/rex"
)

var version = "dev"
var commit = "unknown"

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("[gorex] ")
	if handled, err := helper(os.Args[1:], os.Stdin); handled {
		if err != nil {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gorex:", err)
		os.Exit(1)
	}
}

// Session launchers invoke their owning executable for hooks and Codex helpers,
// just as the GUI executable does. Hook and Codex helpers never initialize the
// UI or start a server.
func helper(args []string, input io.Reader) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "-gateway":
		if len(args) != 2 {
			return true, errors.New("invalid gateway arguments")
		}
		if err := setDataDir(""); err != nil {
			return true, err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err := runGateway(ctx, io.Discard, os.Stderr, args[1], false)
		if err != nil {
			log.Print(err)
		}
		return true, err
	case "-server":
		if len(args) != 1 {
			return true, errors.New("invalid server arguments")
		}
		if err := setDataDir(""); err != nil {
			return true, err
		}
		err := rex.Serve()
		if err != nil {
			log.Print(err)
		}
		return true, err
	case "-agent-hook":
		if len(args) != 2 {
			return true, errors.New("invalid hook arguments")
		}
		rex.RunAgentHook(args[1], input)
		return true, nil
	case "-codex-bridge", "-codex-proxy":
		if len(args) != 4 {
			return true, errors.New("invalid Codex arguments")
		}
		pid, err := rex.ParseBridgePID(args[2])
		if err == nil {
			if args[0] == "-codex-bridge" {
				err = rex.StartCodexBridge(args[1], pid, args[3])
			} else {
				err = rex.RunCodexProxy(args[1], pid, args[3])
			}
		}
		return true, err
	case "-codex-watch":
		if len(args) != 3 {
			return true, errors.New("invalid Codex arguments")
		}
		pid, err := rex.ParseBridgePID(args[2])
		if err == nil {
			err = rex.WatchCodexThread(args[1], pid)
		}
		return true, err
	}
	return false, nil
}
