package gif

import (
	"bytes"
	"image"
	"image/color/palette"
	"image/gif"
	"image/jpeg"

	"golang.org/x/image/draw"
)

// Build takes raw JPEG frame bytes and returns an animated GIF.
func Build(frames [][]byte, delayMs int) ([]byte, error) {
	if len(frames) == 0 {
		return nil, nil
	}
	out := &gif.GIF{}
	for _, raw := range frames {
		img, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		if img == nil {
			continue
		}
		resized := Resize(img, 640)
		paletted := image.NewPaletted(resized.Bounds(), palette.Plan9)
		draw.Draw(paletted, resized.Bounds(), resized, resized.Bounds().Min, draw.Src)
		out.Image = append(out.Image, paletted)
		out.Delay = append(out.Delay, delayMs/10)
	}
	if len(out.Image) == 0 {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Resize(src image.Image, maxW int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW {
		return src
	}
	nw := maxW
	nh := h * maxW / w
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}
