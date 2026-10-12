package web

import (
	"io/fs"
	"testing"
)

func TestFSContainsIndexHTML(t *testing.T) {
	data, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatalf("ReadFile(index.html): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("index.html is empty")
	}
}

func TestFSIsDirectory(t *testing.T) {
	info, err := fs.Stat(FS, ".")
	if err != nil {
		t.Fatalf("Stat(.): %v", err)
	}
	if !info.IsDir() {
		t.Fatal("FS root is not a directory")
	}
}

func TestFSListsFiles(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("ReadDir(.): %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("FS has no embedded files")
	}

	found := false
	for _, e := range entries {
		if e.Name() == "index.html" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("index.html not found in embedded FS")
	}
}
