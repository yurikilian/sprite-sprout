<script lang="ts">
  import { editorState } from '../lib/state.svelte';
  import { autoClean, suggestColorCount } from '../lib/engine/cleanup/pipeline';
  import { snapToGrid } from '../lib/engine/grid/snap';
  import { extractTopColors } from '../lib/engine/color/palette';
  import { calculateFitZoom } from '../lib/engine/canvas/renderer';
  import type { CanvasState } from '../lib/types';
  import {
    quantize,
    QUANTIZE_METHODS,
    type QuantizeMethod,
  } from '../lib/engine/color/quantize-dispatch';
  import {
    autoCleanWithWails,
    fromWailsPixelBuffer,
    isWailsAvailable,
    openRecipeWithWails,
    quantizeWithWails,
    saveRecipeWithWails,
    snapToGridWithWails,
    type WailsColor,
  } from '../lib/wails';
  import type { Color } from '../lib/types';
  import {
    makeRecipe,
    parseRecipe,
    stringifyRecipe,
    type SpriteRecipe,
  } from '../lib/recipe';
  import ConfirmDialog from './ConfirmDialog.svelte';

  // ---------------------------------------------------------------------------
  // Local state
  // ---------------------------------------------------------------------------

  // 65 = "Original" sentinel — slider range is 2..65, where 65 means no
  // reduction applied. Once the user drags to <=64, the max locks to 64.
  const COLOR_ORIGINAL = 65;

  let gridSizeInput: number = $state(1);
  let gridWasAuto: boolean = $state(true);
  let colorCountInput: number = $state(COLOR_ORIGINAL);
  let colorMethod: QuantizeMethod = $state('median-cut');
  let colorsReduced: boolean = $state(false);
  let bannerDismissed: boolean = $state(false);
  let cleanupApplied: boolean = $state(false);

  // Tracks the last grid size that was actually applied to the canvas,
  // so we can restore the slider on cancel.
  let lastAppliedGrid: number = 1;

  // Snapshot of canvas before color reduction — quantize from this to avoid
  // re-quantizing already-quantized data when the user adjusts color settings.
  let preColorSnapshot: CanvasState | null = $state(null);

  // Confirmation dialog state — shown before destructive operations when
  // the user has manual pixel edits that would be lost.
  let confirmAction: (() => void) | null = $state(null);
  let confirmCancel: (() => void) | null = $state(null);
  let confirmMessage: string = $state('');

  // Debounce timers (plain variables — NOT $state, so they don't become
  // spurious dependencies of the sourceImage-reset $effect below)
  let gridDebounceTimer: ReturnType<typeof setTimeout> | undefined = undefined;
  let colorDebounceTimer: ReturnType<typeof setTimeout> | undefined = undefined;
  // Every asynchronous native operation captures this generation and the
  // canvas version it started from. A result from an older image or edit is
  // discarded instead of replacing newer user work.
  let operationGeneration = 0;
  let recipeFileInput: HTMLInputElement | undefined = $state();
  let recipeNotice: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Derived values
  // ---------------------------------------------------------------------------

  let hasCanvas: boolean = $derived(editorState.canvas !== null);

  let hasAnalysis: boolean = $derived(editorState.analysis !== null);

  let showBanner: boolean = $derived(
    hasAnalysis && !bannerDismissed && !cleanupApplied,
  );

  let detectedGrid: number = $derived(editorState.detectedGridSize ?? 4);

  let analysisColorCount: number = $derived(
    editorState.analysis?.uniqueColorCount ?? 0,
  );

  let suggestedGridSizes: number[] = $derived(
    editorState.analysis?.suggestedGridSizes ?? [],
  );

  let topCandidates: number[] = $derived(suggestedGridSizes.slice(0, 3));

  // Once colors have been reduced, lock the slider max to 64
  let colorSliderMax: number = $derived(colorsReduced ? 64 : COLOR_ORIGINAL);

  let suggestedColorCount: number = $derived(
    editorState.analysis
      ? suggestColorCount(editorState.analysis.uniqueColorCount)
      : 16,
  );

  // Reset local cleanup state when a new image is loaded (or cleared)
  $effect(() => {
    editorState.sourceImage; // subscribe
    operationGeneration += 1;
    gridSizeInput = 1;
    gridWasAuto = true;
    colorCountInput = COLOR_ORIGINAL;
    colorsReduced = false;
    bannerDismissed = false;
    cleanupApplied = false;
    lastAppliedGrid = 1;
    preColorSnapshot = null;
    clearTimeout(gridDebounceTimer);
    clearTimeout(colorDebounceTimer);
  });

  /**
   * Fit and center the canvas in the viewport after a resize.
   * Falls back to reading the DOM if editorState viewport values aren't set.
   */
  function fitAndCenter(canvasW: number, canvasH: number): void {
    let vw = editorState.viewportW;
    let vh = editorState.viewportH;

    // Fallback: read from the canvas container element directly
    if (vw <= 0 || vh <= 0) {
      const el = document.querySelector('[role="application"][aria-label="Pixel art canvas"]');
      if (el) {
        const rect = el.getBoundingClientRect();
        vw = rect.width;
        vh = rect.height;
        editorState.viewportW = vw;
        editorState.viewportH = vh;
      }
    }

    if (vw <= 0 || vh <= 0 || canvasW <= 0 || canvasH <= 0) return;

    const newZoom = calculateFitZoom(canvasW, canvasH, vw, vh);
    editorState.zoom = newZoom;
    editorState.panX = (vw - canvasW * newZoom) / 2;
    editorState.panY = (vh - canvasH * newZoom) / 2;
  }

  // ---------------------------------------------------------------------------
  // Grid auto-apply (debounced)
  // ---------------------------------------------------------------------------

  function scheduleGridApply(): void {
    clearTimeout(gridDebounceTimer);
    gridDebounceTimer = setTimeout(() => {
      void autoApplyGrid();
    }, 200);
  }

  async function autoApplyGrid(): Promise<void> {
    if (editorState.hasManualEdits) {
      const prevGrid = lastAppliedGrid;
      const prevAuto = gridWasAuto;
      confirmMessage =
        'Changing the grid size rebuilds the canvas from the original image. ' +
        'Your manual pixel edits will be lost (you can still undo).';
      confirmAction = () => { void doApplyGrid(); };
      confirmCancel = () => {
        gridSizeInput = prevGrid;
        gridWasAuto = prevAuto;
      };
      return;
    }
    await doApplyGrid();
  }

  function paletteFromWails(palette: WailsColor[] | undefined): Color[] {
    return (palette ?? []).map((color) => [color.r, color.g, color.b, color.a]);
  }

  function operationIsCurrent(generation: number, canvasVersion: number): boolean {
    return generation === operationGeneration && editorState.canvasVersion === canvasVersion;
  }

  async function doApplyGrid(): Promise<void> {
    const source = editorState.sourceImage;
    if (!source) return;

    const sourceData = new Uint8ClampedArray(source.data);
    const requestedVersion = editorState.canvasVersion;
    const generation = ++operationGeneration;
    let result: { data: Uint8ClampedArray; width: number; height: number } | null = null;

    if (isWailsAvailable()) {
      try {
        result = await snapToGridWithWails(
          sourceData,
          source.width,
          source.height,
          gridSizeInput,
        );
      } catch {
        // Keep browser fallback available if a desktop binding is unavailable
        // during startup or a native call fails.
      }
    }
    if (!result) {
      result = snapToGrid(sourceData, source.width, source.height, gridSizeInput);
    }
    if (!operationIsCurrent(generation, requestedVersion)) return;

    let finalData = result.data;
    let finalWidth = result.width;
    let finalHeight = result.height;
    let palette: Color[];
    const cleanBase: CanvasState = {
      width: result.width,
      height: result.height,
      data: new Uint8ClampedArray(result.data),
    };

    // Re-apply color reduction if it was previously active
    if (colorsReduced) {
      let quantized: { data: Uint8ClampedArray; width: number; height: number; palette: Color[] } | null = null;
      if (isWailsAvailable()) {
        try {
          const native = await quantizeWithWails(
            cleanBase.data,
            cleanBase.width,
            cleanBase.height,
            colorCountInput,
            colorMethod,
          );
          quantized = { ...native, palette: paletteFromWails(native.palette) };
        } catch {
          // Browser implementation below remains the compatibility path.
        }
      }
      if (!quantized) {
        const local = quantize(
          cleanBase.data,
          cleanBase.width,
          cleanBase.height,
          colorCountInput,
          colorMethod,
        );
        quantized = {
          data: local.remappedData,
          width: cleanBase.width,
          height: cleanBase.height,
          palette: local.palette,
        };
      }
      if (!operationIsCurrent(generation, requestedVersion)) return;
      finalData = quantized.data;
      finalWidth = quantized.width;
      finalHeight = quantized.height;
      palette = quantized.palette;
    } else {
      palette = extractTopColors(result.data, 64);
    }

    editorState.pushHistory('Grid snap');
    editorState.canvas = { width: finalWidth, height: finalHeight, data: finalData };
    preColorSnapshot = cleanBase;
    editorState.palette = palette;
    editorState.bumpVersion();
    fitAndCenter(finalWidth, finalHeight);
    cleanupApplied = true;
    lastAppliedGrid = gridSizeInput;
    gridWasAuto = false;
    editorState.hasManualEdits = false;
  }

  // ---------------------------------------------------------------------------
  // Color auto-apply (debounced)
  // ---------------------------------------------------------------------------

  function scheduleColorApply(): void {
    clearTimeout(colorDebounceTimer);
    colorDebounceTimer = setTimeout(() => {
      void autoApplyColors();
    }, 200);
  }

  async function autoApplyColors(): Promise<void> {
    const canvas = editorState.canvas;
    if (!canvas) return;
    const requestedVersion = editorState.canvasVersion;
    const generation = ++operationGeneration;

    // Save pre-color snapshot on first color reduction
    if (!colorsReduced) {
      preColorSnapshot = {
        width: canvas.width,
        height: canvas.height,
        data: new Uint8ClampedArray(canvas.data),
      };
    }

    // Always quantize from the clean base, not already-quantized data
    const base = preColorSnapshot ?? canvas;
    let remappedData: Uint8ClampedArray;
    let palette: Color[];
    let outputWidth = base.width;
    let outputHeight = base.height;
    if (isWailsAvailable()) {
      try {
        const native = await quantizeWithWails(
          base.data,
          base.width,
          base.height,
          colorCountInput,
          colorMethod,
        );
        remappedData = native.data;
        outputWidth = native.width;
        outputHeight = native.height;
        palette = paletteFromWails(native.palette);
      } catch {
        const local = quantize(base.data, base.width, base.height, colorCountInput, colorMethod);
        remappedData = local.remappedData;
        palette = local.palette;
      }
    } else {
      const local = quantize(base.data, base.width, base.height, colorCountInput, colorMethod);
      remappedData = local.remappedData;
      palette = local.palette;
    }
    if (!operationIsCurrent(generation, requestedVersion)) return;

    editorState.pushHistory('Reduce colors');
    editorState.canvas = {
      width: outputWidth,
      height: outputHeight,
      data: remappedData,
    };
    editorState.palette = palette;
    editorState.bumpVersion();
    colorsReduced = true;
    cleanupApplied = true;
    gridWasAuto = false;
  }

  // ---------------------------------------------------------------------------
  // Auto-clean
  // ---------------------------------------------------------------------------

  function handleAutoClean(): void {
    if (editorState.hasManualEdits) {
      confirmMessage =
        'Auto-Clean rebuilds the canvas from the original image. ' +
        'Your manual pixel edits will be lost (you can still undo).';
      confirmAction = () => { void doAutoClean(); };
      return;
    }
    doAutoClean();
  }

  async function doAutoClean(): Promise<void> {
    const source = editorState.sourceImage;
    if (!source) return;

    const sourceData = new Uint8ClampedArray(source.data);
    const requestedVersion = editorState.canvasVersion;
    const generation = ++operationGeneration;

    if (isWailsAvailable()) {
      try {
        const native = await autoCleanWithWails(sourceData, source.width, source.height);
        if (!operationIsCurrent(generation, requestedVersion)) return;
        const nativeCanvas = {
          width: native.image.width,
          height: native.image.height,
          data: fromWailsPixelBuffer(native.image).data,
        };
        const nativeBase = native.baseImage
          ? {
              width: native.baseImage.width,
              height: native.baseImage.height,
              data: fromWailsPixelBuffer(native.baseImage).data,
            }
          : nativeCanvas;
        editorState.pushHistory('Auto-Clean');
        editorState.canvas = nativeCanvas;
        editorState.palette = paletteFromWails(native.palette);
        editorState.bumpVersion();
        fitAndCenter(nativeCanvas.width, nativeCanvas.height);
        gridSizeInput = native.gridSize;
        gridWasAuto = false;
        colorMethod = 'octree-refine';
        colorCountInput = native.outputColors || COLOR_ORIGINAL;
        colorsReduced = native.outputColors > 0 && native.outputColors < native.originalColors;
        cleanupApplied = true;
        bannerDismissed = true;
        lastAppliedGrid = gridSizeInput;
        preColorSnapshot = { ...nativeBase, data: new Uint8ClampedArray(nativeBase.data) };
        editorState.hasManualEdits = false;
        return;
      } catch {
        // Fall through to the local implementation when bindings are unavailable.
      }
    }
    const result = autoClean(sourceData, source.width, source.height);
    if (!operationIsCurrent(generation, requestedVersion)) return;

    // Sync UI controls to reflect what auto-clean chose
    gridSizeInput = result.gridSize;
    gridWasAuto = false;
    colorMethod = 'octree-refine';
    if (result.reduced) {
      colorCountInput = result.reducedColorCount;
    }

    editorState.pushHistory('Auto-Clean');

    // Apply grid snap result
    const finalData = result.reduced ? result.reduced : result.snapped;
    editorState.canvas = {
      width: finalData.width,
      height: finalData.height,
      data: finalData.data,
    };

    // Store pre-color snapshot (the grid-snapped, pre-quantized state)
    preColorSnapshot = {
      width: result.snapped.width,
      height: result.snapped.height,
      data: new Uint8ClampedArray(result.snapped.data),
    };

    // Update palette
    if (result.reduced) {
      editorState.palette = result.reduced.palette;
    } else {
      editorState.palette = extractTopColors(result.snapped.data, 64);
    }

    editorState.bumpVersion();
    fitAndCenter(finalData.width, finalData.height);
    colorsReduced = result.reduced !== null;
    cleanupApplied = true;
    bannerDismissed = true;
    lastAppliedGrid = gridSizeInput;
    editorState.hasManualEdits = false;
  }

  // ---------------------------------------------------------------------------
  // Input handlers
  // ---------------------------------------------------------------------------

  function handleGridInput(e: Event): void {
    const target = e.target as HTMLInputElement;
    const val = parseInt(target.value, 10);
    if (!isNaN(val) && val >= 1 && val <= 32) {
      gridSizeInput = val;
      gridWasAuto = false;
      scheduleGridApply();
    }
  }

  function selectCandidate(size: number): void {
    gridSizeInput = size;
    gridWasAuto = false;
    scheduleGridApply();
  }

  function handleColorInput(e: Event): void {
    const target = e.target as HTMLInputElement;
    const val = parseInt(target.value, 10);
    if (!isNaN(val) && val >= 2 && val <= 64) {
      colorCountInput = val;
      scheduleColorApply();
    }
    // Ignore values > 64 (the "Original" sentinel) — no auto-apply
  }

  function selectColorTarget(count: number): void {
    colorCountInput = count;
    scheduleColorApply();
  }

  function handleMethodChange(e: Event): void {
    colorMethod = (e.target as HTMLSelectElement).value as QuantizeMethod;
    // Only auto-apply if colors have already been reduced
    if (colorsReduced) {
      scheduleColorApply();
    }
  }

  function currentRecipe(): SpriteRecipe {
    return makeRecipe(
      gridWasAuto ? 0 : gridSizeInput,
      colorsReduced ? colorCountInput : 0,
      colorMethod,
      editorState.exportScale,
    );
  }

  async function exportRecipe(): Promise<void> {
    try {
      const content = stringifyRecipe(currentRecipe());
      if (isWailsAvailable()) {
        try {
          const saved = await saveRecipeWithWails(content, 'sprite-sprout-recipe.json');
          recipeNotice = saved ? 'Recipe exported' : 'Recipe export cancelled';
          return;
        } catch {
          // The browser download remains available if the native chooser fails.
        }
      }
      const blob = new Blob([content], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = 'sprite-sprout-recipe.json';
      anchor.click();
      setTimeout(() => URL.revokeObjectURL(url), 100);
      recipeNotice = 'Recipe exported';
    } catch (err) {
      recipeNotice = err instanceof Error ? err.message : 'Could not export recipe';
    }
  }

  async function applyRecipeText(text: string): Promise<void> {
    const recipe = parseRecipe(text);
    gridWasAuto = recipe.grid === 0;
    gridSizeInput = recipe.grid === 0 ? Math.max(1, detectedGrid) : recipe.grid;
    colorMethod = recipe.method;
    colorCountInput = recipe.colors > 0 ? recipe.colors : COLOR_ORIGINAL;
    colorsReduced = recipe.colors > 0;
    editorState.exportScale = recipe.scale;
    recipeNotice = 'Recipe imported';
    scheduleGridApply();
  }

  async function importRecipe(): Promise<void> {
    if (isWailsAvailable()) {
      try {
        const content = await openRecipeWithWails();
        if (content !== null) await applyRecipeText(content);
      } catch (err) {
        recipeNotice = err instanceof Error ? err.message : 'Invalid recipe';
      }
      return;
    }
    recipeFileInput?.click();
  }

  async function handleRecipeFile(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    try {
      await applyRecipeText(await file.text());
    } catch (err) {
      recipeNotice = err instanceof Error ? err.message : 'Invalid recipe';
    }
  }
</script>

<ConfirmDialog
  show={confirmAction !== null}
  title="Manual edits will be lost"
  message={confirmMessage}
  confirmLabel="Continue"
  cancelLabel="Cancel"
  onconfirm={() => {
    const action = confirmAction;
    confirmAction = null;
    confirmCancel = null;
    action?.();
  }}
  oncancel={() => {
    const restore = confirmCancel;
    confirmAction = null;
    confirmCancel = null;
    restore?.();
  }}
/>

<div class="panel-inner">
  <h3>Cleanup</h3>

  {#if !hasCanvas}
    <p class="hint">Import an image to see cleanup options</p>
  {:else}
    <!-- Smart banner -->
    {#if showBanner}
      <div class="banner">
        <p class="banner-text">
          Detected ~{detectedGrid}x{detectedGrid} grid, {analysisColorCount} colors
        </p>
        <div class="banner-actions">
          <button class="action-btn accent" onclick={handleAutoClean}>
            Auto-Clean
          </button>
          <button
            class="link-btn"
            onclick={() => (bannerDismissed = true)}
          >
            Dismiss
          </button>
        </div>
      </div>
      <div class="divider"></div>
    {/if}

    <!-- Grid section -->
    <div class="section">
      <span class="section-label">Grid Size</span>
      {#if editorState.detectedGridSize !== null}
        <span class="detected-label">detected: {editorState.detectedGridSize}px</span>
      {/if}

      <div class="input-row">
        <input
          type="range"
          min="1"
          max="32"
          bind:value={gridSizeInput}
          oninput={handleGridInput}
          class="slider"
        />
        {#if gridSizeInput <= 1}
          <span class="original-label">Original</span>
        {:else}
          <input
            type="number"
            min="1"
            max="32"
            bind:value={gridSizeInput}
            oninput={handleGridInput}
            class="num-input"
          />
        {/if}
      </div>

      {#if topCandidates.length > 0}
        <div class="chip-row">
          {#each topCandidates as size}
            <button
              class="chip"
              class:active={gridSizeInput === size}
              onclick={() => selectCandidate(size)}
            >
              {size}px
            </button>
          {/each}
        </div>
      {/if}

    </div>

    <div class="divider"></div>

    <!-- Palette section -->
    <div class="section">
      <span class="section-label">
        Colors: {analysisColorCount}
      </span>

      <select class="method-select" value={colorMethod} onchange={handleMethodChange}>
        {#each QUANTIZE_METHODS as m}
          <option value={m.value}>{m.label}</option>
        {/each}
      </select>

      <div class="input-row">
        <input
          type="range"
          min="2"
          max={colorSliderMax}
          bind:value={colorCountInput}
          oninput={handleColorInput}
          class="slider"
        />
        {#if colorCountInput >= COLOR_ORIGINAL}
          <span class="original-label">Original</span>
        {:else}
          <input
            type="number"
            min="2"
            max="64"
            bind:value={colorCountInput}
            oninput={handleColorInput}
            class="num-input"
          />
        {/if}
      </div>

      {#if !colorsReduced}
        <div class="chip-row">
          <button
            class="chip"
            onclick={() => selectColorTarget(suggestedColorCount)}
          >
            {suggestedColorCount} colors
          </button>
        </div>
      {/if}

    </div>

    <div class="divider"></div>

    <div class="section recipe-section">
      <span class="section-label">Recipe</span>
      <div class="button-row">
        <button class="action-btn" onclick={exportRecipe}>Export JSON</button>
        <button class="action-btn" onclick={() => void importRecipe()}>Import JSON</button>
      </div>
      <input
        bind:this={recipeFileInput}
        class="recipe-input"
        type="file"
        accept="application/json,.json"
        onchange={handleRecipeFile}
      />
      {#if recipeNotice}
        <span class="recipe-notice" role="status">{recipeNotice}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .panel-inner {
    padding: 12px;
  }

  h3 {
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-secondary);
    margin-bottom: 12px;
  }

  .hint {
    font-size: 11px;
    color: var(--text-secondary);
    opacity: 0.6;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-secondary);
    opacity: 0.7;
  }

  .detected-label {
    font-size: 10px;
    color: var(--accent);
    opacity: 0.8;
  }

  .input-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .slider {
    flex: 1;
    height: 4px;
    accent-color: var(--accent);
    cursor: pointer;
  }

  .num-input {
    width: 48px;
    padding: 4px 6px;
    font-size: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-primary);
    text-align: center;
  }

  .original-label {
    width: 48px;
    font-size: 10px;
    color: var(--text-secondary);
    text-align: center;
    opacity: 0.7;
  }

  .chip-row {
    display: flex;
    gap: 4px;
  }

  .chip {
    padding: 3px 8px;
    font-size: 11px;
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    color: var(--text-primary);
    cursor: pointer;
  }

  .chip:hover {
    background: var(--accent);
    color: var(--accent-ink);
  }

  .chip.active {
    background: var(--accent);
    color: var(--accent-ink);
    border-color: var(--accent);
  }

  .action-btn {
    padding: 6px 10px;
    font-size: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-primary);
    cursor: pointer;
  }

  .action-btn:hover {
    background: var(--accent);
    color: var(--accent-ink);
  }

  .action-btn.accent {
    background: var(--accent);
    color: var(--accent-ink);
    border-color: var(--accent);
  }

  .action-btn.accent:hover {
    opacity: 0.9;
  }

  .link-btn {
    background: none;
    border: none;
    color: var(--text-secondary);
    font-size: 11px;
    cursor: pointer;
    text-decoration: underline;
    padding: 0;
  }

  .link-btn:hover {
    color: var(--text-primary);
  }

  .banner {
    background: var(--bg-surface);
    border: 1px solid var(--accent);
    border-radius: 6px;
    padding: 10px;
    margin-bottom: 4px;
  }

  .banner-text {
    font-size: 12px;
    color: var(--text-primary);
    margin-bottom: 8px;
  }

  .banner-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .divider {
    height: 1px;
    background: var(--border-color);
    margin: 12px 0;
  }

  .method-select {
    width: 100%;
    padding: 4px 6px;
    font-size: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-primary);
    cursor: pointer;
  }

  .method-select:focus {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }

  .recipe-input {
    display: none;
  }

  .recipe-notice {
    color: var(--accent);
    font-size: 10px;
  }
</style>
