package tray

import (
	"math"
	"testing"
)

func TestPixmapsSizes(t *testing.T) {
	for _, icon := range []Icon{IconConnected, IconReconnecting, IconDisconnected} {
		pixmaps := Pixmaps(icon)
		if len(pixmaps) != len(iconSizes) {
			t.Fatalf("%v: got %d pixmaps, want %d", icon, len(pixmaps), len(iconSizes))
		}
		for i, pixmap := range pixmaps {
			want := iconSizes[i]
			if pixmap.Width != want || pixmap.Height != want {
				t.Errorf("%v: pixmap %d is %dx%d, want %dx%d", icon, i, pixmap.Width, pixmap.Height, want, want)
			}
			if got, wantLen := len(pixmap.Data), int(want*want*4); got != wantLen {
				t.Errorf("%v: pixmap %d has %d bytes, want %d", icon, i, got, wantLen)
			}
		}
	}
}

func TestPixmapsDiscIsAntiAliased(t *testing.T) {
	for _, icon := range []Icon{IconConnected, IconReconnecting, IconDisconnected} {
		pixmap := Pixmaps(icon)[1]
		size := int(pixmap.Width)

		if alpha := pixmap.Data[(0*size+0)*4]; alpha != 0 {
			t.Errorf("%v: corner alpha = %d, want 0", icon, alpha)
		}
		if alpha := pixmap.Data[(size/2*size+size/2)*4]; alpha != 0xff {
			t.Errorf("%v: centre alpha = %d, want 255", icon, alpha)
		}

		white := 0
		for offset := 0; offset < len(pixmap.Data); offset += 4 {
			if pixmap.Data[offset+1] > 240 && pixmap.Data[offset+2] > 240 && pixmap.Data[offset+3] > 240 {
				white++
			}
		}
		if white == 0 {
			t.Errorf("%v: icon has no white status glyph", icon)
		}
	}
}

func TestPixmapsUseDistinctColours(t *testing.T) {
	size := int(iconSizes[1])
	offsets := map[Icon]int{}
	for _, icon := range []Icon{IconConnected, IconReconnecting, IconDisconnected} {
		offsets[icon] = (size/4*size + size/2) * 4
	}

	tests := []struct {
		icon  Icon
		check func(t *testing.T, red, green, blue byte)
	}{
		{IconConnected, func(t *testing.T, red, green, blue byte) {
			if green <= red || green <= blue {
				t.Errorf("connected icon is not green: r=%d g=%d b=%d", red, green, blue)
			}
		}},
		{IconReconnecting, func(t *testing.T, red, green, blue byte) {
			if red <= blue || green <= blue {
				t.Errorf("reconnecting icon is not amber: r=%d g=%d b=%d", red, green, blue)
			}
		}},
		{IconDisconnected, func(t *testing.T, red, green, blue byte) {
			if red <= green || red <= blue {
				t.Errorf("disconnected icon is not red: r=%d g=%d b=%d", red, green, blue)
			}
		}},
	}

	for _, test := range tests {
		pixmap := Pixmaps(test.icon)[1]
		offset := offsets[test.icon]
		if alpha := pixmap.Data[offset]; alpha != 0xff {
			t.Fatalf("%v: sample alpha = %d, want 255", test.icon, alpha)
		}
		test.check(t, pixmap.Data[offset+1], pixmap.Data[offset+2], pixmap.Data[offset+3])
	}
}

func TestPixmapsDifferPerIcon(t *testing.T) {
	connected := Pixmaps(IconConnected)[0].Data
	reconnecting := Pixmaps(IconReconnecting)[0].Data
	disconnected := Pixmaps(IconDisconnected)[0].Data

	if string(connected) == string(reconnecting) {
		t.Error("connected and reconnecting icons are identical")
	}
	if string(connected) == string(disconnected) {
		t.Error("connected and disconnected icons are identical")
	}
	if string(reconnecting) == string(disconnected) {
		t.Error("reconnecting and disconnected icons are identical")
	}
}

func TestDistanceToSegment(t *testing.T) {
	a := point{x: 0, y: 0}
	b := point{x: 1, y: 0}

	if got := distanceToSegment(0.5, 0.25, a, b); got != 0.25 {
		t.Errorf("distance above segment = %v, want 0.25", got)
	}
	if got := distanceToSegment(2, 0, a, b); got != 1 {
		t.Errorf("distance past the end = %v, want 1", got)
	}
	if got := distanceToSegment(0.5, 0.5, a, a); math.Abs(got-math.Sqrt2/2) > 1e-9 {
		t.Errorf("distance to a degenerate segment = %v, want %v", got, math.Sqrt2/2)
	}
}
