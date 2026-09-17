package sprite

import (
	"context"
	"fmt"
)

func SnapToGrid(im Image, grid int) (Image, error) {
	if !im.Valid() {
		return Image{}, errInvalidImage
	}
	if grid < 1 {
		return Image{}, fmt.Errorf("grid must be >= 1")
	}
	if grid == 1 {
		return Image{Width: im.Width, Height: im.Height, Pix: append([]byte(nil), im.Pix...)}, nil
	}
	w, h := im.Width/grid, im.Height/grid
	if w < 1 || h < 1 {
		return Image{}, fmt.Errorf("grid %d is too large for %dx%d", grid, im.Width, im.Height)
	}
	out := Image{Width: w, Height: h, Pix: make([]byte, w*h*4)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			freq := make(map[uint32]int)
			var sumA int
			for dy := 0; dy < grid; dy++ {
				for dx := 0; dx < grid; dx++ {
					i := ((y*grid+dy)*im.Width + x*grid + dx) * 4
					if im.Pix[i+3] == 0 {
						continue
					}
					k := pack(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
					freq[k]++
					sumA += int(im.Pix[i+3])
				}
			}
			var chosen uint32
			best := 0
			for k, n := range freq {
				if n > best {
					chosen, best = k, n
				}
			}
			dst := (y*w + x) * 4
			if best > grid*grid/2 {
				out.Pix[dst], out.Pix[dst+1], out.Pix[dst+2] = byte(chosen>>16), byte(chosen>>8), byte(chosen)
				out.Pix[dst+3] = uint8(sumA / (best))
			} else {
				var rs, gs, bs, as, count int
				for dy := 0; dy < grid; dy++ {
					for dx := 0; dx < grid; dx++ {
						i := ((y*grid+dy)*im.Width + x*grid + dx) * 4
						rs += int(im.Pix[i])
						gs += int(im.Pix[i+1])
						bs += int(im.Pix[i+2])
						as += int(im.Pix[i+3])
						count++
					}
				}
				if count > 0 {
					out.Pix[dst], out.Pix[dst+1], out.Pix[dst+2], out.Pix[dst+3] = uint8(rs/count), uint8(gs/count), uint8(bs/count), uint8(as/count)
				}
			}
		}
	}
	return out, nil
}

// AutoClean mirrors the editor's one-click flow: detect, snap, and reduce
// only when the snapped image has more than 32 opaque colors.
func AutoClean(im Image) (ProcessResult, error) {
	if !im.Valid() {
		return ProcessResult{}, errInvalidImage
	}
	detection := DetectGrid(im)
	snapped, err := SnapToGrid(im, detection.GridSize)
	if err != nil {
		return ProcessResult{}, err
	}
	colors := len(histogram(snapped))
	method := DefaultMethod
	final := snapped
	palette := topColors(snapped, 64)
	if colors > 32 {
		target := SuggestColorCount(colors)
		final, palette, err = Quantize(snapped, target, method)
		if err != nil {
			return ProcessResult{}, err
		}
	}
	return ProcessResult{Image: final, BaseImage: snapped, GridSize: detection.GridSize, OriginalWidth: im.Width, OriginalHeight: im.Height, OriginalColors: colors, OutputColors: len(palette), Method: method, Scale: 1, Palette: palette}, nil
}

func SuggestColorCount(unique int) int {
	if unique < 16 {
		return unique
	}
	if unique <= 64 {
		return 16
	}
	if unique <= 256 {
		return 24
	}
	return 32
}

func Process(im Image, recipe Recipe) (ProcessResult, error) {
	return ProcessContext(context.Background(), im, recipe)
}

func ProcessContext(ctx context.Context, im Image, recipe Recipe) (ProcessResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}
	r, err := recipe.Normalize()
	if err != nil {
		return ProcessResult{}, err
	}
	if !im.Valid() {
		return ProcessResult{}, errInvalidImage
	}
	grid := r.Grid
	if grid == 0 {
		grid = DetectGrid(im).GridSize
	}
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}
	working, err := SnapToGrid(im, grid)
	if err != nil {
		return ProcessResult{}, err
	}
	// Keep the grid-snapped pixels so the editor can reapply colour changes
	// without losing the geometry adjustment when a native operation returns.
	baseImage := working
	before := len(histogram(working))
	palette := topColors(working, 256)
	method := r.Method
	if r.Colors > 0 && r.Colors < 256 {
		if err := ctx.Err(); err != nil {
			return ProcessResult{}, err
		}
		working, palette, err = Quantize(working, r.Colors, method)
		if err != nil {
			return ProcessResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}
	outputColors := len(histogram(working))
	scaled, err := ScaleNearest(working, r.Scale)
	if err != nil {
		return ProcessResult{}, err
	}
	return ProcessResult{Image: scaled, BaseImage: baseImage, GridSize: grid, OriginalWidth: im.Width, OriginalHeight: im.Height, OriginalColors: before, OutputColors: outputColors, Method: method, Scale: r.Scale, Palette: palette}, nil
}
