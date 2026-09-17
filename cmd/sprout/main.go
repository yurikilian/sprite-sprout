// Command sprout is the headless Sprite Sprout processor.
//
// It deliberately has no dependency on Wails, a browser, or the Svelte
// application. The same recipe can therefore be used from CI and from the
// desktop editor.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/yurikilian/sprite-sprout/internal/sprite"
)

const (
	exitOK        = 0
	exitProcess   = 1
	exitUsage     = 2
	exitCancelled = 130

	// Keep descriptive aliases for callers embedding the command in tests.
	exitFailure  = exitUsage
	exitArgument = exitUsage
)

var errCancelled = errors.New("operation cancelled")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run is a convenience entry point for tests and embedders. Production uses
// runContext so SIGINT/SIGTERM can cancel an in-flight batch.
func run(args []string) int {
	return runContext(context.Background(), args, os.Stdout, os.Stderr)
}

// runContext is kept injectable so command behavior can be tested without
// starting a process or replacing the process-wide stdout/stderr.
func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return exitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		printUsage(stdout)
		return exitOK
	case "-v", "--version", "version":
		if args[0] == "version" && len(args) > 1 {
			fs := newFlagSet("version", stderr)
			jsonOutput := fs.Bool("json", false, "emit JSON")
			help := fs.Bool("help", false, "show help")
			helpShort := fs.Bool("h", false, "show help")
			if err := fs.Parse(normalizeArgs(args[1:], map[string]bool{"json": false})); err != nil {
				return exitUsage
			}
			if *help || *helpShort {
				printUsage(stdout)
				return exitOK
			}
			if fs.NArg() != 0 {
				fmt.Fprintln(stderr, "version does not accept positional arguments")
				return exitUsage
			}
			if *jsonOutput {
				_ = writeJSON(stdout, map[string]string{"name": "sprout", "version": sprite.Version})
			} else {
				fmt.Fprintf(stdout, "sprout %s\n", sprite.Version)
			}
			return exitOK
		}
		if len(args) > 1 {
			fmt.Fprintln(stderr, "--version does not accept arguments")
			return exitUsage
		}
		fmt.Fprintf(stdout, "sprout %s\n", sprite.Version)
		return exitOK
	}

	switch args[0] {
	case "inspect":
		return runInspect(ctx, args[1:], stdout, stderr)
	case "clean":
		return runClean(ctx, args[1:], stdout, stderr)
	case "batch":
		return runBatch(ctx, args[1:], stdout, stderr)
	case "recipe":
		return runRecipe(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "sprout - Sprite Sprout headless sprite processor")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  sprout inspect INPUT [--json]")
	fmt.Fprintln(w, "  sprout clean INPUT -o OUTPUT [options]")
	fmt.Fprintln(w, "  sprout batch INPUT_DIR --out-dir OUTPUT_DIR [options]")
	fmt.Fprintln(w, "  sprout recipe validate RECIPE.json [--json]")
	fmt.Fprintln(w, "  sprout version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Clean and batch options:")
	fmt.Fprintln(w, "  --recipe FILE          load a versioned recipe JSON")
	fmt.Fprintln(w, "  --grid auto|N          grid cell size (default: auto)")
	fmt.Fprintln(w, "  --colors N             target colors; 0 preserves source")
	fmt.Fprintln(w, "  --method NAME          octree, weighted-octree, median-cut,")
	fmt.Fprintln(w, "                         octree-refine, or oklab-refine")
	fmt.Fprintln(w, "  --scale N              nearest-neighbor output scale (1-64)")
	fmt.Fprintln(w, "  --json                 emit structured results on stdout")
	fmt.Fprintln(w, "  --overwrite            replace existing output files")
	fmt.Fprintln(w, "  --recursive            recurse into subdirectories (batch)")
	fmt.Fprintln(w, "  --jobs N               concurrent batch workers (batch)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  sprout inspect sprite.png --json")
	fmt.Fprintln(w, "  sprout clean sprite.png -o cleaned.png --grid auto --colors 16")
	fmt.Fprintln(w, "  sprout clean sprite.png -o cleaned.png --recipe recipe.json")
	fmt.Fprintln(w, "  sprout batch ./input --out-dir ./output --recursive --jobs 4")
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { printUsage(stderr) }
	return fs
}

