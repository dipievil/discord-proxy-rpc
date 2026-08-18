package platform

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestIPCPathsReturns10Paths(t *testing.T) {
	paths := IPCPaths()
	if len(paths) != ipcNameCount {
		t.Errorf("expected %d paths, got %d", ipcNameCount, len(paths))
	}
}

func TestIPCPathsContainPrefix(t *testing.T) {
	paths := IPCPaths()
	for i, p := range paths {
		if !strings.Contains(p, "discord-ipc-") {
			t.Errorf("path %d missing 'discord-ipc-' prefix: %s", i, p)
		}
	}
}

func TestIPCPathsSequentialIndices(t *testing.T) {
	paths := IPCPaths()
	for i, p := range paths {
		expected := fmt.Sprintf("discord-ipc-%d", i)
		if !strings.Contains(p, expected) {
			t.Errorf("path %d expected to contain %q, got %q", i, expected, p)
		}
	}
}

func TestWindowsPathsUseNamedPipeFormat(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only test")
	}
	paths := IPCPaths()
	for i, p := range paths {
		expected := `\\?\pipe\discord-ipc-` + fmt.Sprintf("%d", i)
		if p != expected {
			t.Errorf("path %d: expected %q, got %q", i, expected, p)
		}
	}
}

func TestUnixPathsUseXDGOrFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}

	orig, had := os.LookupEnv("XDG_RUNTIME_DIR")
	if had {
		defer os.Setenv("XDG_RUNTIME_DIR", orig)
	} else {
		defer os.Unsetenv("XDG_RUNTIME_DIR")
	}

	t.Run("with XDG_RUNTIME_DIR", func(t *testing.T) {
		os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
		paths := IPCPaths()
		for i, p := range paths {
			expected := fmt.Sprintf("/run/user/1000/discord-ipc-%d", i)
			if p != expected {
				t.Errorf("path %d: expected %q, got %q", i, expected, p)
			}
		}
	})

	t.Run("without XDG_RUNTIME_DIR falls back to /tmp", func(t *testing.T) {
		os.Unsetenv("XDG_RUNTIME_DIR")
		paths := IPCPaths()
		for i, p := range paths {
			expected := fmt.Sprintf("/tmp/discord-ipc-%d", i)
			if p != expected {
				t.Errorf("path %d: expected %q, got %q", i, expected, p)
			}
		}
	})
}
