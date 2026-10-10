package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"retty/internal/rex"
	"time"
)

func uploadSessionAlive(ctx context.Context, socket, sid string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return errors.New("远端会话服务不可用")
	}
	client := rex.NewClient(conn, nil)
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	infos, err := client.List()
	if err != nil {
		return errors.New("无法确认远端会话，请重试")
	}
	for _, info := range infos {
		if info.ID == sid && !info.Exited {
			return ctx.Err()
		}
	}
	return errors.New("远端会话已结束，请重新选择会话")
}

// Receive into a new private directory. Failure/cancellation removes only this
// batch. Files become available together after the client's completion marker;
// completed uploads stay available to programs even after the pane is closed.
func (b *Bridge) serveFileUpload(conn net.Conn, reader *bufio.Reader, socket string, req rex.FileUploadRequest) {
	encoder := json.NewEncoder(conn)
	fail := func(err error) { _ = encoder.Encode(rex.Response{Error: err.Error()}) }
	if b.uploadDir == "" {
		fail(rex.ErrFileUploadUnsupported)
		return
	}
	if _, err := rex.ValidateFileUpload(req); err != nil {
		fail(err)
		return
	}
	select {
	case b.uploadGate <- struct{}{}:
		defer func() { <-b.uploadGate }()
	default:
		fail(errors.New("远端正在接收文件，请稍后重试"))
		return
	}
	parent := b.imageContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, rex.FileUploadTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if err := uploadSessionAlive(ctx, socket, req.SID); err != nil {
		fail(err)
		return
	}
	base, err := filepath.Abs(b.uploadDir)
	if err != nil {
		fail(err)
		return
	}
	if err = os.MkdirAll(base, 0700); err != nil {
		fail(errors.New("无法创建远端上传目录"))
		return
	}
	batch, err := os.MkdirTemp(base, "batch-")
	if err != nil {
		fail(errors.New("无法创建远端上传目录"))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(batch)
		}
	}()
	root, err := os.OpenRoot(batch)
	if err != nil {
		fail(err)
		return
	}
	defer root.Close()
	ready, _ := json.Marshal(rex.FileUploadReply{Ready: true})
	if encoder.Encode(rex.Response{Data: ready}) != nil {
		return
	}
	for _, entry := range req.Entries {
		if ctx.Err() != nil {
			return
		}
		name := filepath.FromSlash(entry.Path)
		if entry.Directory {
			if err = root.MkdirAll(name, 0700); err != nil {
				return
			}
			continue
		}
		if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return
		}
		mode := os.FileMode(0600)
		if entry.Executable {
			mode = 0700
		}
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return
		}
		_, err = io.CopyN(file, reader, entry.Size)
		closeErr := file.Close()
		if err != nil {
			return
		}
		if closeErr != nil {
			return
		}
	}
	line, err := reader.ReadSlice('\n')
	var complete struct {
		Complete bool `json:"complete"`
	}
	if err != nil || json.Unmarshal(line, &complete) != nil || !complete.Complete {
		fail(errors.New("文件上传未完成"))
		return
	}
	// Peer cancellation must also interrupt final session validation.
	go func() { var extra [1]byte; _, _ = reader.Read(extra[:]); cancel() }()
	if err = uploadSessionAlive(ctx, socket, req.SID); err != nil {
		fail(err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	paths := make([]string, len(req.Roots))
	for i, name := range req.Roots {
		paths[i] = filepath.Join(batch, filepath.FromSlash(name))
	}
	data, _ := json.Marshal(rex.FileUploadReply{Paths: paths})
	if err = encoder.Encode(rex.Response{Data: data}); err == nil {
		committed = true
	}
}
