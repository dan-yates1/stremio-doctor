//go:build ignore

// gen_icons draws the tray icons: a coloured disc with a white pulse line.
// Run with `go generate ./internal/tray` and commit the results.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const size = 64

var icons = map[string]color.NRGBA{
	"scanning": {0x8a, 0x8a, 0x96, 0xff},
	"ok":       {0x2f, 0x9e, 0x5b, 0xff},
	"warn":     {0xe0, 0x9a, 0x10, 0xff},
	"fail":     {0xd1, 0x34, 0x3a, 0xff},
}

// pulse is the heartbeat polyline, in units of the icon size.
var pulse = [][2]float64{{.18, .55}, {.38, .55}, {.46, .32}, {.56, .74}, {.64, .48}, {.82, .48}}

func main() {
	if err := os.MkdirAll("icons", 0o755); err != nil {
		log.Fatal(err)
	}
	for name, c := range icons {
		var buf bytes.Buffer
		if err := png.Encode(&buf, draw(c)); err != nil {
			log.Fatal(err)
		}
		write(filepath.Join("icons", name+".png"), buf.Bytes())
		write(filepath.Join("icons", name+".ico"), ico(buf.Bytes()))
	}
}

func draw(fill color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4 // supersampling per axis, for smooth edges
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var disc, line int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := (float64(x) + (float64(sx)+.5)/ss) / size
					py := (float64(y) + (float64(sy)+.5)/ss) / size
					if math.Hypot(px-.5, py-.5) <= .47 {
						disc++
						if nearPulse(px, py) {
							line++
						}
					}
				}
			}
			if disc == 0 {
				continue
			}
			a := float64(disc) / (ss * ss)
			w := float64(line) / float64(disc)
			mix := func(c uint8) uint8 { return uint8(float64(c)*(1-w) + 255*w) }
			img.SetNRGBA(x, y, color.NRGBA{mix(fill.R), mix(fill.G), mix(fill.B), uint8(255 * a)})
		}
	}
	return img
}

func nearPulse(x, y float64) bool {
	const half = .045 // half the stroke width
	for i := 1; i < len(pulse); i++ {
		if segDist(x, y, pulse[i-1], pulse[i]) <= half {
			return true
		}
	}
	return false
}

func segDist(x, y float64, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	t := ((x-a[0])*dx + (y-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(x-(a[0]+t*dx), y-(a[1]+t*dy))
}

// ico wraps a PNG in a single-image ICO container (supported since Vista).
func ico(pngData []byte) []byte {
	var b bytes.Buffer
	le := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	le([3]uint16{0, 1, 1})                  // reserved, type icon, 1 image
	le([4]uint8{size, size, 0, 0})          // width, height, palette, reserved
	le([2]uint16{1, 32})                    // planes, bits per pixel
	le([2]uint32{uint32(len(pngData)), 22}) // data size, offset (6 + 16)
	b.Write(pngData)
	return b.Bytes()
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
}
