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
// keep it sharp on phones. The detail dialog shows one up to wideW wide.
const (
	thumbW       = 216
	thumbH       = 154
	thumbQuality = 75
	wideW        = 960
	wideQuality  = 80
	// thumbPixels caps what is decoded: a small PNG may claim 20000 ×
	// 20000 pixels (1.6 GB once decoded).
	thumbPixels = 8_000_000
	dataPrefix  = "data:"
	base64Mark  = ";base64,"
)

// pictureSizes scales a data: URI picture down, decoded once: thumb
// covers the tile's box, wide fits wideW across for the detail dialog.
// Either is "" when the picture is that small already or cannot be
// scaled (WebP, SVG, broken data); the caller keeps the original then.
//
//	1920 × 1080 JPEG, 190 KB  ─►  thumb 274 × 154 (~10 KB), wide 960 × 540 (~60 KB)
func pictureSizes(uri string) (thumb, wide string) {
	src := decodePicture(uri)
	if src == nil {
		return "", ""
	}
	size := src.Bounds().Size()
	thumb = scaledJPEG(src, max(float64(thumbW)/float64(size.X), float64(thumbH)/float64(size.Y)), thumbQuality)
	wide = scaledJPEG(src, float64(wideW)/float64(size.X), wideQuality)
	return thumb, wide
}

// decodePicture decodes a data: URI picture, nil if it is not one the
// standard library reads or claims too many pixels.
func decodePicture(uri string) image.Image {
	head, payload, ok := strings.Cut(strings.TrimPrefix(uri, dataPrefix), base64Mark)
	if !ok || !strings.HasPrefix(head, imagePrefix) {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width*cfg.Height > thumbPixels {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	return src
}

// scaledJPEG is src scaled by scale as a JPEG data: URI, "" for a scale
// that would not shrink it.
func scaledJPEG(src image.Image, scale float64, quality int) string {
	if scale >= 1 {
		return ""
	}
	size := src.Bounds().Size()
	w, h := max(1, int(float64(size.X)*scale)), max(1, int(float64(size.Y)*scale))

	var out bytes.Buffer
	if err := jpeg.Encode(&out, shrink(src, w, h), &jpeg.Options{Quality: quality}); err != nil {
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