// normalizeArgs lets the documented `sprout inspect image.png --json` form
// work with the standard flag package, which otherwise stops at the first
// positional argument. Known value-taking flags retain their following value;
// positional arguments are moved to the end.
func normalizeArgs(args []string, valueFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positionals = append(positionals, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		if valueFlags[name] && !strings.ContainsRune(a, '=') && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positionals...)
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func loadRecipe(path string) (sprite.Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sprite.Recipe{}, fmt.Errorf("read recipe %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "null" {
		return sprite.Recipe{}, errors.New("recipe must be a JSON object")
	}
	// Decode exactly one JSON value before handing it to the core parser. This
	// prevents an otherwise valid recipe followed by another value from being
	// silently accepted by a command invocation. The CLI accepts both the
	// portable numeric grid value (0 means auto) and the friendlier "auto"
	// spelling used by command line users.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var document recipeDocument
	if err := dec.Decode(&document); err != nil {
		return sprite.Recipe{}, fmt.Errorf("invalid recipe %s: %w", path, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return sprite.Recipe{}, fmt.Errorf("invalid recipe %s: multiple JSON values", path)
		}
		return sprite.Recipe{}, fmt.Errorf("invalid recipe %s: %w", path, err)
	}
	grid := 0
	if len(document.Grid) > 0 {
		var gridName string
		if err := json.Unmarshal(document.Grid, &gridName); err == nil {
			if !strings.EqualFold(strings.TrimSpace(gridName), "auto") {
				return sprite.Recipe{}, fmt.Errorf("invalid recipe %s: grid must be auto or an integer", path)
			}
		} else if err := json.Unmarshal(document.Grid, &grid); err != nil {
			return sprite.Recipe{}, fmt.Errorf("invalid recipe %s: grid must be auto or an integer: %w", path, err)
		}
	}
	recipe := sprite.Recipe{
		Version: document.Version,
		Grid:    grid,
		Colors:  document.Colors,
		Method:  document.Method,
		Scale:   document.Scale,
	}
	recipe, err = recipe.Normalize()
	if err != nil {
		return sprite.Recipe{}, fmt.Errorf("invalid recipe %s: %w", path, err)
	}
	return recipe, nil
}

type recipeDocument struct {
	Version int             `json:"version"`
	Grid    json.RawMessage `json:"grid"`
	Colors  int             `json:"colors"`
	Method  string          `json:"method"`
	Scale   int             `json:"scale"`
}

type recipeFlags struct {
	recipePath string
	gridText   string
	colors     int
	method     string
	scale      int
}

func addRecipeFlags(fs *flag.FlagSet, opts *recipeFlags) {
	fs.StringVar(&opts.recipePath, "recipe", "", "recipe JSON file")
	fs.StringVar(&opts.gridText, "grid", "", "grid size or auto")
	fs.IntVar(&opts.colors, "colors", 0, "target color count")
	fs.StringVar(&opts.method, "method", "", "quantization method")
	fs.IntVar(&opts.scale, "scale", 0, "nearest-neighbor scale")
}

func resolveRecipe(fs *flag.FlagSet, opts recipeFlags) (sprite.Recipe, error) {
	r := sprite.DefaultRecipe()
	if opts.recipePath != "" {
		var err error
		r, err = loadRecipe(opts.recipePath)
		if err != nil {
			return sprite.Recipe{}, err
		}
	}
	var err error
	if fsWasSet(fs, "grid") {
		r.Grid, err = parseGrid(opts.gridText)
		if err != nil {
			return sprite.Recipe{}, err
		}
	}
	if fsWasSet(fs, "colors") {
		r.Colors = opts.colors
		// The editor uses Median Cut for an explicit/manual color reduction;
		// automatic cleanup keeps the faster Octree + Refine default. Preserve
		// that distinction for the CLI when no recipe supplies a method.
		if opts.recipePath == "" && !fsWasSet(fs, "method") && opts.colors > 0 {
			r.Method = "median-cut"
		}
	}
	if fsWasSet(fs, "method") {
		if strings.TrimSpace(opts.method) == "" {
			return sprite.Recipe{}, errors.New("method must be one of the supported quantizers")
		}
		r.Method = opts.method
	}
	if fsWasSet(fs, "scale") {
		if opts.scale == 0 {
			return sprite.Recipe{}, errors.New("scale must be between 1 and 64")
		}
		r.Scale = opts.scale
	}
	return r.Normalize()
}

func fsWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func parseGrid(value string) (int, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "auto") {
		return 0, nil
	}
	grid, err := strconv.Atoi(value)
	if err != nil || grid < 1 || grid > 32 {
		return 0, fmt.Errorf("grid must be auto or an integer from 1 to 32, got %q", value)
	}
	return grid, nil
}

