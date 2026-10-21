package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestClassifyFrame(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name         string
		opcode       int32
		payload      string
		wantOpcode   string
		wantCommand  string
		wantEvent    string
		wantActivity bool
	}{
		{
			name:         "activity join frame is flagged",
			opcode:       opFrame,
			payload:      `{"cmd":"DISPATCH","evt":"ACTIVITY_JOIN","data":{},"nonce":null}`,
			wantOpcode:   "frame",
			wantCommand:  "DISPATCH",
			wantEvent:    "ACTIVITY_JOIN",
			wantActivity: true,
		},
		{
			name:         "activity spectate frame is flagged",
			opcode:       opFrame,
			payload:      `{"cmd":"DISPATCH","evt":"ACTIVITY_SPECTATE","data":{}}`,
			wantOpcode:   "frame",
			wantCommand:  "DISPATCH",
			wantEvent:    "ACTIVITY_SPECTATE",
			wantActivity: true,
		},
		{
			name:         "ready frame is not an activity",
			opcode:       opFrame,
			payload:      `{"cmd":"DISPATCH","evt":"READY","data":{}}`,
			wantOpcode:   "frame",
			wantCommand:  "DISPATCH",
			wantEvent:    "READY",
			wantActivity: false,
		},
		{
			name:         "unparseable frame keeps the payload",
			opcode:       opFrame,
			payload:      `not json`,
			wantOpcode:   "frame",
			wantActivity: false,
		},
		{
			name:       "close frame",
			opcode:     opClose,
			payload:    ``,
			wantOpcode: "close",
		},
		{
			name:       "ping frame",
			opcode:     opPing,
			payload:    ``,
			wantOpcode: "ping",
		},
		{
			name:       "unknown opcode",
			opcode:     99,
			payload:    ``,
			wantOpcode: "opcode-99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := classify(tt.opcode, []byte(tt.payload), at)

			if frame.OpcodeName != tt.wantOpcode {
				t.Errorf("OpcodeName = %q, want %q", frame.OpcodeName, tt.wantOpcode)
			}
			if frame.Command != tt.wantCommand {
				t.Errorf("Command = %q, want %q", frame.Command, tt.wantCommand)
			}
			if frame.Event != tt.wantEvent {
				t.Errorf("Event = %q, want %q", frame.Event, tt.wantEvent)
			}
			if frame.Activity != tt.wantActivity {
				t.Errorf("Activity = %v, want %v", frame.Activity, tt.wantActivity)
			}
			if frame.Payload != tt.payload {
				t.Errorf("Payload = %q, want %q", frame.Payload, tt.payload)
			}
			if frame.Time != at.Format(time.RFC3339Nano) {
				t.Errorf("Time = %q, want %q", frame.Time, at.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestCaptureSummaryRecord(t *testing.T) {
	summary := newCaptureSummary("/tmp/discord-ipc-0")

	summary.record(classify(opFrame, []byte(`{"cmd":"DISPATCH","evt":"READY"}`), time.Now()))
	summary.record(classify(opPing, nil, time.Now()))
	summary.record(classify(opFrame, []byte(`{"cmd":"DISPATCH","evt":"ACTIVITY_JOIN"}`), time.Now()))

	if summary.Frames != 3 {
		t.Errorf("Frames = %d, want 3", summary.Frames)
	}
	if summary.Opcodes["frame"] != 2 {
		t.Errorf("Opcodes[frame] = %d, want 2", summary.Opcodes["frame"])
	}
	if summary.Opcodes["ping"] != 1 {
		t.Errorf("Opcodes[ping] = %d, want 1", summary.Opcodes["ping"])
	}
	if summary.Events["READY"] != 1 {
		t.Errorf("Events[READY] = %d, want 1", summary.Events["READY"])
	}
	if summary.Events["ACTIVITY_JOIN"] != 1 {
		t.Errorf("Events[ACTIVITY_JOIN] = %d, want 1", summary.Events["ACTIVITY_JOIN"])
	}
	if summary.Activity != 1 {
		t.Errorf("Activity = %d, want 1", summary.Activity)
	}
}

func TestPrintSummaryNoActivity(t *testing.T) {
	summary := newCaptureSummary("/tmp/discord-ipc-0")
	summary.record(classify(opFrame, []byte(`{"cmd":"DISPATCH","evt":"READY"}`), time.Now()))

	var out bytes.Buffer
	printSummary(&out, summary)

	got := out.String()
	if !strings.Contains(got, "activity frames: 0") {
		t.Errorf("summary missing activity count:\n%s", got)
	}
	if !strings.Contains(got, `source="none"`) {
		t.Errorf("summary missing the no-source conclusion:\n%s", got)
	}
	if strings.Contains(got, "Found ") {
		t.Errorf("summary should not report activity:\n%s", got)
	}
}

func TestPrintSummaryWithActivity(t *testing.T) {
	summary := newCaptureSummary("/tmp/discord-ipc-0")
	summary.record(classify(opFrame, []byte(`{"cmd":"DISPATCH","evt":"ACTIVITY_JOIN"}`), time.Now()))

	var out bytes.Buffer
	printSummary(&out, summary)

	got := out.String()
	if !strings.Contains(got, "Found 1 ACTIVITY frame(s)") {
		t.Errorf("summary should report the found activity:\n%s", got)
	}
}

func TestFormatCounts(t *testing.T) {
	if got := formatCounts(nil); got != "-" {
		t.Errorf("formatCounts(nil) = %q, want %q", got, "-")
	}

	counts := map[string]int{"frame": 2, "ping": 1}
	if got := formatCounts(counts); got != "frame=2 ping=1" {
		t.Errorf("formatCounts = %q, want %q", got, "frame=2 ping=1")
	}
}
