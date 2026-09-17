package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/yurikilian/sprite-sprout/internal/sprite"
)

func writeFixture(t *testing.T, path string) {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x * 20), uint8(y * 20), 80, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
}

func TestCleanRecipeAndPrecedence(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sprite.png")
	output := filepath.Join(dir, "out.png")
	recipePath := filepath.Join(dir, "recipe.json")
	writeFixture(t, input)
	data, _ := json.Marshal(sprite.Recipe{Version: 1, Grid: 2, Colors: 4, Method: "median-cut", Scale: 2})
	if err := os.WriteFile(recipePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"clean", input, "-o", output, "--recipe", recipePath, "--colors", "2"}); code != exitOK {
		t.Fatalf("clean exit code %d", code)
	}
	result, err := sprite.DecodeFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 8 || result.Height != 8 {
		t.Fatalf("recipe output dimensions %dx%d", result.Width, result.Height)
	}
}

func TestBatchRejectsCollisionWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	output := filepath.Join(dir, "output")
	if err := os.MkdirAll(filepath.Join(input, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(input, "nested", "one.png"))
	if err := os.MkdirAll(filepath.Join(output, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(output, "nested", "one.png"))
	if code := run([]string{"batch", input, "--out-dir", output, "--recursive"}); code != exitUsage {
		t.Fatalf("collision exit code %d", code)
	}
}

func TestInvalidRecipeIsArgumentError(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sprite.png")
	writeFixture(t, input)
	recipe := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(recipe, []byte(`{"version":1,"method":"invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"clean", input, "-o", filepath.Join(dir, "out.png"), "--recipe", recipe}); code != exitArgument {
		t.Fatalf("invalid recipe exit code %d", code)
	}
}