type cleanJSON struct {
	Input          string         `json:"input"`
	Output         string         `json:"output"`
	Recipe         sprite.Recipe  `json:"recipe"`
	GridSize       int            `json:"gridSize"`
	OriginalWidth  int            `json:"originalWidth"`
	OriginalHeight int            `json:"originalHeight"`
	OriginalColors int            `json:"originalColors"`
	OutputColors   int            `json:"outputColors"`
	Width          int            `json:"width"`
	Height         int            `json:"height"`
	Palette        []sprite.Color `json:"palette,omitempty"`
}

func makeCleanJSON(input, output string, recipe sprite.Recipe, result sprite.ProcessResult) cleanJSON {
	return cleanJSON{
		Input: input, Output: output, Recipe: recipe,
		GridSize: result.GridSize, OriginalWidth: result.OriginalWidth,
		OriginalHeight: result.OriginalHeight, OriginalColors: result.OriginalColors,
		OutputColors: result.OutputColors, Width: result.Image.Width, Height: result.Image.Height,
		Palette: result.Palette,
	}
}

func runInspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("inspect", stderr)
	jsonOutput := fs.Bool("json", false, "emit JSON")
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	args = normalizeArgs(args, map[string]bool{"json": false, "h": false, "help": false})
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *help || *helpShort {
		printUsage(stdout)
		return exitOK
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "inspect requires exactly one input image")
		return exitUsage
	}
	if err := ctx.Err(); err != nil {
		return reportCancelled(stderr, err)
	}
	input := fs.Arg(0)
	im, err := sprite.DecodeFile(input)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProcess
	}
	analysis, err := sprite.Analyze(im)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProcess
	}
	if *jsonOutput {
		if err := writeJSON(stdout, analysis); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProcess
		}
		return exitOK
	}
	fmt.Fprintf(stdout, "Image: %s\n", input)
	fmt.Fprintf(stdout, "Dimensions: %dx%d\n", analysis.Width, analysis.Height)
	fmt.Fprintf(stdout, "Unique colors: %d\n", analysis.UniqueColorCount)
	fmt.Fprintf(stdout, "Looks like pixel art: %t\n", analysis.LooksLikePixelArt)
	fmt.Fprintf(stdout, "Detected grid: %d (confidence %.2f)\n", analysis.DetectedGrid, analysis.Confidence)
	fmt.Fprintf(stdout, "Suggested grids: %s\n", joinInts(analysis.SuggestedGridSizes))
	return exitOK
}

