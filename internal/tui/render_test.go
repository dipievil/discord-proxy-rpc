package tui

import (
	"strings"
	"testing"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func TestActivityTypeLabel(t *testing.T) {
	cases := map[types.ActivityType]string{
		types.ActivityPlaying:   "Playing",
		types.ActivityStreaming: "Streaming",
		types.ActivityListening: "Listening",
		types.ActivityWatching:  "Watching",
		types.ActivityCustom:    "Custom",
		types.ActivityCompeting: "Competing",
		types.ActivityType(99):  "Unknown",
	}
	for activityType, want := range cases {
		if got := activityTypeLabel(activityType); got != want {
			t.Errorf("activityTypeLabel(%d) = %q, want %q", activityType, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{
		0:    "0s",
		45:   "45s",
		59:   "59s",
		90:   "1m 30s",
		3600: "1h 0m 0s",
		3661: "1h 1m 1s",
		-5:   "0s",
	}

	for seconds, want := range cases {
		if got := formatDuration(seconds); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", seconds, got, want)
		}
	}
}

func TestComputeTimestamps(t *testing.T) {
	now := int64(1_700_000_000_000) // ms

	t.Run("nil timestamps", func(t *testing.T) {
		values := computeTimestamps(nil, now)
		if values.HasElapsed || values.HasRemaining || values.Ended {
			t.Fatalf("expected zero values, got %+v", values)
		}
	})

	t.Run("start only", func(t *testing.T) {
		values := computeTimestamps(&types.Timestamps{Start: now - 90_000}, now)
		if !values.HasElapsed || values.Elapsed != 90 {
			t.Fatalf("elapsed = (%v, %v), want (90, true)", values.Elapsed, values.HasElapsed)
		}
		if values.HasRemaining || values.Ended {
			t.Fatalf("unexpected remaining state: %+v", values)
		}
	})

	t.Run("end in the future", func(t *testing.T) {
		values := computeTimestamps(&types.Timestamps{End: now + 30_000}, now)
		if !values.HasRemaining || values.Remaining != 30 {
			t.Fatalf("remaining = (%v, %v), want (30, true)", values.Remaining, values.HasRemaining)
		}
		if values.Ended {
			t.Fatal("end in the future must not be Ended")
		}
	})

	t.Run("end in the past", func(t *testing.T) {
		values := computeTimestamps(&types.Timestamps{End: now - 1_000}, now)
		if !values.Ended {
			t.Fatal("end in the past must be Ended")
		}
		if values.HasRemaining {
			t.Fatal("ended timestamps must not report remaining")
		}
	})

	t.Run("start in the future is ignored", func(t *testing.T) {
		values := computeTimestamps(&types.Timestamps{Start: now + 60_000}, now)
		if values.HasElapsed {
			t.Fatal("future start must not produce elapsed")
		}
	})
}

func TestFormatTimestamps(t *testing.T) {
	now := int64(1_700_000_000_000)

	elapsed, remaining := formatTimestamps(nil, now)
	if elapsed != dash || remaining != dash {
		t.Fatalf("nil timestamps = (%q, %q), want (%q, %q)", elapsed, remaining, dash, dash)
	}

	ts := &types.Timestamps{Start: now - 3_661_000, End: now - 1}
	elapsed, remaining = formatTimestamps(ts, now)
	if elapsed != "1h 1m 1s" {
		t.Fatalf("elapsed = %q, want %q", elapsed, "1h 1m 1s")
	}
	if remaining != "Ended" {
		t.Fatalf("remaining = %q, want %q", remaining, "Ended")
	}
}

func TestFormatParty(t *testing.T) {
	if got := formatParty(nil); got != dash {
		t.Fatalf("nil party = %q, want %q", got, dash)
	}
	if got := formatParty(&types.Party{ID: "raid", Size: [2]int{3, 10}}); got != "raid (3/10)" {
		t.Fatalf("party = %q, want %q", got, "raid (3/10)")
	}
	if got := formatParty(&types.Party{}); got != dash+" (0/0)" {
		t.Fatalf("empty party = %q, want %q", got, dash+" (0/0)")
	}
}

func TestFormatAssets(t *testing.T) {
	large, small := formatAssets(nil)
	if large != dash || small != dash {
		t.Fatalf("nil assets = (%q, %q), want dashes", large, small)
	}

	large, small = formatAssets(&types.Assets{LargeText: "Big", SmallImage: "sm.png"})
	if large != "Big" || small != "sm.png" {
		t.Fatalf("assets = (%q, %q), want (Big, sm.png)", large, small)
	}

	large, _ = formatAssets(&types.Assets{LargeImage: "lg.png"})
	if large != "lg.png" {
		t.Fatalf("large = %q, want image id fallback %q", large, "lg.png")
	}
}

func TestFormatButtons(t *testing.T) {
	if got := formatButtons(nil); got != dash {
		t.Fatalf("no buttons = %q, want %q", got, dash)
	}
	got := formatButtons([]types.Button{
		{Label: "Repo", URL: "https://example.com"},
		{Label: "Docs", URL: "https://docs.example.com"},
	})
	if !strings.Contains(got, "Repo (https://example.com)") || !strings.Contains(got, "Docs (https://docs.example.com)") {
		t.Fatalf("buttons = %q, want both labels with URLs", got)
	}
}
