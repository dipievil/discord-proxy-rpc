package tray

import (
	"image/color"
	"math"
)

// Pixmap is one image in the StatusNotifierItem "IconPixmap" property: an
// ARGB32 image in network byte order, exactly as the SNI specification
// requires. godbus marshals the struct as the (iiay) D-Bus entry.
type Pixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

// iconSizes are the embedded icon sizes: 1x and 2x for HiDPI panels.
var iconSizes = []int32{22, 44}

// Pixmaps returns the embedded status icons at every supported size. The
// images are generated at startup instead of loaded from disk, so the binary
// stays self-contained and no icon theme is needed.
func Pixmaps(icon Icon) []Pixmap {
	pixmaps := make([]Pixmap, 0, len(iconSizes))
	for _, size := range iconSizes {
		pixmaps = append(pixmaps, Pixmap{
			Width:  size,
			Height: size,
			Data:   drawIcon(icon, int(size)),
		})
	}
	return pixmaps
}

// iconColor is the disc colour for a status. The glyph on top of it differs
// per state as well, so the icons stay distinguishable in greyscale.
func iconColor(icon Icon) color.RGBA {
	switch icon {
	case IconConnected:
		return color.RGBA{R: 0x3f, G: 0xb9, B: 0x50, A: 0xff}
	case IconReconnecting:
		return color.RGBA{R: 0xd2, G: 0x99, B: 0x22, A: 0xff}
	default:
		return color.RGBA{R: 0xf8, G: 0x51, B: 0x49, A: 0xff}
	}
}

type point struct{ x, y float64 }

// glyphFor returns the white overlay strokes, in normalised icon coordinates:
// a check mark when connected, a minus (retrying) while reconnecting and a
// slash when disconnected.
func glyphFor(icon Icon) [][2]point {
	switch icon {
	case IconConnected:
		return [][2]point{
			{{x: 0.28, y: 0.52}, {x: 0.44, y: 0.68}},
			{{x: 0.44, y: 0.68}, {x: 0.72, y: 0.34}},
		}
	case IconReconnecting:
		return [][2]point{
			{{x: 0.31, y: 0.50}, {x: 0.69, y: 0.50}},
		}
	default:
		return [][2]point{
			{{x: 0.30, y: 0.30}, {x: 0.70, y: 0.70}},
		}
	}
}

const (
	discRadius   = 0.46
	glyphHalfW   = 0.055
	subsamples   = 3
	totalSamples = subsamples * subsamples
	glyphRGBA    = 0xff
)

// drawIcon renders one ARGB32 icon: an anti-aliased coloured disc with the
// status glyph in white.
func drawIcon(icon Icon, size int) []byte {
	fg := iconColor(icon)
	glyph := glyphFor(icon)
	data := make([]byte, size*size*4)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var (
				covered float64
				sumR    float64
				sumG    float64
				sumB    float64
			)
			for sy := 0; sy < subsamples; sy++ {
				for sx := 0; sx < subsamples; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/subsamples) / float64(size)
					py := (float64(y) + (float64(sy)+0.5)/subsamples) / float64(size)
					if !insideCircle(px, py) {
						continue
					}
					covered++
					if insideGlyph(glyph, px, py) {
						sumR += glyphRGBA
						sumG += glyphRGBA
						sumB += glyphRGBA
						continue
					}
					sumR += float64(fg.R)
					sumG += float64(fg.G)
					sumB += float64(fg.B)
				}
			}
			if covered == 0 {
				continue
			}
			offset := (y*size + x) * 4
			data[offset] = uint8(math.Round(255 * covered / totalSamples))
			data[offset+1] = uint8(math.Round(sumR / covered))
			data[offset+2] = uint8(math.Round(sumG / covered))
			data[offset+3] = uint8(math.Round(sumB / covered))
		}
	}
	return data
}

func insideCircle(x, y float64) bool {
	dx, dy := x-0.5, y-0.5
	return dx*dx+dy*dy <= discRadius*discRadius
}

func insideGlyph(glyph [][2]point, x, y float64) bool {
	for _, segment := range glyph {
		if distanceToSegment(x, y, segment[0], segment[1]) <= glyphHalfW {
			return true
		}
	}
	return false
}

// distanceToSegment is the shortest distance from (x, y) to the segment a-b.
func distanceToSegment(x, y float64, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	lengthSquared := dx*dx + dy*dy
	if lengthSquared == 0 {
		return math.Hypot(x-a.x, y-a.y)
	}
	t := ((x-a.x)*dx + (y-a.y)*dy) / lengthSquared
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(x-(a.x+t*dx), y-(a.y+t*dy))
}