func runClean(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("clean", stderr)
	var opts recipeFlags
	addRecipeFlags(fs, &opts)
	var output string
	fs.StringVar(&output, "o", "", "output PNG")
	fs.StringVar(&output, "output", "", "output PNG")
	overwrite := fs.Bool("overwrite", false, "replace existing output")
	jsonOutput := fs.Bool("json", false, "emit JSON")
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	args = normalizeArgs(args, map[string]bool{
		"recipe": true, "grid": true, "colors": true, "method": true, "scale": true,
		"o": true, "output": true, "overwrite": false, "json": false,
	})
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *help || *helpShort {
		printUsage(stdout)
		return exitOK
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "clean requires exactly one input image")
		return exitUsage
	}
	if output == "" {
		fmt.Fprintln(stderr, "clean requires -o OUTPUT or --output OUTPUT")
		return exitUsage
	}
	recipe, err := resolveRecipe(fs, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	input := fs.Arg(0)
	if err := ensureOutputPath(output, *overwrite); err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	if err := ctx.Err(); err != nil {
		return reportCancelled(stderr, err)
	}
	im, err := sprite.DecodeFile(input)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProcess
	}
	var result sprite.ProcessResult
	autoDefault := opts.recipePath == "" && !fsWasSet(fs, "grid") && !fsWasSet(fs, "colors") && !fsWasSet(fs, "method") && !fsWasSet(fs, "scale")
	if autoDefault {
		result, err = sprite.AutoClean(im)
	} else {
		result, err = sprite.ProcessContext(ctx, im, recipe)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return reportCancelled(stderr, err)
		}
		fmt.Fprintln(stderr, err)
		return exitProcess
	}
	if err := sprite.WritePNGAtomic(output, result.Image); err != nil {
		fmt.Fprintln(stderr, fmt.Errorf("write %s: %w", output, err))
		return exitProcess
	}
	report := makeCleanJSON(input, output, recipe, result)
	if *jsonOutput {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProcess
		}
	} else {
		fmt.Fprintf(stderr, "%s -> %s (%dx%d, grid %d, %d colors, scale %dx)\n", input, output, result.Image.Width, result.Image.Height, result.GridSize, result.OutputColors, result.Scale)
	}
	return exitOK
}

func ensureOutputPath(path string, overwrite bool) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check output %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("output %s is a directory", path)
	}
	if !overwrite {
		return fmt.Errorf("output %s already exists; pass --overwrite to replace it", path)
	}
	return nil
}

type batchFileResult struct {
	Input          string `json:"input"`
	Output         string `json:"output"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	GridSize       int    `json:"gridSize,omitempty"`
	OriginalColors int    `json:"originalColors,omitempty"`
	OutputColors   int    `json:"outputColors,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
}

type batchReport struct {
	InputDir  string            `json:"inputDir"`
	OutputDir string            `json:"outputDir"`
	Recipe    sprite.Recipe     `json:"recipe"`
	Jobs      int               `json:"jobs"`
	Recursive bool              `json:"recursive"`
	Overwrite bool              `json:"overwrite"`
	Total     int               `json:"total"`
	Succeeded int               `json:"succeeded"`
	Failed    int               `json:"failed"`
	Cancelled int               `json:"cancelled"`
	Files     []batchFileResult `json:"files"`
}

type batchTask struct {
	input  string
	output string
}

