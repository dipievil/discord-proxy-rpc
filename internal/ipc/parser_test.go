package ipc

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dragsbruh/gopresence"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func TestParseActivityAllFields(t *testing.T) {
	src := &presence.Activity{
		Details: "Playing a game",
		State:   "In a match",
		Type:    presence.ActivityPlaying,
		Timestamps: &presence.Timestamps{
			Start: 1234567890000,
			End:   1234567990000,
		},
		Assets: &presence.Assets{
			LargeImage: "large.png",
			LargeText:  "Large Asset",
			SmallImage: "small.png",
			SmallText:  "Small Asset",
		},
		Party: &presence.Party{
			ID:   "party-123",
			Size: [2]int{2, 4},
		},
		Buttons: []presence.Button{
			{Label: "Join", URL: "https://example.com/join"},
			{Label: "Info", URL: "https://example.com/info"},
		},
	}

	got, err := ParseActivity(src)
	if err != nil {
		t.Fatalf("ParseActivity: %v", err)
	}

	want := types.Activity{
		Details: "Playing a game",
		State:   "In a match",
		Type:    types.ActivityPlaying,
		Timestamps: &types.Timestamps{
			Start: 1234567890000,
			End:   1234567990000,
		},
		Assets: &types.Assets{
			LargeImage: "large.png",
			LargeText:  "Large Asset",
			SmallImage: "small.png",
			SmallText:  "Small Asset",
		},
		Party: &types.Party{
			ID:   "party-123",
			Size: [2]int{2, 4},
		},
		Buttons: []types.Button{
			{Label: "Join", URL: "https://example.com/join"},
			{Label: "Info", URL: "https://example.com/info"},
		},
	}

	assertActivityEqual(t, want, got)
}

func TestParseActivityOmitsEmptyNestedFields(t *testing.T) {
	src := &presence.Activity{
		Type: presence.ActivityPlaying,
	}

	got, err := ParseActivity(src)
	if err != nil {
		t.Fatalf("ParseActivity: %v", err)
	}

	if got.Timestamps != nil {
		t.Error("Timestamps should be nil")
	}
	if got.Assets != nil {
		t.Error("Assets should be nil")
	}
	if got.Party != nil {
		t.Error("Party should be nil")
	}
	if got.Buttons != nil {
		t.Error("Buttons should be nil")
	}
}

func TestParseActivityNil(t *testing.T) {
	_, err := ParseActivity(nil)
	if !errors.Is(err, ErrNilActivity) {
		t.Errorf("error = %v, want ErrNilActivity", err)
	}
}

func TestParseActivityInvalidType(t *testing.T) {
	_, err := ParseActivity(&presence.Activity{Type: presence.ActivityType(99)})
	if !errors.Is(err, ErrInvalidActivity) {
		t.Errorf("error = %v, want ErrInvalidActivity", err)
	}
}

func TestParseActivityInvalidButton(t *testing.T) {
	_, err := ParseActivity(&presence.Activity{
		Type: presence.ActivityPlaying,
		Buttons: []presence.Button{
			{Label: "", URL: "https://example.com"},
		},
	})
	if !errors.Is(err, ErrInvalidActivity) {
		t.Errorf("error = %v, want ErrInvalidActivity", err)
	}
}

func TestParseActivityTooManyButtons(t *testing.T) {
	_, err := ParseActivity(&presence.Activity{
		Type: presence.ActivityPlaying,
		Buttons: []presence.Button{
			{Label: "One", URL: "https://one"},
			{Label: "Two", URL: "https://two"},
			{Label: "Three", URL: "https://three"},
		},
	})
	if !errors.Is(err, ErrInvalidActivity) {
		t.Errorf("error = %v, want ErrInvalidActivity", err)
	}
}

