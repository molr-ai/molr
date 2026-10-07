package assistant

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	avatarutil "molr.ai/runtime/internal/avatar"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const AvatarImageLimit = 10 << 20

// AvatarFromImage bounds decoding before allocation and stores only a thumbnail.
func AvatarFromImage(data []byte) (string, error) {
	if len(data) == 0 || len(data) > AvatarImageLimit {
		return "", invalid("avatar")
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") || c.Width < 1 || c.Height < 1 || int64(c.Width)*int64(c.Height) > 40_000_000 {
		return "", invalid("avatar")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", invalid("avatar")
	}
	b := src.Bounds()
	size := min(b.Dx(), b.Dy())
	x, y := b.Min.X+(b.Dx()-size)/2, b.Min.Y+(b.Dy()-size)/2
	thumb := image.NewRGBA(image.Rect(0, 0, 256, 256))
	// White is encoded image content, matching browser uploads, not a UI color.
	draw.Draw(thumb, thumb.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(thumb, thumb.Bounds(), src, image.Rect(x, y, x+size, y+size), draw.Over, nil)
	var out bytes.Buffer
	if err = jpeg.Encode(&out, thumb, &jpeg.Options{Quality: 85}); err != nil {
		return "", invalid("avatar")
	}
	return avatarutil.Normalize("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()))
}
