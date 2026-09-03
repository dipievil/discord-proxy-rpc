package types

import "encoding/json"

type ActivityType int

const (
	ActivityPlaying   ActivityType = 0
	ActivityStreaming ActivityType = 1
	ActivityListening ActivityType = 2
	ActivityWatching  ActivityType = 3
	ActivityCustom    ActivityType = 4
	ActivityCompeting ActivityType = 5
)

type Activity struct {
	Details    string       `json:"details,omitempty"`
	State      string       `json:"state,omitempty"`
	Timestamps *Timestamps  `json:"timestamps,omitempty"`
	Assets     *Assets      `json:"assets,omitempty"`
	Party      *Party       `json:"party,omitempty"`
	Buttons    []Button     `json:"buttons,omitempty"`
	Type       ActivityType `json:"type"`
}

type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type Party struct {
	ID   string  `json:"id,omitempty"`
	Size [2]int `json:"size,omitempty"`
}

type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

func (a *Activity) IsEmpty() bool {
	return a.Details == "" &&
		a.State == "" &&
		a.Timestamps == nil &&
		a.Assets == nil &&
		a.Party == nil &&
		len(a.Buttons) == 0 &&
		a.Type == ActivityPlaying
}

func (a Activity) Clone() Activity {
	out := Activity{
		Details: a.Details,
		State:   a.State,
		Type:    a.Type,
	}
	if a.Timestamps != nil {
		ts := *a.Timestamps
		out.Timestamps = &ts
	}
	if a.Assets != nil {
		assets := *a.Assets
		out.Assets = &assets
	}
	if a.Party != nil {
		party := *a.Party
		out.Party = &party
	}
	if a.Buttons != nil {
		out.Buttons = make([]Button, len(a.Buttons))
		copy(out.Buttons, a.Buttons)
	}
	return out
}

func (a Activity) Equals(other Activity) bool {
	if a.Details != other.Details || a.State != other.State || a.Type != other.Type {
		return false
	}
	if !timestampsEqual(a.Timestamps, other.Timestamps) {
		return false
	}
	if !assetsEqual(a.Assets, other.Assets) {
		return false
	}
	if !partyEqual(a.Party, other.Party) {
		return false
	}
	if len(a.Buttons) != len(other.Buttons) {
		return false
	}
	for i := range a.Buttons {
		if a.Buttons[i].Label != other.Buttons[i].Label || a.Buttons[i].URL != other.Buttons[i].URL {
			return false
		}
	}
	return true
}

func timestampsEqual(a, b *Timestamps) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil {
		return b.Start == 0 && b.End == 0
	}
	if b == nil {
		return a.Start == 0 && a.End == 0
	}
	return a.Start == b.Start && a.End == b.End
}

func assetsEqual(a, b *Assets) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil {
		return b.LargeImage == "" && b.LargeText == "" && b.SmallImage == "" && b.SmallText == ""
	}
	if b == nil {
		return a.LargeImage == "" && a.LargeText == "" && a.SmallImage == "" && a.SmallText == ""
	}
	return *a == *b
}

func partyEqual(a, b *Party) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil {
		return b.ID == "" && b.Size == [2]int{}
	}
	if b == nil {
		return a.ID == "" && a.Size == [2]int{}
	}
	return a.ID == b.ID && a.Size == b.Size
}

func (a *Activity) MarshalJSON() ([]byte, error) {
	type alias Activity
	if a.Buttons != nil && len(a.Buttons) == 0 {
		a.Buttons = nil
	}
	return json.Marshal((*alias)(a))
}
