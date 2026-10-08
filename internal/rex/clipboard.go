package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net"
	"time"
)

const MaxClipboardImage = 32 << 20
const maxClipboardPixels = 32_000_000
const ImagePasteTimeout = 45 * time.Second

// ImagePasteRequest is a bridge extension on its own authenticated connection.
// The session server never receives image bytes or requires a protocol upgrade.
type ImagePasteRequest struct {
	Op     string `json:"op"`
	SID    string `json:"sid"`
	Length int    `json:"length"`
}

func ValidateClipboardImage(data []byte) error {
	if len(data) == 0 || len(data) > MaxClipboardImage {
		return errors.New("图片过大或为空，请裁剪后重试（最多32MB）")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return errors.New("无法读取剪贴板图片")
	}
	if int64(config.Width)*int64(config.Height) > maxClipboardPixels {
		return errors.New("图片尺寸过大，请裁剪后重试")
	}
	return nil
}

// PasteImage copies an image to the desktop clipboard and sends Ctrl+V to sid.
// It never retries: after an ambiguous connection failure the desktop may have
// already pasted. A separate connection keeps polling and terminal input flowing.
func (c *Client) PasteImage(ctx context.Context, sid string, image []byte) error {
	if err := ValidateClipboardImage(image); err != nil {
		return err
	}
	if sid == "" || len(sid) > 128 {
		return errors.New("请先打开一个终端会话")
	}
	if c.dial == nil {
		return errors.New("当前连接不支持图片粘贴")
	}
	select {
	case <-c.Closed():
		return errors.New("桌面连接已断开")
	default:
	}
	ctx, cancel := context.WithTimeout(ctx, ImagePasteTimeout)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	req := ImagePasteRequest{Op: "paste-image", SID: sid, Length: len(image)}
	if err = json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(conn, 8192))
	var response Response
	if err = decoder.Decode(&response); err != nil {
		return imagePasteError(ctx, err)
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	var ready struct {
		Ready bool `json:"ready"`
	}
	if json.Unmarshal(response.Data, &ready) != nil || !ready.Ready {
		return errors.New("请更新桌面端以支持图片粘贴")
	}
	if _, err = io.Copy(conn, bytes.NewReader(image)); err != nil {
		return imagePasteError(ctx, err)
	}
	response = Response{}
	if err = decoder.Decode(&response); err != nil {
		return imagePasteError(ctx, err)
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}
func imagePasteError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return errors.New("图片粘贴超时，请确认桌面会话后再试")
	}
	return errors.New("图片粘贴连接中断，请确认桌面会话后再试")
}
