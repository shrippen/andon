package sources

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"

	// Decoders for image.Decode.
	_ "image/gif"
	_ "image/png"
)

// Thumbnail box: a feed tile shows its picture at 4.5 × 3.2 rem
// (72 × 51 CSS px), cropped to cover; three device pixels per CSS pixel
// keep it sharp on phones.
const (
	thumbW       = 216
	thumbH       = 154
	thumbQuality = 75
	// thumbPixels caps what is decoded: a 150 KB PNG may claim 20000 ×
	// 20000 pixels (1.6 GB once decoded).
	thumbPixels = 8_000_000
	dataPrefix  = "data:"
	base64Mark  = ";base64,"
)

// thumbnail scales a data: URI picture down to cover the thumbnail box,
// as a JPEG data: URI. "" when it cannot (WebP, SVG, broken data) or
// when the picture is small already: the caller then shows the original.
//
//	1920 × 1080 JPEG, 140 KB  ─►  274 × 154 JPEG, ~10 KB
func thumbnail(uri string) string {
	head, payload, ok := strings.Cut(strings.TrimPrefix(uri, dataPrefix), base64Mark)
	if !ok || !strings.HasPrefix(head, imagePrefix) {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width*cfg.Height > thumbPixels {
		return ""
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return ""
	}

	size := src.Bounds().Size()
	scale := max(float64(thumbW)/float64(size.X), float64(thumbH)/float64(size.Y))
	if scale >= 1 {
		return ""
	}
	w, h := max(1, int(float64(size.X)*scale)), max(1, int(float64(size.Y)*scale))

	var out bytes.Buffer
	if err := jpeg.Encode(&out, shrink(src, w, h), &jpeg.Options{Quality: thumbQuality}); err != nil {
		return ""
	}
	return dataPrefix + "image/jpeg" + base64Mark + base64.StdEncoding.EncodeToString(out.Bytes())
}

// shrink scales src down to w × h by averaging the source pixels under
// each target pixel (box filter: no aliasing when shrinking a lot).
// The picture is first painted onto white (JPEG has no alpha); draw has
// fast paths for the usual formats, src.At per pixel would be ~10× slower.
func shrink(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	flat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), src, b.Min, draw.Over)

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := y*b.Dy()/h, max((y+1)*b.Dy()/h, y*b.Dy()/h+1)
		for x := range w {
			x0, x1 := x*b.Dx()/w, max((x+1)*b.Dx()/w, x*b.Dx()/w+1)
			dst.SetRGBA(x, y, average(flat, x0, y0, x1, y1))
		}
	}
	return dst
}

// average is the mean colour of img in [x0,x1) × [y0,y1).
func average(img *image.RGBA, x0, y0, x1, y1 int) color.RGBA {
	var r, g, b, n int
	for y := y0; y < y1; y++ {
		row := img.Pix[y*img.Stride+x0*4 : y*img.Stride+x1*4]
		for i := 0; i < len(row); i += 4 {
			r, g, b = r+int(row[i]), g+int(row[i+1]), b+int(row[i+2])
			n++
		}
	}
	return color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: 0xff}
}
