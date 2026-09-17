package sprite

import (
	"math"
	"sort"
)

// histColor is one opaque RGB colour and its frequency in the image.
type histColor struct {
	r, g, b byte
	count   int
}

type bucket struct {
	entries                            []histColor
	population                         int
	rMin, rMax, gMin, gMax, bMin, bMax byte
}

// Quantize implements the five algorithms exposed by the editor. The
// octree and weighted-octree variants share the same deterministic tree; the
// weighted reduction order preserves high-frequency colours. Refine variants
// run k-means in CIELAB or OKLab after the initial octree palette.
func Quantize(im Image, target int, method string) (Image, []Color, error) {
	if !im.Valid() {
		return Image{}, nil, errInvalidImage
	}
	if !ValidMethod(method) {
		return Image{}, nil, &imageError{"unknown quantization method " + method}
	}
	if target <= 0 || target >= 256 {
		return Image{Width: im.Width, Height: im.Height, Pix: append([]byte(nil), im.Pix...)}, topColors(im, 256), nil
	}
	h := histogram(im)
	if len(h) == 0 {
		return Image{Width: im.Width, Height: im.Height, Pix: append([]byte(nil), im.Pix...)}, nil, nil
	}
	if target > len(h) {
		target = len(h)
	}
	var palette []Color
	switch method {
	case "median-cut":
		palette = medianCut(h, target)
	case "weighted-octree":
		palette = octreePalette(im, target, true)
	case "octree", "octree-refine", "oklab-refine":
		palette = octreePalette(im, target, false)
	}
	if method == "octree-refine" {
		palette = refinePalette(im, palette, false)
	}
	if method == "oklab-refine" {
		palette = refinePalette(im, palette, true)
	}
	return remap(im, palette, method == "oklab-refine"), palette, nil
}

func histogram(im Image) []histColor {
	m := make(map[uint32]*histColor)
	for i := 0; i < len(im.Pix); i += 4 {
		if im.Pix[i+3] == 0 {
			continue
		}
		key := pack(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
		if entry, ok := m[key]; ok {
			entry.count++
		} else {
			m[key] = &histColor{r: im.Pix[i], g: im.Pix[i+1], b: im.Pix[i+2], count: 1}
		}
	}
	out := make([]histColor, 0, len(m))
	for _, entry := range m {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		if out[i].r != out[j].r {
			return out[i].r < out[j].r
		}
		if out[i].g != out[j].g {
			return out[i].g < out[j].g
		}
		return out[i].b < out[j].b
	})
	return out
}

func topColors(im Image, n int) []Color {
	h := histogram(im)
	if n > len(h) {
		n = len(h)
	}
	p := make([]Color, n)
	for i := range p {
		p[i] = Color{R: h[i].r, G: h[i].g, B: h[i].b, A: 255}
	}
	return p
}

func makeBucket(entries []histColor) bucket {
	b := bucket{entries: entries, rMin: 255, gMin: 255, bMin: 255}
	for _, entry := range entries {
		b.population += entry.count
		if entry.r < b.rMin {
			b.rMin = entry.r
		}
		if entry.r > b.rMax {
			b.rMax = entry.r
		}
		if entry.g < b.gMin {
			b.gMin = entry.g
		}
		if entry.g > b.gMax {
			b.gMax = entry.g
		}
		if entry.b < b.bMin {
			b.bMin = entry.b
		}
		if entry.b > b.bMax {
			b.bMax = entry.b
		}
	}
	return b
}

