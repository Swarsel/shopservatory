package source

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	_ "image/gif"
	_ "image/png"
)

const searchImageMaxDim = 380

func NormalizeSearchImage(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	b := src.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return nil, fmt.Errorf("empty image")
	}
	factor := 1
	for b.Dx()/factor > searchImageMaxDim || b.Dy()/factor > searchImageMaxDim {
		factor++
	}
	dst := boxDownscale(src, factor)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}
	return buf.Bytes(), nil
}

func boxDownscale(src image.Image, factor int) *image.RGBA {
	b := src.Bounds()
	ow, oh := b.Dx()/factor, b.Dy()/factor
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	n := uint32(factor * factor)
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			var r, g, bl, a uint32
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					pr, pg, pb, pa := src.At(b.Min.X+x*factor+dx, b.Min.Y+y*factor+dy).RGBA()
					r += pr >> 8
					g += pg >> 8
					bl += pb >> 8
					a += pa >> 8
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), uint8(a / n)})
		}
	}
	return dst
}
