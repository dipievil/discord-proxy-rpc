package types

import (
	"encoding/json"
	"testing"
)

func TestActivityJSONRoundTrip(t *testing.T) {
	original := Activity{
		Details: "Playing a game",
		State:   "In a match",
		Timestamps: &Timestamps{
			Start: 1234567890000,
			End:   1234567990000,
		},
		Assets: &Assets{
			LargeImage: "large.png",
			LargeText:  "Large Asset",
			SmallImage: "small.png",
			SmallText:  "Small Asset",
		},
		Party: &Party{
			ID:   "party-123",
			Size: [2]int{2, 4},
		},
		Buttons: []Button{
			{Label: "Join", URL: "https://example.com/join"},
			{Label: "Info", URL: "https://example.com/info"},
		},
		Type: ActivityPlaying,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Activity
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	assertEqual(t, original.Details, decoded.Details, "Details")
	assertEqual(t, original.State, decoded.State, "State")
	assertEqual(t, original.Type, decoded.Type, "Type")

	if decoded.Timestamps == nil {
		t.Fatal("Timestamps is nil after round-trip")
	}
	assertEqual(t, original.Timestamps.Start, decoded.Timestamps.Start, "Timestamps.Start")
	assertEqual(t, original.Timestamps.End, decoded.Timestamps.End, "Timestamps.End")

	if decoded.Assets == nil {
		t.Fatal("Assets is nil after round-trip")
	}
	assertEqual(t, original.Assets.LargeImage, decoded.Assets.LargeImage, "Assets.LargeImage")
	assertEqual(t, original.Assets.LargeText, decoded.Assets.LargeText, "Assets.LargeText")
	assertEqual(t, original.Assets.SmallImage, decoded.Assets.SmallImage, "Assets.SmallImage")
	assertEqual(t, original.Assets.SmallText, decoded.Assets.SmallText, "Assets.SmallText")

	if decoded.Party == nil {
		t.Fatal("Party is nil after round-trip")
	}
	assertEqual(t, original.Party.ID, decoded.Party.ID, "Party.ID")
	assertEqual(t, original.Party.Size[0], decoded.Party.Size[0], "Party.Size[0]")
	assertEqual(t, original.Party.Size[1], decoded.Party.Size[1], "Party.Size[1]")

	if len(decoded.Buttons) != len(original.Buttons) {
		t.Fatalf("Buttons length mismatch: got %d, want %d", len(decoded.Buttons), len(original.Buttons))
	}
	for i, btn := range decoded.Buttons {
		assertEqual(t, original.Buttons[i].Label, btn.Label, "Buttons["+string(rune('0'+i))+"].Label")
		assertEqual(t, original.Buttons[i].URL, btn.URL, "Buttons["+string(rune('0'+i))+"].URL")
	}
}

func TestActivityAllFieldsPreserved(t *testing.T) {
	original := Activity{
		Details: "details",
		State:   "state",
		Timestamps: &Timestamps{
			Start: 9999,
			End:   8888,
		},
		Assets: &Assets{
			LargeImage: "li",
			LargeText:  "lt",
			SmallImage: "si",
			SmallText:  "st",
		},
		Party: &Party{
			ID:   "p1",
			Size: [2]int{1, 5},
		},
		Buttons: []Button{
			{Label: "btn", URL: "http://btn"},
		},
		Type: ActivityStreaming,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Activity
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	assertEqual(t, decoded.Details, original.Details, "Details")
	assertEqual(t, decoded.State, original.State, "State")
	assertEqual(t, decoded.Type, original.Type, "Type")
	assertEqual(t, *decoded.Timestamps, *original.Timestamps, "Timestamps")
	assertEqual(t, *decoded.Assets, *original.Assets, "Assets")
	assertEqual(t, *decoded.Party, *original.Party, "Party")
	assertEqual(t, len(decoded.Buttons), len(original.Buttons), "Buttons len")
	assertEqual(t, decoded.Buttons[0], original.Buttons[0], "Buttons[0]")
}

func TestActivityIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		activity Activity
		want     bool
	}{
		{
			name:     "zero value",
			activity: Activity{},
			want:     true,
		},
		{
			name:     "with details",
			activity: Activity{Details: "something"},
			want:     false,
		},
		{
			name:     "with state",
			activity: Activity{State: "something"},
			want:     false,
		},
		{
			name:     "with timestamps",
			activity: Activity{Timestamps: &Timestamps{Start: 1}},
			want:     false,
		},
		{
			name:     "with assets",
			activity: Activity{Assets: &Assets{LargeImage: "img"}},
			want:     false,
		},
		{
			name:     "with party",
			activity: Activity{Party: &Party{ID: "p1"}},
			want:     false,
		},
		{
			name:     "with buttons",
			activity: Activity{Buttons: []Button{{Label: "btn", URL: "http://url"}}},
			want:     false,
		},
		{
			name:     "with non-zero type",
			activity: Activity{Type: ActivityListening},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.activity.IsEmpty()
			if got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestActivityTypeConstants(t *testing.T) {
	if ActivityPlaying != 0 {
		t.Errorf("ActivityPlaying = %d, want 0", ActivityPlaying)
	}
	if ActivityStreaming != 1 {
		t.Errorf("ActivityStreaming = %d, want 1", ActivityStreaming)
	}
	if ActivityListening != 2 {
		t.Errorf("ActivityListening = %d, want 2", ActivityListening)
	}
	if ActivityWatching != 3 {
		t.Errorf("ActivityWatching = %d, want 3", ActivityWatching)
	}
	if ActivityCustom != 4 {
		t.Errorf("ActivityCustom = %d, want 4", ActivityCustom)
	}
	if ActivityCompeting != 5 {
		t.Errorf("ActivityCompeting = %d, want 5", ActivityCompeting)
	}
}

func TestActivityOmitEmptyButtons(t *testing.T) {
	a := Activity{
		Details: "test",
		Type:    ActivityPlaying,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}

	if _, exists := raw["buttons"]; exists {
		t.Error("buttons should be omitted from JSON when empty")
	}
}

func TestActivityCloneDeepCopy(t *testing.T) {
	original := Activity{
		Details: "details",
		State:   "state",
		Timestamps: &Timestamps{
			Start: 1234,
			End:   5678,
		},
		Assets: &Assets{
			LargeImage: "li",
			LargeText:  "lt",
		},
		Party: &Party{
			ID:   "p1",
			Size: [2]int{2, 4},
		},
		Buttons: []Button{
			{Label: "btn", URL: "http://btn"},
		},
		Type: ActivityStreaming,
	}

	clone := original.Clone()

	assertEqual(t, clone.Details, original.Details, "Clone.Details")
	assertEqual(t, clone.State, original.State, "Clone.State")
	assertEqual(t, clone.Type, original.Type, "Clone.Type")
	assertEqual(t, *clone.Timestamps, *original.Timestamps, "Clone.Timestamps")
	assertEqual(t, *clone.Assets, *original.Assets, "Clone.Assets")
	assertEqual(t, *clone.Party, *original.Party, "Clone.Party")
	assertEqual(t, len(clone.Buttons), len(original.Buttons), "Clone.Buttons len")

	clone.Timestamps.Start = 9999
	clone.Assets.LargeImage = "modified"
	clone.Party.ID = "modified"
	clone.Buttons[0].Label = "modified"

	if original.Timestamps.Start == 9999 {
		t.Error("Clone shares Timestamps pointer")
	}
	if original.Assets.LargeImage == "modified" {
		t.Error("Clone shares Assets pointer")
	}
	if original.Party.ID == "modified" {
		t.Error("Clone shares Party pointer")
	}
	if original.Buttons[0].Label == "modified" {
		t.Error("Clone shares Buttons slice backing array")
	}
}

func TestActivityCloneNilPointers(t *testing.T) {
	original := Activity{Details: "simple", Type: ActivityPlaying}
	clone := original.Clone()

	assertEqual(t, clone.Details, original.Details, "Clone.Details")
	assertEqual(t, clone.Type, original.Type, "Clone.Type")

	if clone.Timestamps != nil {
		t.Error("Clone.Timestamps should be nil")
	}
	if clone.Assets != nil {
		t.Error("Clone.Assets should be nil")
	}
	if clone.Party != nil {
		t.Error("Clone.Party should be nil")
	}
	if clone.Buttons != nil {
		t.Error("Clone.Buttons should be nil")
	}
}

func assertEqual(t *testing.T, got, want interface{}, field string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}
