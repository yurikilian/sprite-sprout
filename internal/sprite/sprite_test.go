package sprite

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func testImage() Image {
	im := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := color.NRGBA{R: uint8(x * 30), G: uint8(y * 30), B: uint8((x + y) * 15), A: 255}
			if x == 0 && y == 0 {
				c = color.NRGBA{}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	return FromImage(im)
}

func TestDecodeAndScalePreservePixels(t *testing.T) {
	im := testImage()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBytes(buf.Bytes())
	if err != nil || decoded.Width != 8 || decoded.Height != 8 {
		t.Fatalf("decode: %#v %v", decoded, err)
	}
	scaled, err := ScaleNearest(im, 2)
	if err != nil {
		t.Fatal(err)
	}
	if scaled.Width != 16 || scaled.Height != 16 {
		t.Fatalf("unexpected dimensions: %dx%d", scaled.Width, scaled.Height)
	}
	if got := scaled.Pix[(2*scaled.Width+2)*4+3]; got != im.Pix[4+3] {
		t.Fatalf("scale alpha = %d", got)
	}
}

func TestQuantizersBoundPaletteAndKeepTransparency(t *testing.T) {
	im := testImage()
	for _, method := range []string{"octree", "weighted-octree", "median-cut", "octree-refine", "oklab-refine"} {
		out, palette, err := Quantize(im, 4, method)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if len(palette) == 0 || len(palette) > 4 {
			t.Fatalf("%s palette size %d", method, len(palette))
		}
		if out.Pix[3] != 0 {
			t.Fatalf("%s changed transparent pixel", method)
		}
		again, againPalette, err := Quantize(im, 4, method)
		if err != nil || !bytes.Equal(out.Pix, again.Pix) || !equalColors(palette, againPalette) {
			t.Fatalf("%s is not deterministic", method)
		}
	}
}

func equalColors(a, b []Color) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSnapAndProcess(t *testing.T) {
	im := testImage()
	snapped, err := SnapToGrid(im, 2)
	if err != nil {
		t.Fatal(err)
	}
	if snapped.Width != 4 || snapped.Height != 4 {
		t.Fatalf("snap dimensions: %dx%d", snapped.Width, snapped.Height)
	}
	result, err := Process(im, Recipe{Grid: 2, Colors: 4, Method: "median-cut", Scale: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Image.Width != 12 || result.Image.Height != 12 || result.GridSize != 2 || result.Scale != 3 || result.OutputColors > 4 {
		t.Fatalf("unexpected process result: %#v", result)
	}
	if result.BaseImage.Width != 4 || result.BaseImage.Height != 4 {
		t.Fatalf("unexpected base image dimensions: %dx%d", result.BaseImage.Width, result.BaseImage.Height)
	}
	odd := Image{Width: 5, Height: 3, Pix: make([]byte, 5*3*4)}
	if snappedOdd, err := SnapToGrid(odd, 2); err != nil || snappedOdd.Width != 2 || snappedOdd.Height != 1 {
		t.Fatalf("irregular grid snap: %#v %v", snappedOdd, err)
	}
}

func TestProcessContextHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ProcessContext(ctx, testImage(), Recipe{}); err != context.Canceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestRecipeValidation(t *testing.T) {
	if _, err := (Recipe{Version: 99}).Normalize(); err == nil {
		t.Fatal("expected version error")
	}
	if _, err := (Recipe{Method: "nope"}).Normalize(); err == nil {
		t.Fatal("expected method error")
	}
	if got, err := (Recipe{}).Normalize(); err != nil || got.Method != DefaultMethod || got.Scale != 1 {
		t.Fatalf("defaults: %#v %v", got, err)
	}
	if _, err := ParseRecipe([]byte(`{"version":1,"grid":0,"colors":0,"method":"octree-refine","scale":1} {}`)); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
}
