package sandbox

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractWorkspaceArchiveWritesOnlyRegularFilesInsideWorkspace(t *testing.T) {
	destination := t.TempDir()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: "./src", Typeflag: tar.TypeDir, Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	content := []byte("package main\n")
	if err := writer.WriteHeader(&tar.Header{Name: "src/main.go", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractWorkspaceArchive(&archive, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "src", "main.go"))
	if err != nil || string(data) != string(content) {
		t.Fatalf("collected workspace content=%q err=%v", data, err)
	}
	info, err := os.Stat(filepath.Join(destination, "src", "main.go"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("executable mode not preserved: mode=%v err=%v", info.Mode(), err)
	}
}

func TestExtractWorkspaceArchiveRejectsTraversalLinksAndSpecialFiles(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"},
		{Name: "device", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3},
	} {
		t.Run(header.Name, func(t *testing.T) {
			var archive bytes.Buffer
			writer := tar.NewWriter(&archive)
			if err := writer.WriteHeader(&header); err != nil {
				t.Fatal(err)
			}
			if header.Size > 0 {
				_, _ = writer.Write([]byte("x"))
			}
			_ = writer.Close()
			destination := t.TempDir()
			if err := extractWorkspaceArchive(&archive, destination); err == nil {
				t.Fatal("accepted unsafe workspace archive entry")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(destination), "escape")); !os.IsNotExist(err) {
				t.Fatalf("archive escaped destination: %v", err)
			}
		})
	}
}