func runBatch(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("batch", stderr)
	var opts recipeFlags
	addRecipeFlags(fs, &opts)
	outDir := ""
	fs.StringVar(&outDir, "out-dir", "", "output directory")
	defaultJobs := runtime.GOMAXPROCS(0)
	if defaultJobs < 1 {
		defaultJobs = 1
	}
	jobs := fs.Int("jobs", defaultJobs, "number of workers")
	recursive := fs.Bool("recursive", false, "include subdirectories")
	overwrite := fs.Bool("overwrite", false, "replace existing output files")
	jsonOutput := fs.Bool("json", false, "emit JSON")
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	args = normalizeArgs(args, map[string]bool{
		"recipe": true, "grid": true, "colors": true, "method": true, "scale": true,
		"out-dir": true, "jobs": true, "recursive": false, "overwrite": false, "json": false,
	})
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *help || *helpShort {
		printUsage(stdout)
		return exitOK
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "batch requires exactly one input directory")
		return exitUsage
	}
	if outDir == "" {
		fmt.Fprintln(stderr, "batch requires --out-dir OUTPUT_DIR")
		return exitUsage
	}
	if *jobs < 1 {
		fmt.Fprintln(stderr, "--jobs must be at least 1")
		return exitUsage
	}
	recipe, err := resolveRecipe(fs, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	inputDir := fs.Arg(0)
	inputRoot, err := filepath.Abs(inputDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	outputRoot, err := filepath.Abs(outDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	inputInfo, err := os.Stat(inputRoot)
	if err != nil {
		fmt.Fprintln(stderr, fmt.Errorf("input directory: %w", err))
		return exitProcess
	}
	if !inputInfo.IsDir() {
		fmt.Fprintln(stderr, fmt.Errorf("input %s is not a directory", inputDir))
		return exitUsage
	}
	if inputRoot == outputRoot {
		fmt.Fprintln(stderr, "input and output directories must be different")
		return exitUsage
	}
	tasks, err := collectTasks(inputRoot, outputRoot, *recursive)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProcess
	}
	if err := validateBatchOutputs(tasks, *overwrite); err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	autoDefault := opts.recipePath == "" && !fsWasSet(fs, "grid") && !fsWasSet(fs, "colors") && !fsWasSet(fs, "method") && !fsWasSet(fs, "scale")
	report := processBatch(ctx, tasks, recipe, *jobs, autoDefault)
	report.InputDir = inputRoot
	report.OutputDir = outputRoot
	report.Recursive = *recursive
	report.Overwrite = *overwrite
	if *jsonOutput {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProcess
		}
	} else {
		for _, item := range report.Files {
			if item.Status == "ok" {
				fmt.Fprintf(stderr, "%s -> %s\n", item.Input, item.Output)
			} else {
				fmt.Fprintf(stderr, "%s: %s (%s)\n", item.Input, item.Error, item.Status)
			}
		}
		fmt.Fprintf(stderr, "processed %d files: %d succeeded, %d failed, %d cancelled\n", report.Total, report.Succeeded, report.Failed, report.Cancelled)
	}
	if report.Cancelled > 0 || ctx.Err() != nil {
		return exitCancelled
	}
	if report.Failed > 0 {
		return exitProcess
	}
	return exitOK
}

