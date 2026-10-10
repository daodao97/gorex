package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"retty/internal/rex"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fileUploadFixture(t *testing.T) (*rex.Client, *Bridge, string, *atomic.Bool) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "retty-upload-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	alive := &atomic.Bool{}
	alive.Store(true)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				var req rex.Request
				if json.NewDecoder(conn).Decode(&req) != nil {
					return
				}
				if req.Op != "list" {
					t.Error("upload sent a non-read-only operation to the daemon", req.Op)
					return
				}
				data, _ := json.Marshal([]rex.SessionInfo{{ID: "pane", PID: 999, Cols: 91, Rows: 27, Exited: !alive.Load()}})
				json.NewEncoder(conn).Encode(rex.Response{ID: req.ID, Data: data})
			}()
		}
	}()
	control, peer := net.Pipe()
	t.Cleanup(func() { control.Close(); peer.Close() })
	b := &Bridge{uploadDir: filepath.Join(dir, "uploads"), uploadGate: make(chan struct{}, 2), imageContext: context.Background()}
	c := rex.NewClient(control, func(context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		go func() { defer server.Close(); b.serveConnection(server, socket) }()
		return client, nil
	})
	t.Cleanup(func() { c.Close() })
	return c, b, socket, alive
}

func waitUploadCleanup(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) || err == nil && len(entries) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("incomplete upload was retained", entries, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFileUploadStreamsFoldersAndSameNamedFilesIntoPrivateBatch(t *testing.T) {
	c, _, _, _ := fileUploadFixture(t)
	local := t.TempDir()
	folder := filepath.Join(local, "中文 ' folder")
	if err := os.MkdirAll(filepath.Join(folder, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("a\x00b\xff"), 1<<18)
	if err := os.WriteFile(filepath.Join(folder, "run.sh"), data, 0700); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, parent := range []string{"a", "b"} {
		os.Mkdir(filepath.Join(local, parent), 0700)
		name := filepath.Join(local, parent, "same.txt")
		os.WriteFile(name, []byte(parent), 0600)
		sources = append(sources, name)
	}
	sources = append(sources, folder)
	var last, total int64
	paths, err := c.UploadFiles(context.Background(), "pane", sources, func(sent, size int64) {
		if sent < last {
			t.Error("progress moved backwards")
		}
		last, total = sent, size
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || paths[0] == paths[1] || filepath.Base(paths[0]) != "same.txt" || filepath.Base(paths[2]) != filepath.Base(folder) {
		t.Fatal("root names or isolation changed", paths)
	}
	for i, want := range []string{"a", "b"} {
		got, err := os.ReadFile(paths[i])
		if err != nil || string(got) != want {
			t.Fatal("same-name file overwritten", err)
		}
		st, _ := os.Stat(paths[i])
		if st.Mode().Perm() != 0600 {
			t.Fatal("upload is not private")
		}
	}
	got, err := os.ReadFile(filepath.Join(paths[2], "run.sh"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("binary upload changed", err)
	}
	st, _ := os.Stat(filepath.Join(paths[2], "run.sh"))
	if st.Mode().Perm() != 0700 {
		t.Fatal("executable permission not retained privately")
	}
	st, err = os.Stat(filepath.Join(paths[2], "empty"))
	if err != nil || !st.IsDir() {
		t.Fatal("empty folder was lost", err)
	}
	if last != total || total != int64(len(data)+2) {
		t.Fatal("progress did not finish", last, total)
	}
}

func TestFileUploadCancellationAndEndedSessionsDoNotCommit(t *testing.T) {
	c, b, _, alive := fileUploadFixture(t)
	file := filepath.Join(t.TempDir(), "large.bin")
	os.WriteFile(file, bytes.Repeat([]byte("x"), 1<<20), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	paths, err := c.UploadFiles(ctx, "pane", []string{file}, func(sent, _ int64) {
		if sent > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || len(paths) != 0 {
		t.Fatal("cancelled upload committed", err, paths)
	}
	waitUploadCleanup(t, b.uploadDir)
	alive.Store(false)
	if paths, err := c.UploadFiles(context.Background(), "pane", []string{file}, nil); err == nil || len(paths) != 0 || !strings.Contains(err.Error(), "会话已结束") {
		t.Fatal("ended session accepted upload", err)
	}
	waitUploadCleanup(t, b.uploadDir)
}

func TestFileUploadRejectsTraversalAndTruncatedBatches(t *testing.T) {
	_, b, socket, _ := fileUploadFixture(t)
	for _, name := range []string{"../escape", "/absolute", "root/../../escape", "root\\escape"} {
		client, server := net.Pipe()
		client.SetDeadline(time.Now().Add(time.Second))
		go func() { defer server.Close(); b.serveConnection(server, socket) }()
		req := rex.FileUploadRequest{Op: "upload-files", SID: "pane", Roots: []string{name}, Entries: []rex.FileUploadEntry{{Path: name, Size: 1}}}
		json.NewEncoder(client).Encode(req)
		var response rex.Response
		if err := json.NewDecoder(client).Decode(&response); err != nil || response.Error == "" {
			t.Fatal("unsafe path accepted", name, err)
		}
		client.Close()
	}
	client, server := net.Pipe()
	client.SetDeadline(time.Now().Add(time.Second))
	done := make(chan struct{})
	go func() { defer close(done); defer server.Close(); b.serveConnection(server, socket) }()
	json.NewEncoder(client).Encode(rex.FileUploadRequest{Op: "upload-files", SID: "pane", Roots: []string{"one"}, Entries: []rex.FileUploadEntry{{Path: "one", Size: 100}}})
	var response rex.Response
	if err := json.NewDecoder(client).Decode(&response); err != nil || response.Error != "" {
		t.Fatal(err, response.Error)
	}
	client.Write([]byte("partial"))
	client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("truncated transfer did not stop")
	}
	waitUploadCleanup(t, b.uploadDir)
}

func TestFileUploadRequiresCompletionAndLiveSessionAfterBody(t *testing.T) {
	_, b, socket, alive := fileUploadFixture(t)
	for _, complete := range []bool{false, true} {
		alive.Store(true)
		client, server := net.Pipe()
		client.SetDeadline(time.Now().Add(time.Second))
		done := make(chan struct{})
		go func() { defer close(done); defer server.Close(); b.serveConnection(server, socket) }()
		json.NewEncoder(client).Encode(rex.FileUploadRequest{Op: "upload-files", SID: "pane", Roots: []string{"one"}, Entries: []rex.FileUploadEntry{{Path: "one", Size: 1}}})
		reader := bufio.NewReader(client)
		reader.ReadString('\n')
		client.Write([]byte("a"))
		if complete {
			alive.Store(false)
		}
		json.NewEncoder(client).Encode(map[string]bool{"complete": complete})
		line, err := reader.ReadString('\n')
		var response rex.Response
		if err != nil || json.Unmarshal([]byte(line), &response) != nil || response.Error == "" {
			t.Fatal("incomplete/dead session batch committed", err, line)
		}
		client.Close()
		<-done
		waitUploadCleanup(t, b.uploadDir)
	}
}

func TestFileUploadOldBridgeAndBusyReceiverSendNoFileBytes(t *testing.T) {
	c, b, _, _ := fileUploadFixture(t)
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("secret"), 0600)
	b.uploadDir = ""
	if _, err := c.UploadFiles(context.Background(), "pane", []string{file}, nil); !errors.Is(err, rex.ErrFileUploadUnsupported) && (err == nil || err.Error() != rex.ErrFileUploadUnsupported.Error()) {
		t.Fatal("unsupported bridge not reported", err)
	}
	// A legacy daemon answers the extension with unknown-op before accepting a body.
	control, peer := net.Pipe()
	defer peer.Close()
	legacy := rex.NewClient(control, func(context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			reader.ReadString('\n')
			json.NewEncoder(server).Encode(rex.Response{Error: `unknown op "upload-files"`})
			server.SetReadDeadline(time.Now().Add(time.Second))
			if n, err := reader.Read(make([]byte, 1)); n != 0 || err != io.EOF {
				t.Error("legacy bridge received file bytes", n, err)
			}
		}()
		return client, nil
	})
	defer legacy.Close()
	if _, err := legacy.UploadFiles(context.Background(), "pane", []string{file}, nil); !errors.Is(err, rex.ErrFileUploadUnsupported) {
		t.Fatal("legacy bridge not reported", err)
	}
	b.uploadDir = filepath.Join(t.TempDir(), "uploads")
	b.uploadGate <- struct{}{}
	b.uploadGate <- struct{}{}
	if _, err := c.UploadFiles(context.Background(), "pane", []string{file}, nil); err == nil || !strings.Contains(err.Error(), "正在接收") {
		t.Fatal("busy receiver accepted upload", err)
	}
}

func TestFileUploadChangedSourceDoesNotPublishPartialBatch(t *testing.T) {
	c, b, _, _ := fileUploadFixture(t)
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("original"), 0600)
	paths, err := c.UploadFiles(context.Background(), "pane", []string{file}, func(sent, _ int64) {
		if sent == 0 {
			os.WriteFile(file, []byte("changed contents"), 0600)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "发生变化") || len(paths) != 0 {
		t.Fatal("changed source was published", err, paths)
	}
	waitUploadCleanup(t, b.uploadDir)
}
