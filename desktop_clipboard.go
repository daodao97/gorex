package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/egoist/mygo"
	"gorex/internal/rex"
	"io"
	"net"
	"time"
)

func pasteRemoteImage(ctx context.Context, sid string, image []byte) error {
	return pasteImageIntoSession(ctx, rex.SocketPath(), sid, image, mygo.Clipboard.WriteImage)
}

// Attach with a zero geometry before touching the clipboard, so missing/closed
// sessions fail without replacing it and mobile never resizes the desktop PTY.
func pasteImageIntoSession(ctx context.Context, socket, sid string, image []byte, writeClipboard func([]byte) error) error {
	if err := rex.ValidateClipboardImage(image); err != nil {
		return err
	}
	control, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
	if err != nil {
		return errors.New("桌面会话服务不可用")
	}
	client := rex.NewClient(control, nil)
	stopControl := context.AfterFunc(ctx, func() { client.Close() })
	sessions, err := client.List()
	stopControl()
	client.Close()
	if err != nil {
		return errors.New("无法确认桌面会话，请重试")
	}
	alive := false
	for _, session := range sessions {
		alive = alive || session.ID == sid && !session.Exited
	}
	if !alive {
		return errors.New("会话已结束，请重新选择会话")
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
	if err != nil {
		return errors.New("桌面会话服务不可用")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline := time.Now().Add(rex.ImagePasteTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	if json.NewEncoder(conn).Encode(rex.Attach{Op: "attach", SID: sid}) != nil {
		return errors.New("无法连接桌面会话")
	}
	reader := bufio.NewReaderSize(conn, 4096)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return errors.New("无法连接桌面会话")
	}
	var reply struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(line, &reply) != nil || !reply.OK {
		return errors.New("会话已结束，请重新选择会话")
	}
	// Drain replayed output without displaying it or creating a second terminal.
	go io.Copy(io.Discard, reader)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err = writeClipboard(image); err != nil {
		return errors.New("无法同步桌面剪贴板：" + err.Error())
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err = conn.Write([]byte{0x16}); err != nil {
		return errors.New("图片已同步，但无法发送 Ctrl+V，请在桌面会话中确认")
	}
	return nil
}
