package sprite

import "fmt"

const (
	Version       = "0.5.0"
	RecipeVersion = 1
	DefaultMethod = "octree-refine"
	DefaultScale  = 1
)

// Image is an RGBA pixel buffer. Pix is always Width*Height*4 bytes.
type Image struct {
	Width  int
	Height int
	Pix    []byte
}

func (im Image) Valid() bool {
	return im.Width > 0 && im.Height > 0 && len(im.Pix) == im.Width*im.Height*4
}

// Recipe is deliberately small and portable: manual drawing is editor state,
// while a recipe describes repeatable import/cleanup/export operations.
type Recipe struct {
	Version int    `json:"version"`
	Grid    int    `json:"grid,omitempty"`   // 0 means auto-detect
	Colors  int    `json:"colors,omitempty"` // 0 means preserve source
	Method  string `json:"method,omitempty"` // quantizer name
	Scale   int    `json:"scale,omitempty"`  // nearest-neighbour output scale
}

func DefaultRecipe() Recipe {
	return Recipe{Version: RecipeVersion, Method: DefaultMethod, Scale: DefaultScale}
}

func (r Recipe) Normalize() (Recipe, error) {
	if r.Version == 0 {
		r.Version = RecipeVersion
	}
	if r.Version != RecipeVersion {
		return Recipe{}, fmt.Errorf("unsupported recipe version %d", r.Version)
	}
	if r.Grid < 0 || r.Grid > 32 {
		return Recipe{}, fmt.Errorf("grid must be zero (auto) or an integer from 1 to 32")
	}
	if r.Colors < 0 || r.Colors > 256 {
		return Recipe{}, fmt.Errorf("colors must be between 0 and 256")
	}
	if r.Scale == 0 {
		r.Scale = DefaultScale
	}
	if r.Scale < 1 || r.Scale > 64 {
		return Recipe{}, fmt.Errorf("scale must be between 1 and 64")
	}
	if r.Method == "" {
		r.Method = DefaultMethod
	}
	if !ValidMethod(r.Method) {
		return Recipe{}, fmt.Errorf("unknown quantization method %q", r.Method)
	}
	return r, nil
}

func ValidMethod(method string) bool {
	switch method {
	case "octree", "weighted-octree", "median-cut", "octree-refine", "oklab-refine":
		return true
	default:
		return false
	}
}

type Color struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
	A uint8 `json:"a"`
}

type Analysis struct {
	Width              int             `json:"width"`
	Height             int             `json:"height"`
	UniqueColorCount   int             `json:"uniqueColorCount"`
	SuggestedGridSizes []int           `json:"suggestedGridSizes"`
	LooksLikePixelArt  bool            `json:"looksLikePixelArt"`
	DetectedGrid       int             `json:"detectedGrid"`
	Confidence         float64         `json:"confidence"`
	Candidates         []GridCandidate `json:"candidates"`
}

type GridCandidate struct {
	Size  int     `json:"size"`
	Score float64 `json:"score"`
}

type ProcessResult struct {
	Image Image `json:"-"`
	// BaseImage is the grid-snapped image before optional colour reduction.
	// It is kept out of CLI JSON but lets the desktop editor reapply a palette
	// without quantizing an already-quantized result.
	BaseImage      Image   `json:"-"`
	GridSize       int     `json:"gridSize"`
	OriginalWidth  int     `json:"originalWidth"`
	OriginalHeight int     `json:"originalHeight"`
	OriginalColors int     `json:"originalColors"`
	OutputColors   int     `json:"outputColors"`
	Method         string  `json:"method"`
	Scale          int     `json:"scale"`
	Palette        []Color `json:"palette,omitempty"`
}
