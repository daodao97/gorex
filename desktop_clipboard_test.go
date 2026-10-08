package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gorex/internal/rex"
	"image"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoteImageClipboardPrecedesCtrlVAndKeepsGeometry(t *testing.T) {
	var imageData bytes.Buffer
	png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	for _, scenario := range []string{"success", "closed pane", "clipboard denied"} {
		t.Run(scenario, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "gorex-paste-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "s")
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			copied := make(chan struct{})
			done := make(chan error, 1)
			clipboardCalls := 0
			go func() {
				control, e := ln.Accept()
				if e != nil {
					done <- e
					return
				}
				var request rex.Request
				if json.NewDecoder(control).Decode(&request) != nil || request.Op != "list" {
					control.Close()
					done <- errors.New("missing live session check")
					return
				}
				data, _ := json.Marshal([]rex.SessionInfo{{ID: "owned-pane", Exited: scenario == "closed pane"}})
				json.NewEncoder(control).Encode(rex.Response{ID: request.ID, Data: data})
				control.Close()
				if scenario == "closed pane" {
					done <- nil
					return
				}
				conn, e := ln.Accept()
				if e != nil {
					done <- e
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				reader := bufio.NewReader(conn)
				line, e := reader.ReadBytes('\n')
				if e != nil {
					done <- e
					return
				}
				var attach rex.Attach
				if json.Unmarshal(line, &attach) != nil || attach.SID != "owned-pane" || attach.Cols != 0 || attach.Rows != 0 {
					done <- errors.New("paste resized/wrong session")
					return
				}
				if scenario == "closed pane" {
					conn.Write([]byte("{\"ok\":false,\"error\":\"missing\"}\n"))
					done <- nil
					return
				}
				conn.Write([]byte("{\"ok\":true}\nreplayed output"))
				p := make([]byte, 1)
				n, e := reader.Read(p)
				if scenario == "clipboard denied" {
					if n > 0 {
						done <- errors.New("Ctrl+V sent after clipboard failure")
					} else {
						done <- nil
					}
					return
				}
				if e != nil || n != 1 || p[0] != 0x16 {
					done <- errors.New("Ctrl+V missing")
					return
				}
				select {
				case <-copied:
					done <- nil
				default:
					done <- errors.New("Ctrl+V preceded image")
				}
			}()
			err = pasteImageIntoSession(context.Background(), socket, "owned-pane", imageData.Bytes(), func(p []byte) error {
				clipboardCalls++
				if !bytes.Equal(p, imageData.Bytes()) {
					return errors.New("corrupt image")
				}
				if scenario == "clipboard denied" {
					return errors.New("denied")
				}
				close(copied)
				return nil
			})
			if (scenario == "success") != (err == nil) {
				t.Fatal("incorrect result", err)
			}
			if scenario == "closed pane" && clipboardCalls != 0 {
				t.Fatal("missing pane changed desktop clipboard")
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}
