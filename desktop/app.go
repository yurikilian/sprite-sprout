package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yurikilian/sprite-sprout/internal/sprite"
)

const (
	fileOpenEvent  = "sprite-sprout:file-open"
	fileErrorEvent = "sprite-sprout:file-error"
)

// PixelBuffer is the small, explicit wire format shared with the browser
// frontend. The pixel payload is base64 RGBA so Wails does not reinterpret
// Uint8Array values as a JSON array of numbers.
type PixelBuffer struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Data   string `json:"data"`
}

type NativeImage struct {
	Name     string          `json:"name"`
	Image    PixelBuffer     `json:"image"`
	Analysis sprite.Analysis `json:"analysis"`
}

type TransformResult struct {
	Image   PixelBuffer    `json:"image"`
	Palette []sprite.Color `json:"palette,omitempty"`
}

type ProcessResult struct {
	Image          PixelBuffer    `json:"image"`
	BaseImage      PixelBuffer    `json:"baseImage"`
	GridSize       int            `json:"gridSize"`
	OriginalWidth  int            `json:"originalWidth"`
	OriginalHeight int            `json:"originalHeight"`
	OriginalColors int            `json:"originalColors"`
	OutputColors   int            `json:"outputColors"`
	Method         string         `json:"method"`
	Scale          int            `json:"scale"`
	Palette        []sprite.Color `json:"palette,omitempty"`
}

// App owns the desktop-only bridge. It contains no editor state: the Svelte
// store remains authoritative for drawing, history, zoom and comparison.
type App struct {
	ctx context.Context
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.OnFileDrop(ctx, func(_ int, _ int, paths []string) {
		if len(paths) == 0 {
			return
		}
		a.emitFile(paths[0])
	})
}

func (a *App) shutdown(ctx context.Context) {
	runtime.OnFileDropOff(ctx)
	a.ctx = nil
}

// Version is intentionally tiny so the frontend can show which desktop
// bridge it is talking to while retaining browser mode when no bridge exists.
func (a *App) Version() string { return sprite.Version }

func (a *App) AnalyzeImage(input PixelBuffer) (sprite.Analysis, error) {
	im, err := decodeBuffer(input)
	if err != nil {
		return sprite.Analysis{}, err
	}
	return sprite.Analyze(im)
}

func (a *App) SnapToGrid(input PixelBuffer, grid int) (TransformResult, error) {
	im, err := decodeBuffer(input)
	if err != nil {
		return TransformResult{}, err
	}
	out, err := sprite.SnapToGrid(im, grid)
	if err != nil {
		return TransformResult{}, err
	}
	return TransformResult{Image: encodeBuffer(out)}, nil
}

func (a *App) QuantizeImage(input PixelBuffer, colors int, method string) (TransformResult, error) {
	im, err := decodeBuffer(input)
	if err != nil {
		return TransformResult{}, err
	}
	out, palette, err := sprite.Quantize(im, colors, method)
	if err != nil {
		return TransformResult{}, err
	}
	return TransformResult{Image: encodeBuffer(out), Palette: palette}, nil
}

// ScaleImage applies nearest-neighbour enlargement before the native save
// dialog. Keeping this operation in the shared Go core makes scaled exports
// byte-for-byte identical between the CLI and desktop app.
func (a *App) ScaleImage(input PixelBuffer, scale int) (TransformResult, error) {
	im, err := decodeBuffer(input)
	if err != nil {
		return TransformResult{}, err
	}
	out, err := sprite.ScaleNearest(im, scale)
	if err != nil {
		return TransformResult{}, err
	}
	return TransformResult{Image: encodeBuffer(out)}, nil
}

func (a *App) AutoCleanImage(input PixelBuffer) (ProcessResult, error) {
	im, err := decodeBuffer(input)
	if err != nil {
		return ProcessResult{}, err
	}
	result, err := sprite.AutoClean(im)
	if err != nil {
		return ProcessResult{}, err
	}
	return processResult(result), nil
}

func (a *App) ProcessImage(input PixelBuffer, recipe sprite.Recipe) (ProcessResult, error) {
	im, err := decodeBuffer(input)
	if err != nil {
		return ProcessResult{}, err
	}
	result, err := sprite.Process(im, recipe)
	if err != nil {
		return ProcessResult{}, err
	}
	return processResult(result), nil
}

// OpenImage uses the native chooser and returns decoded RGBA data, so the
// browser frontend does not need filesystem access in the desktop build.
func (a *App) OpenImage() (*NativeImage, error) {
	if a.ctx == nil {
		return nil, errors.New("desktop runtime is not ready")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open sprite image",
		Filters: []runtime.FileFilter{{
			DisplayName: "Images (PNG, JPEG, WebP, GIF)",
			Pattern:     "*.png;*.jpg;*.jpeg;*.webp;*.gif",
		}},
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	return a.loadImage(path)
}

// OpenImageAtPath is used by the Wails native file-drop event. The path is
// supplied by Wails itself and is never accepted by the CLI or persisted.
func (a *App) OpenImageAtPath(path string) (*NativeImage, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("image path is empty")
	}
	return a.loadImage(path)
}

