// Command ipc-capture records every raw frame the local Discord IPC socket
// sends for a bounded period, and reports whether any of them carries Rich
// Presence (an ACTIVITY frame).
//
// It exists to answer one question with evidence: does Discord's local IPC
// socket push another application's Rich Presence to a connected client? It
// does not today, which is why the proxy serves an explicit "none" activity
// source (see docs/implementation/SPEC.md, "Activity source"). Run this on the
// machine running the Discord desktop client while a game is open and share the
// output.
//
// Usage:
//
//	go run ./cmd/ipc-capture -client-id <discord-app-id> -duration 60s -out capture.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	presence "github.com/dragsbruh/gopresence"
	gopresenceipc "github.com/dragsbruh/gopresence/ipc"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/platform"
)

// Discord RPC opcodes (mirrors internal/ipc).
const (
	opHandshake int32 = 0
	opFrame     int32 = 1
	opClose     int32 = 2
	opPing      int32 = 3
	opPong      int32 = 4
)

func main() {
	clientID := flag.String("client-id", "", "Discord application (client) ID used for the IPC handshake")
	duration := flag.Duration("duration", 60*time.Second, "how long to capture before exiting")
	out := flag.String("out", "", "optional file for the raw capture (JSON Lines)")
	flag.Parse()

	if *clientID == "" {
		fmt.Fprintln(os.Stderr, "error: -client-id is required")
		flag.Usage()
		os.Exit(2)
	}

	summary, err := capture(*clientID, *duration, *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printSummary(os.Stdout, summary)
}

// captureFrame is one IPC frame observed on the socket.
type captureFrame struct {
	Time       string `json:"time"`
	Opcode     int32  `json:"opcode"`
	OpcodeName string `json:"opcode_name"`
	Command    string `json:"command,omitempty"`
	Event      string `json:"event,omitempty"`
	Activity   bool   `json:"activity"`
	Payload    string `json:"payload"`
}

// captureSummary aggregates a capture so the result can be read at a glance.
type captureSummary struct {
	SocketPath string
	Frames     int
	Opcodes    map[string]int
	Events     map[string]int
	Activity   int
}

func newCaptureSummary(socketPath string) *captureSummary {
	return &captureSummary{
		SocketPath: socketPath,
		Opcodes:    map[string]int{},
		Events:     map[string]int{},
	}
}

func (s *captureSummary) record(frame captureFrame) {
	s.Frames++
	s.Opcodes[frame.OpcodeName]++
	if frame.Event != "" {
		s.Events[frame.Event]++
	}
	if frame.Activity {
		s.Activity++
	}
}

// opcodeName renders a Discord RPC opcode for humans and the JSON capture.
func opcodeName(op int32) string {
	switch op {
	case opHandshake:
		return "handshake"
	case opFrame:
		return "frame"
	case opClose:
		return "close"
	case opPing:
		return "ping"
	case opPong:
		return "pong"
	default:
		return fmt.Sprintf("opcode-%d", op)
	}
}

// classify turns a raw IPC frame into a captureFrame. It is pure, so the
// classification logic is unit-testable without a Discord socket.
func classify(opcode int32, data []byte, at time.Time) captureFrame {
	frame := captureFrame{
		Time:       at.UTC().Format(time.RFC3339Nano),
		Opcode:     opcode,
		OpcodeName: opcodeName(opcode),
		Payload:    string(data),
	}

	if opcode == opFrame {
		if parsed, err := presence.ParseFrame(data); err == nil {
			frame.Command = parsed.Command
			frame.Event = parsed.Event
			frame.Activity = strings.Contains(strings.ToUpper(parsed.Event), "ACTIVITY")
		}
	}

	return frame
}

// dial connects to the first available discord-ipc-{0..9} socket.
func dial(conn *gopresenceipc.Client) (string, error) {
	paths := platform.IPCPaths()
	var lastErr error
	for _, path := range paths {
		if err := conn.Connect(path, 2*time.Second); err != nil {
			lastErr = err
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("no Discord IPC socket accepted a connection in %v: %w", paths, lastErr)
}

func capture(clientID string, duration time.Duration, out string) (*captureSummary, error) {
	conn := gopresenceipc.New()
	path, err := dial(conn)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rpc := presence.NewFrom(conn)
	ready, err := rpc.Login(clientID)
	if err != nil {
		return nil, fmt.Errorf("handshake on %s: %w", path, err)
	}

	if ready != nil {
		fmt.Fprintf(os.Stderr, "connected to %s as %s (capturing for %s)\n", path, ready.User.Username, duration)
	}

	var sink = io.Discard
	if out != "" {
		f, err := os.Create(out)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", out, err)
		}
		defer f.Close()
		sink = f
	}
	writer := bufio.NewWriter(sink)
	defer writer.Flush()

	summary := newCaptureSummary(path)
	raw := rpc.GetIPC()

	// Closing the socket after the capture window unblocks the pending Read.
	timer := time.AfterFunc(duration, func() { _ = raw.Close() })

	var writeErr error
	for {
		opcode, data, err := raw.Read()
		if err != nil {
			break
		}

		frame := classify(opcode, data, time.Now())
		summary.record(frame)

		if out != "" {
			line, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			if _, err := writer.Write(line); err != nil {
				writeErr = err
				break
			}
			if err := writer.WriteByte('\n'); err != nil {
				writeErr = err
				break
			}
		}
	}
	timer.Stop()

	if writeErr != nil {
		return nil, fmt.Errorf("write capture output: %w", writeErr)
	}
	return summary, nil
}

func printSummary(w io.Writer, s *captureSummary) {
	fmt.Fprintln(w, "=== IPC capture summary ===")
	fmt.Fprintf(w, "socket:          %s\n", s.SocketPath)
	fmt.Fprintf(w, "frames:          %d\n", s.Frames)
	fmt.Fprintf(w, "opcodes:         %s\n", formatCounts(s.Opcodes))
	fmt.Fprintf(w, "events:          %s\n", formatCounts(s.Events))
	fmt.Fprintf(w, "activity frames: %d\n", s.Activity)
	fmt.Fprintln(w)

	if s.Activity == 0 {
		fmt.Fprintln(w, "No ACTIVITY frame was observed. Discord's local IPC socket does not")
		fmt.Fprintln(w, "push another application's Rich Presence, so the proxy has no activity")
		fmt.Fprintln(w, "source via IPC and correctly reports source=\"none\".")
		return
	}

	fmt.Fprintf(w, "Found %d ACTIVITY frame(s). Discord does push Rich Presence on this setup;\n", s.Activity)
	fmt.Fprintln(w, "share this capture so the ACTIVITY frame can be wired into the proxy source.")
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, " ")
}
