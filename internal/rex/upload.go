package rex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxUploadBytes    = 1 << 30
	MaxUploadEntries  = 4096
	MaxUploadHeader   = 512 << 10
	FileUploadTimeout = 10 * time.Minute
)

var ErrFileUploadUnsupported = errors.New("远端尚不支持文件上传，请更新远端 Retty 桌面端或 CLI 网关")

// File uploads are a bridge extension, on an independent authenticated stream.
// Neither file bytes nor new requests reach the retained session daemon.
type FileUploadEntry struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Directory  bool   `json:"directory,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}

type FileUploadRequest struct {
	Op      string            `json:"op"`
	SID     string            `json:"sid"`
	Roots   []string          `json:"roots"`
	Entries []FileUploadEntry `json:"entries"`
}

type FileUploadReply struct {
	Ready bool     `json:"ready,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

func ValidUploadPath(name string) bool {
	return len(name) <= 4096 && name != "." && fs.ValidPath(name) &&
		!strings.ContainsAny(name, "\\\x00") && filepath.IsLocal(filepath.FromSlash(name))
}

func ValidateFileUpload(req FileUploadRequest) (int64, error) {
	bad := errors.New("文件上传清单无效（最多 4096 项、合计 1GB）")
	if req.SID == "" || len(req.SID) > 128 || len(req.Roots) == 0 || len(req.Roots) > 256 || len(req.Entries) == 0 || len(req.Entries) > MaxUploadEntries {
		return 0, bad
	}
	roots := map[string]bool{}
	for _, root := range req.Roots {
		if !ValidUploadPath(root) || roots[root] {
			return 0, bad
		}
		for prior := range roots {
			if strings.HasPrefix(root, prior+"/") || strings.HasPrefix(prior, root+"/") {
				return 0, bad
			}
		}
		roots[root] = true
	}
	seen := map[string]bool{}
	var total int64
	for _, e := range req.Entries {
		if !ValidUploadPath(e.Path) || e.Size < 0 || e.Size > MaxUploadBytes-total || e.Directory && e.Size != 0 {
			return 0, bad
		}
		if _, duplicate := seen[e.Path]; duplicate {
			return 0, bad
		}
		underRoot := false
		for root := range roots {
			underRoot = underRoot || e.Path == root || strings.HasPrefix(e.Path, root+"/")
		}
		if !underRoot {
			return 0, bad
		}
		seen[e.Path] = e.Directory
		total += e.Size
	}
	for root := range roots {
		if _, ok := seen[root]; !ok {
			return 0, bad
		}
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if directory, ok := seen[parent]; ok && !directory {
				return 0, bad
			}
		}
	}
	encoded, _ := json.Marshal(req)
	if len(encoded) > MaxUploadHeader {
		return 0, bad
	}
	return total, nil
}

type uploadSource struct {
	name string
	info fs.FileInfo
}

func prepareFileUpload(ctx context.Context, sid string, paths []string) (FileUploadRequest, []uploadSource, error) {
	req := FileUploadRequest{Op: "upload-files", SID: sid}
	var sources []uploadSource
	for _, source := range paths {
		if source == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(source)
		if err != nil {
			return req, nil, fmt.Errorf("无法读取拖入的文件：%w", err)
		}
		name := fmt.Sprintf("%03d/%s", len(req.Roots)+1, filepath.Base(filepath.Clean(source)))
		req.Roots = append(req.Roots, name)
		err = filepath.WalkDir(resolved, func(local string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return errors.New("文件夹中包含符号链接或特殊文件，请单独拖入链接指向的文件")
			}
			rel, err := filepath.Rel(resolved, local)
			if err != nil {
				return err
			}
			remote := name
			if rel != "." {
				remote += "/" + filepath.ToSlash(rel)
			}
			size := info.Size()
			if info.IsDir() {
				size = 0
			}
			req.Entries = append(req.Entries, FileUploadEntry{Path: remote, Size: size, Directory: info.IsDir(), Executable: info.Mode()&0111 != 0})
			sources = append(sources, uploadSource{local, info})
			if len(req.Entries) > MaxUploadEntries {
				return errors.New("上传内容超过 4096 项，请分批拖入")
			}
			return nil
		})
		if err != nil {
			return req, nil, err
		}
	}
	_, err := ValidateFileUpload(req)
	return req, sources, err
}

