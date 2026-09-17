# Sprite Sprout

Sprite Sprout is a pixel-art cleanup editor. Drop an image, detect its logical
grid, reduce noisy colours, touch up pixels, and export a crisp PNG.

The editor keeps Svelte 5, Vite, and Canvas 2D for the interactive surface.
The image pipeline is shared with a Go library and a headless `sprout` CLI.
Go 1.27.1 is the module toolchain; the macOS desktop shell uses Wails 2.
The desktop bridge uses native open/save dialogs and keeps the same PNG
processing path as the CLI; drag-and-drop and the system image clipboard stay
available inside the WebView.

## CLI

```sh
go run ./cmd/sprout inspect public/fishing-cat.png
go run ./cmd/sprout clean public/fishing-cat.png -o /tmp/fishing-clean.png --grid auto --colors 16
go run ./cmd/sprout clean public/fishing-cat.png -o /tmp/fishing-clean.png --recipe recipe.json
go run ./cmd/sprout batch ./input --out-dir ./output --recipe recipe.json --recursive
```

The CLI accepts PNG, JPEG, WebP, and the first frame of GIF files. Recipes are
versioned JSON documents and contain only repeatable cleanup/export settings:

```json
{
  "version": 1,
  "grid": 0,
  "colors": 16,
  "method": "octree-refine",
  "scale": 2
}
```

`grid: 0` means auto-detect. Supported methods are `octree`,
`weighted-octree`, `median-cut`, `octree-refine`, and `oklab-refine`.

## Development

```sh
npm install
npm run check
npm test
go test ./...
```

Run the browser editor with `npm run dev`. Build the frontend with
`npm run build`. The Wails application lives in `desktop/`. For a universal
macOS bundle (Apple Silicon + Intel), run `bash desktop/build.sh`; the script
builds and embeds the Vite assets, then runs Wails 2.14.0. For an interactive
development window, install the Wails CLI with `go install
github.com/wailsapp/wails/v2/cmd/wails@v2.14.0` and use `wails dev` from
`desktop/`.

The Cleanup panel can export/import the same versioned JSON recipe accepted by
the CLI. Manual canvas edits and undo history stay local to the editor and are
never written to a recipe.

The CLI is deliberately independent from Wails and can be built headlessly:

```sh
CGO_ENABLED=0 go build -o sprout ./cmd/sprout
```
