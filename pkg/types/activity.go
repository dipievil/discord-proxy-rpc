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

func (a *Activity) MarshalJSON() ([]byte, error) {
	type alias Activity
	if a.Buttons != nil && len(a.Buttons) == 0 {
		a.Buttons = nil
	}
	return json.Marshal((*alias)(a))
}
