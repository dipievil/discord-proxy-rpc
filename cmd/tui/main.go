package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/tui"
)

func main() {
	addr := flag.String("addr", "", "proxy address as host:port or ws:// URL (default $PROXY_HOST:$PROXY_PORT, 127.0.0.1:8765)")
	token := flag.String("token", "", "bearer token (default $PROXY_TOKEN or $PROXY_AUTH_TOKEN)")
	wsPath := flag.String("ws-path", "", "proxy WebSocket path (default $PROXY_SERVER_WS_PATH, /ws)")
	flag.Parse()

	endpoint := tui.WSURL(tui.ResolveAddr(*addr), tui.ResolveWSPath(*wsPath))
	authToken := tui.ResolveToken(*token)

	program := tea.NewProgram(tui.New(endpoint, authToken), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
