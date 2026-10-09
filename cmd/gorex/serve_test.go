//go:build gorex_cli && (darwin || linux)

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHelpAndStatusNeverStartServer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOREX_DIR", dir)
	var out, diagnostics bytes.Buffer
	if err := run(context.Background(), []string{"help"}, &out, &diagnostics); err != nil || !strings.Contains(out.String(), "gorex serve") {
		t.Fatal("missing CLI help", err)
	}
	if err := run(context.Background(), []string{"status"}, &out, &diagnostics); err == nil {
		t.Fatal("missing server reported as healthy")
	}
	if _, err := os.Stat(filepath.Join(dir, "server.sock")); !os.IsNotExist(err) {
		t.Fatal("status started a daemon")
	}
	if err := run(context.Background(), []string{"stop"}, &out, &diagnostics); err != nil {
		t.Fatal("stopping an inactive gateway failed", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(canceled, []string{"serve"}, &out, &diagnostics); err == nil {
		t.Fatal("canceled start launched a gateway")
	}
	for _, args := range [][]string{{"unknown"}, {"serve", "extra"}, {"status", "--bad-flag"}} {
		if err := run(context.Background(), args, &out, &diagnostics); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
}

func TestSavedLinkIsPrivateAndUnrelatedStateIsPreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOREX_DIR", dir)
	path := filepath.Join(dir, "connect.txt")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveLink(path, "fixture-capability"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("connection capability is not private", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "fixture-capability\n" {
		t.Fatal("connection link was not atomically replaced")
	}
	if err := setDataDir(dir); err != nil || os.Getenv("GOREX_DIR") != dir {
		t.Fatal("data directory not propagated", err)
	}
	var out, diagnostics bytes.Buffer
	if err := run(context.Background(), []string{"link"}, &out, &diagnostics); err == nil {
		t.Fatal("invalid saved capability accepted")
	}
}
