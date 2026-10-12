package ipc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dragsbruh/gopresence"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

const maxActivityButtons = 2

var (
	ErrNilActivity    = errors.New("ipc: activity is nil")
	ErrInvalidActivity = errors.New("ipc: invalid activity")
)

// ParseActivity normalizes a gopresence Activity into the shared pkg/types.Activity
// representation. Timestamps are preserved as Unix milliseconds. Optional nested
// fields are omitted when empty.
func ParseActivity(src *presence.Activity) (types.Activity, error) {
	if src == nil {
		return types.Activity{}, ErrNilActivity
	}

	if err := validateActivityType(src.Type); err != nil {
		return types.Activity{}, err
	}

	dst := types.Activity{
		Details: src.Details,
		State:   src.State,
		Type:    types.ActivityType(src.Type),
	}

	if ts := parseTimestamps(src.Timestamps); ts != nil {
		dst.Timestamps = ts
	}
	if assets := parseAssets(src.Assets); assets != nil {
		dst.Assets = assets
	}
	if party := parseParty(src.Party); party != nil {
		dst.Party = party
	}
	buttons, err := parseButtons(src.Buttons)
	if err != nil {
		return types.Activity{}, err
	}
	if len(buttons) > 0 {
		dst.Buttons = buttons
	}

	return dst, nil
}

// ParseActivityData unmarshals raw Discord activity JSON and normalizes it.
func ParseActivityData(data json.RawMessage) (types.Activity, error) {
	if len(data) == 0 || string(data) == "null" {
		return types.Activity{}, ErrNilActivity
	}

	var src presence.Activity
	if err := json.Unmarshal(data, &src); err != nil {
		return types.Activity{}, fmt.Errorf("ipc: unmarshal activity: %w", err)
	}
	return ParseActivity(&src)
}

// ParseActivityFrame extracts and normalizes an activity from a DISPATCH frame.
func ParseActivityFrame(frame *presence.ResponseFrame) (types.Activity, error) {
	if frame == nil {
		return types.Activity{}, errors.New("ipc: frame is nil")
	}
	return ParseActivityData(frame.Data)
}

func validateActivityType(t presence.ActivityType) error {
	switch types.ActivityType(t) {
	case types.ActivityPlaying, types.ActivityStreaming, types.ActivityListening,
		types.ActivityWatching, types.ActivityCustom, types.ActivityCompeting:
		return nil
	default:
		return fmt.Errorf("%w: unknown type %d", ErrInvalidActivity, t)
	}
}

func parseTimestamps(src *presence.Timestamps) *types.Timestamps {
	if src == nil || (src.Start == 0 && src.End == 0) {
		return nil
	}
	return &types.Timestamps{
		Start: int64(src.Start),
		End:   int64(src.End),
	}
}

func parseAssets(src *presence.Assets) *types.Assets {
	if src == nil {
		return nil
	}

	dst := types.Assets{
		LargeImage: src.LargeImage,
		LargeText:  src.LargeText,
		SmallImage: src.SmallImage,
		SmallText:  src.SmallText,
	}
	if dst.LargeImage == "" && dst.LargeText == "" && dst.SmallImage == "" && dst.SmallText == "" {
		return nil
	}
	return &dst
}

func parseParty(src *presence.Party) *types.Party {
	if src == nil || (src.ID == "" && src.Size[0] == 0 && src.Size[1] == 0) {
		return nil
	}
	return &types.Party{
		ID:   src.ID,
		Size: src.Size,
	}
}

func parseButtons(src []presence.Button) ([]types.Button, error) {
	if len(src) == 0 {
		return nil, nil
	}
	if len(src) > maxActivityButtons {
		return nil, fmt.Errorf("%w: at most %d buttons allowed, got %d", ErrInvalidActivity, maxActivityButtons, len(src))
	}

	dst := make([]types.Button, 0, len(src))
	for i, btn := range src {
		if btn.Label == "" {
			return nil, fmt.Errorf("%w: button %d: label is required", ErrInvalidActivity, i)
		}
		if btn.URL == "" {
			return nil, fmt.Errorf("%w: button %d: url is required", ErrInvalidActivity, i)
		}
		dst = append(dst, types.Button{Label: btn.Label, URL: btn.URL})
	}
	return dst, nil
}