func medianCut(entries []histColor, target int) []Color {
	buckets := []bucket{makeBucket(entries)}
	for len(buckets) < target {
		best := -1
		bestScore := int64(-1)
		for i, current := range buckets {
			if len(current.entries) < 2 {
				continue
			}
			volume := int64(current.rMax-current.rMin+1) * int64(current.gMax-current.gMin+1) * int64(current.bMax-current.bMin+1)
			score := int64(current.population) * volume
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		current := buckets[best]
		rRange := current.rMax - current.rMin
		gRange := current.gMax - current.gMin
		bRange := current.bMax - current.bMin
		channel := 0
		if gRange > rRange && gRange >= bRange {
			channel = 1
		} else if bRange > rRange && bRange > gRange {
			channel = 2
		}
		sort.SliceStable(current.entries, func(i, j int) bool {
			return channelValue(current.entries[i], channel) < channelValue(current.entries[j], channel)
		})
		half := float64(current.population) / 2
		cumulative, split := 0, 1
		for i, entry := range current.entries[:len(current.entries)-1] {
			cumulative += entry.count
			if float64(cumulative) >= half {
				split = i + 1
				break
			}
		}
		buckets[best] = makeBucket(current.entries[:split])
		buckets = append(buckets, makeBucket(current.entries[split:]))
	}
	return bucketPalette(buckets)
}

func bucketPalette(buckets []bucket) []Color {
	palette := make([]Color, 0, len(buckets))
	for _, current := range buckets {
		var rSum, gSum, bSum int64
		for _, entry := range current.entries {
			rSum += int64(entry.r) * int64(entry.count)
			gSum += int64(entry.g) * int64(entry.count)
			bSum += int64(entry.b) * int64(entry.count)
		}
		if current.population > 0 {
			palette = append(palette, Color{R: uint8(math.Round(float64(rSum) / float64(current.population))), G: uint8(math.Round(float64(gSum) / float64(current.population))), B: uint8(math.Round(float64(bSum) / float64(current.population))), A: 255})
		}
	}
	return palette
}

func channelValue(entry histColor, channel int) byte {
	switch channel {
	case 1:
		return entry.g
	case 2:
		return entry.b
	default:
		return entry.r
	}
}

type octNode struct {
	rSum, gSum, bSum int64
	pixelCount       int
	paletteIndex     int
	children         [8]*octNode
	isLeaf           bool
	depth            int
}

func newOctNode(depth int) *octNode {
	return &octNode{depth: depth, paletteIndex: -1}
}

func octChildIndex(r, g, b byte, depth int) int {
	bit := uint(7 - depth)
	return int(((r>>bit)&1)<<2 | ((g>>bit)&1)<<1 | ((b >> bit) & 1))
}

func insertOctree(root *octNode, r, g, b byte) {
	node := root
	for depth := 0; depth < 7; depth++ {
		idx := octChildIndex(r, g, b, depth)
		if node.children[idx] == nil {
			node.children[idx] = newOctNode(depth + 1)
			if depth+1 == 7 {
				node.children[idx].isLeaf = true
			}
		}
		node = node.children[idx]
	}
	node.rSum += int64(r)
	node.gSum += int64(g)
	node.bSum += int64(b)
	node.pixelCount++
}

func countLeaves(node *octNode) int {
	if node == nil {
		return 0
	}
	if node.isLeaf {
		if node.pixelCount > 0 {
			return 1
		}
		return 0
	}
	count := 0
	for _, child := range node.children {
		count += countLeaves(child)
	}
	return count
}

func countPixels(node *octNode) int {
	if node == nil {
		return 0
	}
	total := node.pixelCount
	for _, child := range node.children {
		total += countPixels(child)
	}
	return total
}

func countPaletteEntries(node *octNode) int {
	if node == nil {
		return 0
	}
	if node.isLeaf {
		if node.pixelCount > 0 {
			return 1
		}
		return 0
	}
	count := 0
	if node.pixelCount > 0 {
		count++
	}
	for _, child := range node.children {
		count += countPaletteEntries(child)
	}
	return count
}

func collapseIntoSelf(node *octNode) {
	if node == nil {
		return
	}
	for i, child := range node.children {
		if child == nil {
			continue
		}
		collapseIntoSelf(child)
		node.rSum += child.rSum
		node.gSum += child.gSum
		node.bSum += child.bSum
		node.pixelCount += child.pixelCount
		node.children[i] = nil
	}
	node.isLeaf = true
}

func gatherInto(target, node *octNode) {
	if node == nil {
		return
	}
	target.rSum += node.rSum
	target.gSum += node.gSum
	target.bSum += node.bSum
	target.pixelCount += node.pixelCount
	for _, child := range node.children {
		gatherInto(target, child)
	}
}

func absorbChild(target *octNode, childIndex int) int {
	child := target.children[childIndex]
	if child == nil {
		return 0
	}
	leaves := countLeaves(child)
	gatherInto(target, child)
	target.children[childIndex] = nil
	return leaves
}

func octreePalette(im Image, target int, weighted bool) []Color {
	root := newOctNode(0)
	leafCount := 0
	for i := 0; i < len(im.Pix); i += 4 {
		if im.Pix[i+3] == 0 {
			continue
		}
		insertOctree(root, im.Pix[i], im.Pix[i+1], im.Pix[i+2])
	}
	// Count only occupied leaves (all inserted colours end at depth 7).
	leafCount = countLeaves(root)
	var byDepth [8][]*octNode
	var collect func(*octNode)
	collect = func(node *octNode) {
		if node == nil || node.isLeaf {
			return
		}
		for _, child := range node.children {
			collect(child)
		}
		byDepth[node.depth] = append(byDepth[node.depth], node)
	}
	collect(root)
	for depth := 6; depth >= 0 && leafCount > target; depth-- {
		nodes := byDepth[depth]
		sort.SliceStable(nodes, func(i, j int) bool {
			if weighted {
				return countPixels(nodes[i]) < countPixels(nodes[j])
			}
			return countLeaves(nodes[i]) < countLeaves(nodes[j])
		})
		for _, node := range nodes {
			if node.isLeaf || leafCount <= target {
				continue
			}
			leaves := countLeaves(node)
			if leaves <= 1 || leafCount-(leaves-1) < target {
				continue
			}
			collapseIntoSelf(node)
			leafCount -= leaves - 1
		}
	}
	for leafCount > target {
		var best *octNode
		var find func(*octNode)
		find = func(node *octNode) {
			if node == nil || node.isLeaf {
				return
			}
			children := 0
			for _, child := range node.children {
				if child != nil {
					children++
					find(child)
				}
			}
			if children >= 2 && (best == nil || node.depth < best.depth) {
				best = node
			}
		}
		find(root)
		if best == nil {
			break
		}
		minLeaves, minIndex := math.MaxInt, -1
		for i, child := range best.children {
			if child == nil {
				continue
			}
			leaves := countLeaves(child)
			if leaves < minLeaves {
				minLeaves, minIndex = leaves, i
			}
		}
		if minIndex < 0 {
			break
		}
		leafCount -= absorbChild(best, minIndex)
		remaining := 0
		for _, child := range best.children {
			if child != nil {
				remaining++
			}
		}
		if remaining == 0 {
			best.isLeaf = true
			leafCount++
		}
		leafCount = countPaletteEntries(root)
	}

	palette := make([]Color, 0, target)
	var assign func(*octNode)
	assign = func(node *octNode) {
		if node == nil {
			return
		}
		if node.isLeaf {
			if node.pixelCount > 0 {
				node.paletteIndex = len(palette)
				palette = append(palette, averageNode(node))
			}
			return
		}
		if node.pixelCount > 0 {
			node.paletteIndex = len(palette)
			palette = append(palette, averageNode(node))
		}
		for _, child := range node.children {
			assign(child)
		}
	}
	assign(root)
	return palette
}

func averageNode(node *octNode) Color {
	if node.pixelCount == 0 {
		return Color{A: 255}
	}
	return Color{R: uint8(math.Round(float64(node.rSum) / float64(node.pixelCount))), G: uint8(math.Round(float64(node.gSum) / float64(node.pixelCount))), B: uint8(math.Round(float64(node.bSum) / float64(node.pixelCount))), A: 255}
}

func remap(im Image, palette []Color, oklab bool) Image {
	out := Image{Width: im.Width, Height: im.Height, Pix: make([]byte, len(im.Pix))}
	if len(palette) == 0 {
		return out
	}
	for i := 0; i < len(im.Pix); i += 4 {
		if im.Pix[i+3] == 0 {
			continue
		}
		best := nearestPalette(im.Pix[i], im.Pix[i+1], im.Pix[i+2], palette, oklab)
		color := palette[best]
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = color.R, color.G, color.B, 255
	}
	return out
}

func nearestPalette(r, g, b byte, palette []Color, oklab bool) int {
	best, bestDistance := 0, math.Inf(1)
	var value [3]float64
	if oklab {
		value = rgbToOklab(r, g, b)
	}
	for i, color := range palette {
		var distance float64
		if oklab {
			d := oklabDistance(value, rgbToOklab(color.R, color.G, color.B))
			distance = d * d
		} else {
			dr := float64(r) - float64(color.R)
			dg := float64(g) - float64(color.G)
			db := float64(b) - float64(color.B)
			distance = dr*dr + dg*dg + db*db
		}
		if distance < bestDistance {
			best, bestDistance = i, distance
		}
	}
	return best
}

func refinePalette(im Image, initial []Color, useOklab bool) []Color {
	if len(initial) == 0 {
		return initial
	}
	palette := append([]Color(nil), initial...)
	centers := make([][3]float64, len(palette))
	for i, color := range palette {
		if useOklab {
			centers[i] = rgbToOklab(color.R, color.G, color.B)
		} else {
			centers[i] = rgbToLab(color.R, color.G, color.B)
		}
	}
	for iteration := 0; iteration < 3; iteration++ {
		sums := make([][3]float64, len(palette))
		rgbSums := make([][3]float64, len(palette))
		counts := make([]int, len(palette))
		for i := 0; i < len(im.Pix); i += 4 {
			if im.Pix[i+3] == 0 {
				continue
			}
			var value [3]float64
			if useOklab {
				value = rgbToOklab(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
			} else {
				value = rgbToLab(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
			}
			best, bestDistance := 0, math.Inf(1)
			for j, center := range centers {
				d0, d1, d2 := value[0]-center[0], value[1]-center[1], value[2]-center[2]
				distance := d0*d0 + d1*d1 + d2*d2
				if distance < bestDistance {
					best, bestDistance = j, distance
				}
			}
			for k := 0; k < 3; k++ {
				sums[best][k] += value[k]
			}
			if !useOklab {
				rgbSums[best][0] += float64(im.Pix[i])
				rgbSums[best][1] += float64(im.Pix[i+1])
				rgbSums[best][2] += float64(im.Pix[i+2])
			}
			counts[best]++
		}
		converged := true
		for i := range centers {
			if counts[i] == 0 {
				continue
			}
			if useOklab {
				next := [3]float64{}
				for k := 0; k < 3; k++ {
					next[k] = sums[i][k] / float64(counts[i])
				}
				r, g, b := okToRGB(next)
				if absInt(int(r)-int(palette[i].R)) > 1 || absInt(int(g)-int(palette[i].G)) > 1 || absInt(int(b)-int(palette[i].B)) > 1 {
					converged = false
				}
				palette[i].R, palette[i].G, palette[i].B = r, g, b
				centers[i] = rgbToOklab(r, g, b)
			} else {
				r := uint8(math.Round(rgbSums[i][0] / float64(counts[i])))
				g := uint8(math.Round(rgbSums[i][1] / float64(counts[i])))
				b := uint8(math.Round(rgbSums[i][2] / float64(counts[i])))
				if absInt(int(r)-int(palette[i].R)) > 1 || absInt(int(g)-int(palette[i].G)) > 1 || absInt(int(b)-int(palette[i].B)) > 1 {
					converged = false
				}
				palette[i].R, palette[i].G, palette[i].B = r, g, b
				centers[i] = rgbToLab(r, g, b)
			}
			palette[i].A = 255
		}
		if converged {
			break
		}
	}
	return palette
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func rgbToLab(r, g, b byte) [3]float64 {
	linear := func(value byte) float64 {
		v := float64(value) / 255
		if v > 0.04045 {
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return v / 12.92
	}
	rLin, gLin, bLin := linear(r), linear(g), linear(b)
	x := (rLin*0.4124564 + gLin*0.3575761 + bLin*0.1804375) / 0.95047
	y := gLin*0.7151522 + rLin*0.2126729 + bLin*0.0721750
	z := (rLin*0.0193339 + gLin*0.1191920 + bLin*0.9503041) / 1.08883
	f := func(value float64) float64 {
		if value > 0.008856 {
			return math.Cbrt(value)
		}
		return (903.3*value + 16) / 116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

func rgbToOklab(r, g, b byte) [3]float64 {
	linear := func(value byte) float64 {
		v := float64(value) / 255
		if v > 0.04045 {
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return v / 12.92
	}
	rLin, gLin, bLin := linear(r), linear(g), linear(b)
	l := 0.4122214708*rLin + 0.5363325363*gLin + 0.0514459929*bLin
	m := 0.2119034982*rLin + 0.6806995451*gLin + 0.1073969566*bLin
	s := 0.0883024619*rLin + 0.2220049401*gLin + 0.6896926080*bLin
	l, m, s = math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)
	return [3]float64{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func oklabDistance(a, b [3]float64) float64 {
	d0, d1, d2 := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return math.Sqrt(d0*d0 + d1*d1 + d2*d2)
}

func okToRGB(value [3]float64) (uint8, uint8, uint8) {
	l := value[0] + 0.3963377774*value[1] + 0.2158037573*value[2]
	m := value[0] - 0.1055613458*value[1] - 0.0638541728*value[2]
	s := value[0] - 0.0894841775*value[1] - 1.2914855480*value[2]
	l, m, s = l*l*l, m*m*m, s*s*s
	rLin := 4.061205891158455*l - 3.266997275914592*m + 0.2057913823715241*s
	gLin := -1.245479778614608*l + 2.549591190626481*m - 0.3041114087157996*s
	bLin := -0.1190556689640171*l - 0.4024081653933972*m + 1.521463819102535*s
	gamma := func(value float64) float64 {
		if value > 0.0031308 {
			return 1.055*math.Pow(value, 1/2.4) - 0.055
		}
		return 12.92 * value
	}
	return uint8(clampUnit(gamma(rLin))), uint8(clampUnit(gamma(gLin))), uint8(clampUnit(gamma(bLin)))
}

func clampUnit(value float64) float64 {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return math.Round(value * 255)
}
