package iconstore

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
)

const defaultIconSize = 256

var defaultPalette = []color.RGBA{
	{R: 0x58, G: 0x65, B: 0xF2, A: 0xFF}, // blurple
	{R: 0xED, G: 0x42, B: 0x45, A: 0xFF}, // red
	{R: 0x57, G: 0xF2, B: 0x87, A: 0xFF}, // green
	{R: 0xFE, G: 0xE7, B: 0x5C, A: 0xFF}, // yellow
	{R: 0xEB, G: 0x45, B: 0x9E, A: 0xFF}, // pink
	{R: 0x5B, G: 0xC0, B: 0xDE, A: 0xFF}, // cyan
	{R: 0xF0, G: 0x93, B: 0x2A, A: 0xFF}, // orange
	{R: 0x9B, G: 0x59, B: 0xB6, A: 0xFF}, // purple
}

func GenerateDefaultIcon() ([]byte, error) {
	c := defaultPalette[rand.IntN(len(defaultPalette))]

	img := image.NewRGBA(image.Rect(0, 0, defaultIconSize, defaultIconSize))
	for y := 0; y < defaultIconSize; y++ {
		for x := 0; x < defaultIconSize; x++ {
			img.SetRGBA(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *LocalStore) GenerateDefault() ([]byte, error) {
	return GenerateDefaultIcon()
}