func collectTasks(inputRoot, outputRoot string, recursive bool) ([]batchTask, error) {
	tasks := make([]batchTask, 0)
	add := func(path string, info os.FileInfo) error {
		if !info.Mode().IsRegular() || !isImagePath(path) {
			return nil
		}
		rel, err := filepath.Rel(inputRoot, path)
		if err != nil {
			return err
		}
		ext := filepath.Ext(rel)
		output := filepath.Join(outputRoot, strings.TrimSuffix(rel, ext)+".png")
		tasks = append(tasks, batchTask{input: path, output: output})
		return nil
	}
	if recursive {
		err := filepath.Walk(inputRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path != inputRoot && pathWithin(path, outputRoot) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return add(path, info)
		})
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", inputRoot, err)
		}
	} else {
		entries, err := os.ReadDir(inputRoot)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", inputRoot, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(inputRoot, entry.Name())
			info, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf("stat %s: %w", path, err)
			}
			if err := add(path, info); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].input < tasks[j].input })
	return tasks, nil
}

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func pathWithin(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func validateBatchOutputs(tasks []batchTask, overwrite bool) error {
	seen := make(map[string]string, len(tasks))
	for _, task := range tasks {
		if previous, ok := seen[task.output]; ok {
			return fmt.Errorf("output collision: %s and %s both map to %s", previous, task.input, task.output)
		}
		seen[task.output] = task.input
		if err := ensureOutputPath(task.output, overwrite); err != nil {
			return err
		}
	}
	return nil
}

func processBatch(ctx context.Context, tasks []batchTask, recipe sprite.Recipe, jobs int, autoDefault bool) batchReport {
	report := batchReport{Recipe: recipe, Jobs: jobs, Total: len(tasks), Files: make([]batchFileResult, len(tasks))}
	if len(tasks) > 0 {
		report.InputDir = filepath.Dir(tasks[0].input)
		report.OutputDir = filepath.Dir(tasks[0].output)
	}
	for i, task := range tasks {
		report.Files[i] = batchFileResult{Input: task.input, Output: task.output, Status: "cancelled"}
	}
	if len(tasks) == 0 {
		return report
	}
	if jobs > len(tasks) {
		jobs = len(tasks)
	}
	taskCh := make(chan int)
	resultCh := make(chan struct {
		index int
		item  batchFileResult
	})
	var wg sync.WaitGroup
	for worker := 0; worker < jobs; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range taskCh {
				task := tasks[index]
				item := batchFileResult{Input: task.input, Output: task.output, Status: "failed"}
				if err := ctx.Err(); err != nil {
					item.Status = "cancelled"
					item.Error = errCancelled.Error()
					resultCh <- struct {
						index int
						item  batchFileResult
					}{index, item}
					continue
				}
				im, err := sprite.DecodeFile(task.input)
				if err == nil {
					var result sprite.ProcessResult
					if autoDefault {
						result, err = sprite.AutoClean(im)
					} else {
						result, err = sprite.ProcessContext(ctx, im, recipe)
					}
					if err == nil {
						if ctx.Err() != nil {
							err = ctx.Err()
						} else {
							err = sprite.WritePNGAtomic(task.output, result.Image)
						}
					}
					if err == nil {
						item.Status = "ok"
						item.GridSize = result.GridSize
						item.OriginalColors = result.OriginalColors
						item.OutputColors = result.OutputColors
						item.Width = result.Image.Width
						item.Height = result.Image.Height
					}
				}
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						item.Status = "cancelled"
					}
					item.Error = err.Error()
				}
				resultCh <- struct {
					index int
					item  batchFileResult
				}{index, item}
			}
		}()
	}
	go func() {
		defer close(taskCh)
		for index := range tasks {
			select {
			case taskCh <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	for result := range resultCh {
		report.Files[result.index] = result.item
	}
	for _, item := range report.Files {
		switch item.Status {
		case "ok":
			report.Succeeded++
		case "cancelled":
			report.Cancelled++
		default:
			report.Failed++
		}
	}
	return report
}

func reportCancelled(stderr io.Writer, err error) int {
	if err == nil {
		err = errCancelled
	}
	fmt.Fprintln(stderr, errCancelled)
	return exitCancelled
}

func runRecipe(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stdout, "Usage: sprout recipe validate RECIPE.json [--json]")
		return exitOK
	}
	if args[0] != "validate" {
		fmt.Fprintf(stderr, "unknown recipe command %q\n", args[0])
		return exitUsage
	}
	fs := newFlagSet("recipe validate", stderr)
	jsonOutput := fs.Bool("json", false, "emit JSON")
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	args = normalizeArgs(args[1:], map[string]bool{"json": false})
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *help || *helpShort {
		fmt.Fprintln(stdout, "Usage: sprout recipe validate RECIPE.json [--json]")
		return exitOK
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "recipe validate requires exactly one recipe file")
		return exitUsage
	}
	path := fs.Arg(0)
	recipe, err := loadRecipe(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	if *jsonOutput {
		if err := writeJSON(stdout, map[string]any{"valid": true, "recipe": recipe}); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProcess
		}
	} else {
		fmt.Fprintf(stdout, "valid recipe: %s\n", path)
	}
	return exitOK
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return "none"
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprint(value)
	}
	return strings.Join(parts, ", ")
}
