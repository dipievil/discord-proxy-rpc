// Command example-consumer connects to a running discord-proxy-rpc instance,
// subscribes to presence/state updates, and prints them as they arrive.
//
// Usage:
//
//	example-consumer [-addr ws://127.0.0.1:8765/ws] [-token <token>]
//
// The token may also be provided via the PROXY_AUTH_TOKEN environment
// variable. Press Ctrl-C to exit cleanly.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultAddr      = "ws://127.0.0.1:8765/ws"
	pongWait         = 90 * time.Second
	handshakeTimeout = 10 * time.Second
	reconnectMin     = time.Second
	reconnectMax     = 30 * time.Second
)

func main() {
	addr := flag.String("addr", defaultAddr, "proxy WebSocket address")
	token := flag.String("token", os.Getenv("PROXY_AUTH_TOKEN"), "bearer auth token (falls back to $PROXY_AUTH_TOKEN)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, *token); err != nil {
		log.Fatal(err)
	}
}

// run keeps a consume loop alive until ctx is cancelled, backing off
// exponentially between failed connection attempts.
func run(ctx context.Context, addr, token string) error {
	backoff := reconnectMin
	for {
		received, err := consume(ctx, addr, token)
		if ctx.Err() != nil {
			log.Println("shutting down")
			return nil
		}
		if received {
			backoff = reconnectMin
		}
		log.Printf("connection lost: %v; retrying in %s", err, backoff)
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return nil
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > reconnectMax {
			backoff = reconnectMax
		}
	}
}

// consume performs one session: dial, authenticate, subscribe, request the
// current snapshot, then print server messages until the connection drops.
// It reports whether at least one message was received before the error.
func consume(ctx context.Context, addr, token string) (bool, error) {
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	dialer := websocket.Dialer{HandshakeTimeout: handshakeTimeout}
	conn, _, err := dialer.DialContext(ctx, addr, header)
	if err != nil {
		return false, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()
	log.Printf("connected to %s", addr)

	// Close the connection when the context is cancelled so the blocking
	// read loop below returns promptly on Ctrl-C.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client shutting down"),
				time.Now().Add(time.Second),
			)
			conn.Close()
		case <-done:
		}
	}()

	if err := writeClientMessage(conn, map[string]any{
		"type":   msgTypeSubscribe,
		"events": []string{msgTypePresence, msgTypeState, msgTypeCurrent},
	}); err != nil {
		return false, err
	}
	if err := writeClientMessage(conn, map[string]any{"type": msgTypeGetCurrent}); err != nil {
		return false, err
	}

	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return false, fmt.Errorf("setting read deadline: %w", err)
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	received := false
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return received, fmt.Errorf("reading message: %w", err)
		}
		received = true

		msg, err := parseServerMessage(data)
		if err != nil {
			log.Printf("skipping malformed message: %v", err)
			continue
		}
		line, err := formatMessage(msg)
		if err != nil {
			log.Printf("skipping undecodable message: %v", err)
			continue
		}
		fmt.Println(line)
	}
}

func writeClientMessage(conn *websocket.Conn, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encoding %v message: %w", msg["type"], err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("sending %v message: %w", msg["type"], err)
	}
	return nil
}
