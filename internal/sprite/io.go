package sprite

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp"
)

const (
	MaxDimension    = 4096
	MaxPixels       = 16_777_216
	MaxEncodedBytes = 64 * 1024 * 1024
)

func Decode(r io.Reader) (Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxEncodedBytes+1))
	if err != nil {
		return Image{}, fmt.Errorf("read image: %w", err)
	}
	if len(data) > MaxEncodedBytes {
		return Image{}, fmt.Errorf("encoded image exceeds the %d MiB limit", MaxEncodedBytes/(1024*1024))
	}
	return DecodeBytes(data)
}

func DecodeBytes(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, fmt.Errorf("image is empty")
	}
	if len(data) > MaxEncodedBytes {
		return Image{}, fmt.Errorf("encoded image exceeds the %d MiB limit", MaxEncodedBytes/(1024*1024))
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("read image metadata: %w", err)
	}
	return decodeBytes(data, config)
}

func decodeBytes(data []byte, config image.Config) (Image, error) {
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaxDimension || config.Height > MaxDimension || int64(config.Width)*int64(config.Height) > MaxPixels {
		return Image{}, fmt.Errorf("image dimensions %dx%d exceed the %dx%d / %d-pixel limit", config.Width, config.Height, MaxDimension, MaxDimension, MaxPixels)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("decode image: %w", err)
	}
	return FromImage(decoded), nil
}

func DecodeFile(path string) (Image, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Image{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > MaxEncodedBytes {
		return Image{}, fmt.Errorf("encoded image exceeds the %d MiB limit", MaxEncodedBytes/(1024*1024))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Image{}, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > MaxEncodedBytes {
		return Image{}, fmt.Errorf("encoded image exceeds the %d MiB limit", MaxEncodedBytes/(1024*1024))
	}
	return DecodeBytes(data)
}

func FromImage(src image.Image) Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)
		}
	}
	return Image{Width: w, Height: h, Pix: pix}
}

func EncodePNG(w io.Writer, im Image) error {
	if !im.Valid() {
		return fmt.Errorf("invalid image buffer")
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, im.Width, im.Height))
	copy(nrgba.Pix, im.Pix)
	return png.Encode(w, nrgba)
}

func EncodePNGBytes(im Image) ([]byte, error) {
	var b bytes.Buffer
	if err := EncodePNG(&b, im); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func WritePNGAtomic(path string, im Image) error {
	if !im.Valid() {
		return fmt.Errorf("invalid image buffer")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sprout-*.png")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := EncodePNG(tmp, im); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func ScaleNearest(im Image, scale int) (Image, error) {
	if !im.Valid() || scale < 1 {
		return Image{}, fmt.Errorf("invalid image or scale")
	}
	if scale == 1 {
		return Image{Width: im.Width, Height: im.Height, Pix: append([]byte(nil), im.Pix...)}, nil
	}
	if int64(im.Width)*int64(scale) > MaxDimension || int64(im.Height)*int64(scale) > MaxDimension || int64(im.Width)*int64(im.Height)*int64(scale)*int64(scale) > MaxPixels {
		return Image{}, fmt.Errorf("scaled image dimensions exceed the %dx%d / %d-pixel limit", MaxDimension, MaxDimension, MaxPixels)
	}
	out := Image{Width: im.Width * scale, Height: im.Height * scale, Pix: make([]byte, im.Width*im.Height*scale*scale*4)}
	for y := 0; y < out.Height; y++ {
		for x := 0; x < out.Width; x++ {
			src := ((y/scale)*im.Width + x/scale) * 4
			dst := (y*out.Width + x) * 4
			copy(out.Pix[dst:dst+4], im.Pix[src:src+4])
		}
	}
	return out, nil
}

// Keep format registrations explicit for users of image.Decode outside this
// package and document supported input families in the binary.
var _ = []any{gif.GIF{}, jpeg.Options{}, png.Encoder{}}