func TestParseActivityStripsUnmappedAssetFields(t *testing.T) {
	src := &presence.Activity{
		Type: presence.ActivityListening,
		Assets: &presence.Assets{
			LargeImage:       "img",
			LargeURL:         "https://large",
			InviteCoverImage: "cover",
		},
	}

	got, err := ParseActivity(src)
	if err != nil {
		t.Fatalf("ParseActivity: %v", err)
	}
	if got.Assets == nil || got.Assets.LargeImage != "img" {
		t.Fatalf("Assets = %#v, want large_image only", got.Assets)
	}
}

func TestParseActivityJSONRoundTrip(t *testing.T) {
	raw := []byte(`{
		"details": "Playing a game",
		"state": "In a match",
		"type": 0,
		"timestamps": {"start": 1234567890000, "end": 1234567990000},
		"assets": {
			"large_image": "large.png",
			"large_text": "Large Asset",
			"small_image": "small.png",
			"small_text": "Small Asset"
		},
		"party": {"id": "party-123", "size": [2, 4]},
		"buttons": [
			{"label": "Join", "url": "https://example.com/join"},
			{"label": "Info", "url": "https://example.com/info"}
		]
	}`)

	parsed, err := ParseActivityData(raw)
	if err != nil {
		t.Fatalf("ParseActivityData: %v", err)
	}

	encoded, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded types.Activity
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assertActivityEqual(t, parsed, decoded)
}

func TestParseActivityFrame(t *testing.T) {
	frame := &presence.ResponseFrame{
		Command: "DISPATCH",
		Event:   "ACTIVITY_UPDATE",
		Data:    json.RawMessage(`{"details":"hello","state":"world","type":3}`),
	}

	got, err := ParseActivityFrame(frame)
	if err != nil {
		t.Fatalf("ParseActivityFrame: %v", err)
	}

	if got.Details != "hello" || got.State != "world" || got.Type != types.ActivityWatching {
		t.Errorf("activity = %#v, want details/state/type from frame", got)
	}
}

func TestParseActivityDataNull(t *testing.T) {
	_, err := ParseActivityData(json.RawMessage("null"))
	if !errors.Is(err, ErrNilActivity) {
		t.Errorf("error = %v, want ErrNilActivity", err)
	}
}

func assertActivityEqual(t *testing.T, want, got types.Activity) {
	t.Helper()

	if got.Details != want.Details {
		t.Errorf("Details = %q, want %q", got.Details, want.Details)
	}
	if got.State != want.State {
		t.Errorf("State = %q, want %q", got.State, want.State)
	}
	if got.Type != want.Type {
		t.Errorf("Type = %v, want %v", got.Type, want.Type)
	}

	if (want.Timestamps == nil) != (got.Timestamps == nil) {
		t.Fatalf("Timestamps nil mismatch: got %#v want %#v", got.Timestamps, want.Timestamps)
	}
	if want.Timestamps != nil && *got.Timestamps != *want.Timestamps {
		t.Errorf("Timestamps = %#v, want %#v", got.Timestamps, want.Timestamps)
	}

	if (want.Assets == nil) != (got.Assets == nil) {
		t.Fatalf("Assets nil mismatch: got %#v want %#v", got.Assets, want.Assets)
	}
	if want.Assets != nil && *got.Assets != *want.Assets {
		t.Errorf("Assets = %#v, want %#v", got.Assets, want.Assets)
	}

	if (want.Party == nil) != (got.Party == nil) {
		t.Fatalf("Party nil mismatch: got %#v want %#v", got.Party, want.Party)
	}
	if want.Party != nil && *got.Party != *want.Party {
		t.Errorf("Party = %#v, want %#v", got.Party, want.Party)
	}

	if len(got.Buttons) != len(want.Buttons) {
		t.Fatalf("Buttons len = %d, want %d", len(got.Buttons), len(want.Buttons))
	}
	for i := range want.Buttons {
		if got.Buttons[i] != want.Buttons[i] {
			t.Errorf("Buttons[%d] = %#v, want %#v", i, got.Buttons[i], want.Buttons[i])
		}
	}
}