// UploadFiles streams files/folders to a private remote batch directory. Only a
// completed batch returns paths. Callers decide when to paste; this never types
// into a session, retries a transfer, or changes terminal geometry.
func (c *Client) UploadFiles(ctx context.Context, sid string, paths []string, progress func(sent, total int64)) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, FileUploadTimeout)
	defer cancel()
	go func() {
		select {
		case <-c.Closed():
			cancel()
		case <-ctx.Done():
		}
	}()
	req, sources, err := prepareFileUpload(ctx, sid, paths)
	if err != nil {
		return nil, err
	}
	total, _ := ValidateFileUpload(req)
	if progress != nil {
		progress(0, total)
	}
	if c.dial == nil {
		return nil, ErrFileUploadUnsupported
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, uploadError(ctx, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if err = json.NewEncoder(conn).Encode(req); err != nil {
		return nil, uploadError(ctx, err)
	}
	reader := bufio.NewReaderSize(conn, 1<<20)
	readReply := func() (FileUploadReply, error) {
		var response Response
		var reply FileUploadReply
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return reply, err
		}
		if err = json.Unmarshal(line, &response); err != nil {
			return reply, err
		}
		if response.Error != "" {
			if strings.HasPrefix(response.Error, "unknown op ") || response.Error == ErrFileUploadUnsupported.Error() {
				return reply, ErrFileUploadUnsupported
			}
			return reply, errors.New(response.Error)
		}
		if err = json.Unmarshal(response.Data, &reply); err != nil {
			return reply, err
		}
		return reply, nil
	}
	ready, err := readReply()
	if err != nil {
		return nil, uploadError(ctx, err)
	}
	if !ready.Ready {
		return nil, ErrFileUploadUnsupported
	}
	writer := &uploadProgressWriter{writer: conn, progress: progress, total: total, ctx: ctx}
	for i, entry := range req.Entries {
		if entry.Directory {
			continue
		}
		source := sources[i]
		file, err := os.Open(source.name)
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		if err == nil && (!info.Mode().IsRegular() || !os.SameFile(source.info, info) || info.Size() != entry.Size || !info.ModTime().Equal(source.info.ModTime())) {
			err = errors.New("文件在上传期间发生变化，请重试")
		}
		if err == nil {
			_, err = io.CopyN(writer, file, entry.Size)
		}
		if err == nil {
			info, err = file.Stat()
			if err == nil && (info.Size() != entry.Size || !info.ModTime().Equal(source.info.ModTime())) {
				err = errors.New("文件在上传期间发生变化，请重试")
			}
		}
		closeErr := file.Close()
		if err != nil {
			return nil, uploadError(ctx, err)
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if err = json.NewEncoder(conn).Encode(struct {
		Complete bool `json:"complete"`
	}{true}); err != nil {
		return nil, uploadError(ctx, err)
	}
	reply, err := readReply()
	if err != nil {
		return nil, uploadError(ctx, err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if len(reply.Paths) != len(req.Roots) {
		return nil, errors.New("远端未返回完整的文件路径")
	}
	for _, name := range reply.Paths {
		if !path.IsAbs(name) || strings.ContainsRune(name, 0) {
			return nil, errors.New("远端返回的文件路径无效")
		}
	}
	return reply.Paths, nil
}

type uploadProgressWriter struct {
	writer      io.Writer
	progress    func(int64, int64)
	sent, total int64
	last        time.Time
	ctx         context.Context
}

func (w *uploadProgressWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(p)
	w.sent += int64(n)
	if w.progress != nil && (w.sent == w.total || time.Since(w.last) >= 100*time.Millisecond) {
		w.progress(w.sent, w.total)
		w.last = time.Now()
	}
	return n, err
}

func uploadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("文件上传连接中断，请重试")
	}
	return err
}
