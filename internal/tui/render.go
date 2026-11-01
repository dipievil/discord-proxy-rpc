package tui

import (
	"fmt"
	"strings"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

const dash = "\u2014"

var activityTypeLabels = map[types.ActivityType]string{
	types.ActivityPlaying:   "Playing",
	types.ActivityStreaming: "Streaming",
	types.ActivityListening: "Listening",
	types.ActivityWatching:  "Watching",
	types.ActivityCustom:    "Custom",
	types.ActivityCompeting: "Competing",
}

func activityTypeLabel(t types.ActivityType) string {
	if label, ok := activityTypeLabels[t]; ok {
		return label
	}
	return "Unknown"
}

// formatDuration mirrors the web dashboard: whole seconds rendered as
// "Xh Ym Zs", dropping zero-leading units.
func formatDuration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

type timestampValues struct {
	Elapsed      int64
	HasElapsed   bool
	Remaining    int64
	HasRemaining bool
	Ended        bool
}

// computeTimestamps mirrors the web dashboard: Discord IPC timestamps are
// Unix milliseconds, compared against nowMs in the same unit.
func computeTimestamps(ts *types.Timestamps, nowMs int64) timestampValues {
	var out timestampValues
	if ts == nil {
		return out
	}
	if ts.Start != 0 {
		elapsed := (nowMs - ts.Start) / 1000
		if elapsed >= 0 {
			out.Elapsed = elapsed
			out.HasElapsed = true
		}
	}
	if ts.End != 0 {
		remaining := (ts.End - nowMs) / 1000
		if remaining <= 0 {
			out.Ended = true
		} else {
			out.Remaining = remaining
			out.HasRemaining = true
		}
	}
	return out
}

func formatTimestamps(ts *types.Timestamps, nowMs int64) (elapsed, remaining string) {
	values := computeTimestamps(ts, nowMs)

	elapsed = dash
	if values.HasElapsed {
		elapsed = formatDuration(values.Elapsed)
	}

	remaining = dash
	switch {
	case values.Ended:
		remaining = "Ended"
	case values.HasRemaining:
		remaining = formatDuration(values.Remaining)
	}

	return elapsed, remaining
}

func formatParty(party *types.Party) string {
	if party == nil {
		return dash
	}
	id := party.ID
	if id == "" {
		id = dash
	}
	return fmt.Sprintf("%s (%d/%d)", id, party.Size[0], party.Size[1])
}

func formatAssets(assets *types.Assets) (large, small string) {
	if assets == nil {
		return dash, dash
	}
	large = dash
	if assets.LargeText != "" || assets.LargeImage != "" {
		large = assets.LargeText
		if large == "" {
			large = assets.LargeImage
		}
	}
	small = dash
	if assets.SmallText != "" || assets.SmallImage != "" {
		small = assets.SmallText
		if small == "" {
			small = assets.SmallImage
		}
	}
	return large, small
}

func formatButtons(buttons []types.Button) string {
	if len(buttons) == 0 {
		return dash
	}
	labels := make([]string, 0, len(buttons))
	for _, b := range buttons {
		labels = append(labels, fmt.Sprintf("%s (%s)", b.Label, b.URL))
	}
	return strings.Join(labels, ", ")
}
