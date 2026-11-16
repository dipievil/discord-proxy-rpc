package platform

import (
	"fmt"
	"os"
	"runtime"
)

// SlotCount is the number of discord-ipc-{0..9} sockets the platform resolver
// exposes. It is also the upper bound for discord.max_accounts.
const SlotCount = 10

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

	paths := make([]string, SlotCount)
	for i := 0; i < SlotCount; i++ {
		paths[i] = fmt.Sprintf("%s/discord-ipc-%d", base, i)
	}
	return paths
}

func windowsPaths() []string {
	paths := make([]string, SlotCount)
	for i := 0; i < SlotCount; i++ {
		paths[i] = fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
	}
	return paths
}
