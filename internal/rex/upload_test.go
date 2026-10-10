package rex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileUploadManifestRejectsEscapesDuplicatesAndOversizedBatches(t *testing.T) {
	valid := func() FileUploadRequest {
		return FileUploadRequest{SID: "pane", Roots: []string{"one"}, Entries: []FileUploadEntry{{Path: "one", Size: 1}}}
	}
	for _, edit := range []func(*FileUploadRequest){
		func(r *FileUploadRequest) { r.Entries[0].Size = -1 },
		func(r *FileUploadRequest) { r.Entries[0].Size = MaxUploadBytes + 1 },
		func(r *FileUploadRequest) { r.Entries[0].Directory = true },
		func(r *FileUploadRequest) { r.Roots = append(r.Roots, "one") },
		func(r *FileUploadRequest) { r.Entries = append(r.Entries, r.Entries[0]) },
		func(r *FileUploadRequest) { r.Entries = append(r.Entries, FileUploadEntry{Path: "one/child"}) },
		func(r *FileUploadRequest) { r.Entries[0].Path = "../escape" },
		func(r *FileUploadRequest) { r.Entries[0].Path = "outside" },
		func(r *FileUploadRequest) { r.Roots = []string{"one", "one/child"} },
		func(r *FileUploadRequest) {
			r.Entries[0].Size = MaxUploadBytes
			r.Roots = append(r.Roots, "two")
			r.Entries = append(r.Entries, FileUploadEntry{Path: "two", Size: 1})
		},
	} {
		r := valid()
		edit(&r)
		if _, err := ValidateFileUpload(r); err == nil {
			t.Fatal("invalid manifest accepted", r)
		}
	}
	r := valid()
	r.Entries[0].Size = 0
	if total, err := ValidateFileUpload(r); err != nil || total != 0 {
		t.Fatal("empty file rejected", err)
	}
	r.Entries[0].Directory = true
	if _, err := ValidateFileUpload(r); err != nil {
		t.Fatal("empty directory rejected", err)
	}
}

func TestFileUploadPreparationPreservesTopLevelLinkNameWithoutTraversingFolderLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.txt")
	if err := os.Symlink(target, alias); err != nil {
		t.Skip("symlink fixture unavailable", err)
	}
	req, sources, err := prepareFileUpload(context.Background(), "pane", []string{alias})
	if err != nil || len(req.Roots) != 1 || req.Roots[0] != "001/alias.txt" || len(sources) != 1 || sources[0].info.Size() != 4 {
		t.Fatal("top-level link changed file name or data", req, err)
	}
	if _, _, err := prepareFileUpload(context.Background(), "pane", []string{dir}); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatal("folder link was silently traversed", err)
	}
}

func TestFileUploadPreparationHonorsCancellation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("body"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := prepareFileUpload(ctx, "pane", []string{file}); err != context.Canceled {
		t.Fatal("cancelled manifest preparation continued", err)
	}
}
