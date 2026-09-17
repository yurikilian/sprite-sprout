package sprite

import "sort"

func Analyze(im Image) (Analysis, error) {
	if !im.Valid() {
		return Analysis{}, errInvalidImage
	}
	colors := make(map[uint32]int)
	totalRuns, totalRunLength := 0, 0
	for y := 0; y < im.Height; y++ {
		prev := uint32(0)
		run := 0
		for x := 0; x < im.Width; x++ {
			i := (y*im.Width + x) * 4
			a := im.Pix[i+3]
			if a == 0 {
				if run > 0 {
					totalRuns++
					totalRunLength += run
				}
				run = 0
				continue
			}
			key := pack(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
			colors[key]++
			if run == 0 {
				prev, run = key, 1
			} else if key == prev {
				run++
			} else {
				totalRuns++
				totalRunLength += run
				prev, run = key, 1
			}
		}
		if run > 0 {
			totalRuns++
			totalRunLength += run
		}
	}
	avgRun := 1.0
	if totalRuns > 0 {
		avgRun = float64(totalRunLength) / float64(totalRuns)
	}
	grid := DetectGrid(im)
	suggested := make([]int, 0, 4)
	for _, candidate := range []int{2, 4, 8, 16} {
		if im.Width%candidate == 0 && im.Height%candidate == 0 {
			suggested = append(suggested, candidate)
		}
	}
	return Analysis{Width: im.Width, Height: im.Height, UniqueColorCount: len(colors), SuggestedGridSizes: suggested, LooksLikePixelArt: avgRun > 2, DetectedGrid: grid.GridSize, Confidence: grid.Confidence, Candidates: grid.Candidates}, nil
}

var errInvalidImage = &imageError{"invalid image buffer"}

type imageError struct{ message string }

func (e *imageError) Error() string { return e.message }

func pack(r, g, b byte) uint32 { return uint32(r)<<16 | uint32(g)<<8 | uint32(b) }

type colorRun struct {
	key   uint32
	count int
}

func DetectGrid(im Image) GridDetectionResult {
	if im.Width <= 1 || im.Height <= 1 {
		return GridDetectionResult{GridSize: 1, Confidence: 1, Candidates: []GridCandidate{{Size: 1, Score: 1}}, LogicalWidth: im.Width, LogicalHeight: im.Height}
	}
	runs := map[int]int{}
	for y := 0; y < im.Height; y++ {
		run := 1
		prev := colorAt(im, 0, y)
		for x := 1; x < im.Width; x++ {
			cur := colorAt(im, x, y)
			if colorDistance(prev, cur) < 30 {
				run++
			} else {
				if run >= 2 {
					runs[run]++
				}
				run = 1
				prev = cur
			}
		}
		if run >= 2 {
			runs[run]++
		}
	}
	for x := 0; x < im.Width; x++ {
		run := 1
		prev := colorAt(im, x, 0)
		for y := 1; y < im.Height; y++ {
			cur := colorAt(im, x, y)
			if colorDistance(prev, cur) < 30 {
				run++
			} else {
				if run >= 2 {
					runs[run]++
				}
				run = 1
				prev = cur
			}
		}
		if run >= 2 {
			runs[run]++
		}
	}
	total := 0
	for _, n := range runs {
		total += n
	}
	if total == 0 {
		total = 1
	}
	maxG := im.Width
	if im.Height > maxG {
		maxG = im.Height
	}
	if maxG > 32 {
		maxG = 32
	}
	edges := make(map[int]float64)
	totalGradient := 0.0
	h := make([]float64, im.Height*(im.Width-1))
	for y := 0; y < im.Height; y++ {
		for x := 0; x < im.Width-1; x++ {
			g := gradient(colorAt(im, x, y), colorAt(im, x+1, y))
			h[y*(im.Width-1)+x] = g
			totalGradient += g
		}
	}
	v := make([]float64, (im.Height-1)*im.Width)
	for y := 0; y < im.Height-1; y++ {
		for x := 0; x < im.Width; x++ {
			g := gradient(colorAt(im, x, y), colorAt(im, x, y+1))
			v[y*im.Width+x] = g
			totalGradient += g
		}
	}
	for g := 2; g <= maxG; g++ {
		aligned := 0.0
		for y := 0; y < im.Height; y++ {
			for col := g; col < im.Width; col += g {
				aligned += h[y*(im.Width-1)+col-1]
			}
		}
		for x := 0; x < im.Width; x++ {
			for row := g; row < im.Height; row += g {
				aligned += v[(row-1)*im.Width+x]
			}
		}
		if totalGradient > 0 {
			edges[g] = aligned / totalGradient
		} else {
			edges[g] = 0
		}
	}
	combined := make(map[int]float64)
	combined[1] = 0
	for g, n := range runs {
		if g >= 2 && g <= maxG {
			combined[g] = float64(n)/float64(total)*0.4 + edges[g]*0.6
		}
	}
	for g, score := range edges {
		if _, ok := combined[g]; !ok {
			combined[g] = score * 0.6
		}
	}
	for small, score := range combined {
		if small < 2 {
			continue
		}
		for big, bigScore := range combined {
			if big > small && big%small == 0 && bigScore >= score*0.7 {
				combined[small] = score * 0.3
				break
			}
		}
	}
	items := make([]GridCandidate, 0, len(combined))
	for g, s := range combined {
		items = append(items, GridCandidate{Size: g, Score: s})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	if len(items) > 3 {
		items = items[:3]
	}
	best := 1
	bestScore := 0.0
	if len(items) > 0 {
		best, bestScore = items[0].Size, items[0].Score
	}
	second := 0.0
	if len(items) > 1 {
		second = items[1].Score
	}
	runsMode := 1
	runBest := 0
	for g, n := range runs {
		if n > runBest {
			runBest = n
			runsMode = g
		}
	}
	confidence := 0.0
	if runsMode == best {
		confidence += 0.3
	}
	if bestScore > 0 {
		confidence += (bestScore-second)/bestScore*0.5 + bestScore*0.5
	}
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return GridDetectionResult{GridSize: best, Confidence: confidence, Candidates: items, LogicalWidth: im.Width / best, LogicalHeight: im.Height / best}
}

type GridDetectionResult struct {
	GridSize      int
	Confidence    float64
	Candidates    []GridCandidate
	LogicalWidth  int
	LogicalHeight int
}

func colorAt(im Image, x, y int) [3]byte {
	i := (y*im.Width + x) * 4
	return [3]byte{im.Pix[i], im.Pix[i+1], im.Pix[i+2]}
}
func colorDistance(a, b [3]byte) int {
	return abs(int(a[0])-int(b[0])) + abs(int(a[1])-int(b[1])) + abs(int(a[2])-int(b[2]))
}
func gradient(a, b [3]byte) float64 { return float64(colorDistance(a, b)) }
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
