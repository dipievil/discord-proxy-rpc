package platform

import (
	"fmt"
	"os"
	"runtime"
)

const ipcNameCount = 10

func IPCPaths() []string {
	switch runtime.GOOS {
	case "windows":
		return windowsPaths()
	default:
		return unixPaths()
	}
}

func unixPaths() []string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/tmp"
	}

	paths := make([]string, ipcNameCount)
	for i := 0; i < ipcNameCount; i++ {
		paths[i] = fmt.Sprintf("%s/discord-ipc-%d", base, i)
	}
	return paths
}

func windowsPaths() []string {
	paths := make([]string, ipcNameCount)
	for i := 0; i < ipcNameCount; i++ {
		paths[i] = fmt.Sprintf(`\\?\pipe\discord-ipc-%d`, i)
	}
	return paths
}