// SavePNG opens the native save chooser and writes the already-scaled image
// atomically. Returning false means the user cancelled the chooser.
func (a *App) SavePNG(input PixelBuffer, suggestedName string) (bool, error) {
	if a.ctx == nil {
		return false, errors.New("desktop runtime is not ready")
	}
	im, err := decodeBuffer(input)
	if err != nil {
		return false, err
	}
	name := filepath.Base(strings.TrimSpace(suggestedName))
	if name == "." || name == "" {
		name = "sprite-sprout.png"
	}
	if strings.ToLower(filepath.Ext(name)) != ".png" {
		name += ".png"
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "Save sprite PNG",
		DefaultFilename:      name,
		CanCreateDirectories: true,
		Filters: []runtime.FileFilter{{
			DisplayName: "PNG image",
			Pattern:     "*.png",
		}},
	})
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}
	if strings.ToLower(filepath.Ext(path)) != ".png" {
		path += ".png"
	}
	if err := sprite.WritePNGAtomic(path, im); err != nil {
		return false, err
	}
	return true, nil
}

// OpenRecipe and SaveRecipe use the same native chooser as image export while
// leaving recipe validation to the shared JSON schema in the frontend/core.
func (a *App) OpenRecipe() (*string, error) {
	if a.ctx == nil {
		return nil, errors.New("desktop runtime is not ready")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Open Sprite Sprout recipe",
		Filters: []runtime.FileFilter{{DisplayName: "Sprite Sprout recipe", Pattern: "*.json"}},
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	return &text, nil
}

func (a *App) SaveRecipe(data, suggestedName string) (bool, error) {
	if a.ctx == nil {
		return false, errors.New("desktop runtime is not ready")
	}
	name := filepath.Base(strings.TrimSpace(suggestedName))
	if name == "." || name == "" {
		name = "sprite-sprout-recipe.json"
	}
	if strings.ToLower(filepath.Ext(name)) != ".json" {
		name += ".json"
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "Save Sprite Sprout recipe",
		DefaultFilename:      name,
		CanCreateDirectories: true,
		Filters:              []runtime.FileFilter{{DisplayName: "Sprite Sprout recipe", Pattern: "*.json"}},
	})
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}
	if strings.ToLower(filepath.Ext(path)) != ".json" {
		path += ".json"
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sprout-recipe-*.json")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) loadImage(path string) (*NativeImage, error) {
	im, err := sprite.DecodeFile(path)
	if err != nil {
		return nil, err
	}
	analysis, err := sprite.Analyze(im)
	if err != nil {
		return nil, err
	}
	return &NativeImage{
		Name:     filepath.Base(path),
		Image:    encodeBuffer(im),
		Analysis: analysis,
	}, nil
}

func (a *App) emitFile(path string) {
	if a.ctx == nil {
		return
	}
	image, err := a.loadImage(path)
	if err != nil {
		runtime.EventsEmit(a.ctx, fileErrorEvent, err.Error())
		return
	}
	runtime.EventsEmit(a.ctx, fileOpenEvent, image)
}

func decodeBuffer(input PixelBuffer) (sprite.Image, error) {
	if input.Width <= 0 || input.Height <= 0 || input.Width > sprite.MaxDimension || input.Height > sprite.MaxDimension {
		return sprite.Image{}, fmt.Errorf("invalid image dimensions %dx%d", input.Width, input.Height)
	}
	if int64(input.Width)*int64(input.Height) > sprite.MaxPixels {
		return sprite.Image{}, errors.New("image exceeds the pixel limit")
	}
	pix, err := base64.StdEncoding.DecodeString(input.Data)
	if err != nil {
		return sprite.Image{}, fmt.Errorf("decode pixel buffer: %w", err)
	}
	im := sprite.Image{Width: input.Width, Height: input.Height, Pix: pix}
	if !im.Valid() {
		return sprite.Image{}, errors.New("pixel buffer length does not match image dimensions")
	}
	return im, nil
}

func encodeBuffer(im sprite.Image) PixelBuffer {
	return PixelBuffer{
		Width:  im.Width,
		Height: im.Height,
		Data:   base64.StdEncoding.EncodeToString(im.Pix),
	}
}

func processResult(result sprite.ProcessResult) ProcessResult {
	return ProcessResult{
		Image:          encodeBuffer(result.Image),
		BaseImage:      encodeBuffer(result.BaseImage),
		GridSize:       result.GridSize,
		OriginalWidth:  result.OriginalWidth,
		OriginalHeight: result.OriginalHeight,
		OriginalColors: result.OriginalColors,
		OutputColors:   result.OutputColors,
		Method:         result.Method,
		Scale:          result.Scale,
		Palette:        result.Palette,
	}
}
