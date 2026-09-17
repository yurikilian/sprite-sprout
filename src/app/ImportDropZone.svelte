<script lang="ts">
  import { onMount } from 'svelte';
  import { editorState } from '../lib/state.svelte';
  import {
    loadImageFromFile,
    loadImageFromClipboard,
    analyzeImage,
  } from '../lib/engine/io/import';
  import { detectGridSize } from '../lib/engine/grid/detect';
  import { calculateFitZoom } from '../lib/engine/canvas/renderer';
  import {
    analyzeWithWails,
    isWailsAvailable,
    onWailsFileError,
    onWailsFileOpen,
    openImageWithWails,
    toImageData,
    type WailsAnalysis,
    type WailsNativeImage,
  } from '../lib/wails';

  // ---- Local reactive state ---------------------------------------------------

  let isDragOver = $state(false);
  let isLoading = $state(false);
  let errorMessage: string | null = $state(null);

  /** Hidden file input element */
  let fileInput: HTMLInputElement | undefined = $state();

  onMount(() => {
    const stopOpen = onWailsFileOpen((nativeImage) => {
      void applyNativeImage(nativeImage);
    });
    const stopError = onWailsFileError((message) => {
      isLoading = false;
      errorMessage = message;
    });
    return () => {
      stopOpen();
      stopError();
    };
  });

  async function applyImage(imageData: ImageData, nativeAnalysis?: WailsAnalysis): Promise<void> {
    // Keep the source immutable and give drawing tools their own working copy.
    editorState.sourceImage = imageData;
    editorState.canvas = {
      width: imageData.width,
      height: imageData.height,
      data: new Uint8ClampedArray(imageData.data),
    };

    let analysis: WailsAnalysis | null = nativeAnalysis ?? null;
    if (!analysis && isWailsAvailable()) {
      try {
        analysis = await analyzeWithWails(imageData.data, imageData.width, imageData.height);
      } catch {
        // The local analyzer keeps browser mode and early Wails startup usable.
      }
    }
    editorState.analysis = analysis ?? analyzeImage(imageData);
    editorState.detectedGridSize =
      analysis && 'detectedGrid' in analysis
        ? analysis.detectedGrid
        : detectGridSize(imageData.data, imageData.width, imageData.height).gridSize;
    editorState.bumpVersion();

    // Center the imported image in the viewport. The ResizeObserver may not
    // have delivered its first measurement while the drop zone is still
    // mounted, so read the container as a fallback instead of leaving a
    // large sheet at the 1x zoom default.
    let vw = editorState.viewportW;
    let vh = editorState.viewportH;
    if (vw <= 0 || vh <= 0) {
      const viewport = document.querySelector<HTMLElement>(
        '[role="application"][aria-label="Pixel art canvas"]',
      );
      if (viewport) {
        const rect = viewport.getBoundingClientRect();
        vw = rect.width;
        vh = rect.height;
        editorState.viewportW = vw;
        editorState.viewportH = vh;
      }
    }
    if (vw > 0 && vh > 0) {
      const z = calculateFitZoom(imageData.width, imageData.height, vw, vh);
      editorState.zoom = z;
      editorState.panX = (vw - imageData.width * z) / 2;
      editorState.panY = (vh - imageData.height * z) / 2;
    }
  }

  async function applyNativeImage(nativeImage: WailsNativeImage): Promise<void> {
    isLoading = true;
    errorMessage = null;
    try {
      await applyImage(toImageData(nativeImage.image), nativeImage.analysis);
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : 'Failed to load image';
    } finally {
      isLoading = false;
    }
  }

  // ---- Import handler ---------------------------------------------------------

  async function handleImport(file: File): Promise<void> {
    if (!file.type.startsWith('image/')) {
      errorMessage = 'Please drop an image file (PNG, JPG, GIF, etc.)';
      return;
    }

    isLoading = true;
    errorMessage = null;

    try {
      const imageData = await loadImageFromFile(file);

      await applyImage(imageData);
    } catch (err) {
      errorMessage =
        err instanceof Error ? err.message : 'Failed to load image';
    } finally {
      isLoading = false;
    }
  }

  // ---- Drag-and-drop handlers -------------------------------------------------

  function handleDragOver(e: DragEvent): void {
    e.preventDefault();
    isDragOver = true;
  }

  function handleDragLeave(e: DragEvent): void {
    e.preventDefault();
    isDragOver = false;
  }

  async function handleDrop(e: DragEvent): Promise<void> {
    e.preventDefault();
    isDragOver = false;

    const file = e.dataTransfer?.files[0];
    if (file) {
      await handleImport(file);
    }
  }

  // ---- File picker ------------------------------------------------------------

  async function openFilePicker(): Promise<void> {
    if (isWailsAvailable()) {
      isLoading = true;
      errorMessage = null;
      try {
        // A cancelled native chooser returns null and should remain cancelled,
        // rather than opening a second browser picker behind the Wails window.
        const nativeImage = await openImageWithWails();
        if (nativeImage) await applyImage(nativeImage.image, nativeImage.analysis);
      } catch (err) {
        errorMessage = err instanceof Error ? err.message : 'Failed to open image';
      } finally {
        isLoading = false;
      }
      return;
    }
    fileInput?.click();
  }

  async function handleFileChange(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file) {
      await handleImport(file);
    }
    // Reset so the same file can be re-selected
    input.value = '';
  }

  // ---- Demo loader ------------------------------------------------------------

  const DEMOS = [
    { path: '/fishing-cat.png', label: 'Fishing Cat', type: 'image/png' },
    { path: '/tired-salaryman.png', label: 'Salaryman', type: 'image/png' },
    { path: '/demo-sprites.jpg', label: 'Sprite Sheet', type: 'image/jpeg' },
  ] as const;

  let loadingDemo: string | null = $state(null);

  async function loadDemo(e: MouseEvent, demo: typeof DEMOS[number]): Promise<void> {
    e.stopPropagation();
    loadingDemo = demo.path;
    errorMessage = null;
    try {
      const res = await fetch(demo.path);
      if (!res.ok) throw new Error('Could not load demo image');
      const blob = await res.blob();
      const filename = demo.path.split('/').pop()!;
      const file = new File([blob], filename, { type: demo.type });
      await handleImport(file);
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : 'Failed to load demo';
    } finally {
      loadingDemo = null;
    }
  }

  // ---- Clipboard paste --------------------------------------------------------

  async function handlePaste(e: ClipboardEvent): Promise<void> {
    if (!e.clipboardData) return;

    const imageData = await loadImageFromClipboard(e.clipboardData.items);
    if (imageData) {
      e.preventDefault();

      isLoading = true;
      errorMessage = null;

      try {
        await applyImage(imageData);
      } catch (err) {
        errorMessage =
          err instanceof Error ? err.message : 'Failed to load image';
      } finally {
        isLoading = false;
      }
    }
  }
</script>

<svelte:window onpaste={handlePaste} />

<div
  class="drop-zone"
  style="--wails-drop-target: drop"
  class:drag-over={isDragOver}
  class:loading={isLoading}
  role="button"
  tabindex="0"
  aria-label="Import image"
  ondragover={handleDragOver}
  ondragleave={handleDragLeave}
  ondrop={handleDrop}
  onclick={openFilePicker}
  onkeydown={(e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      void openFilePicker();
    }
  }}
>
  <input
    bind:this={fileInput}
    type="file"
    accept="image/*"
    class="file-input"
    onchange={handleFileChange}
  />

  <div class="drop-content">
    {#if isLoading}
      <p class="label">Loading...</p>
    {:else if isDragOver}
      <p class="label">Release to drop</p>
    {:else}
      <p class="label">Drop an image here</p>
      <p class="hint">or click to browse</p>
      <p class="hint">You can also paste from clipboard</p>
      <p class="demo-label">Or try one of our sample AI-generated sprite sheets</p>
      <div class="demo-row">
        {#each DEMOS as demo}
          <button
            class="demo-btn"
            onclick={(e: MouseEvent) => loadDemo(e, demo)}
            disabled={loadingDemo !== null}
          >
            {loadingDemo === demo.path ? 'Loading...' : demo.label}
          </button>
        {/each}
      </div>
    {/if}

    {#if errorMessage}
      <p class="error">{errorMessage}</p>
    {/if}
  </div>
</div>

<style>
  .drop-zone {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 2px dashed var(--border-color);
    border-radius: 8px;
    margin: 16px;
    cursor: pointer;
    transition:
      border-color 0.15s ease,
      background-color 0.15s ease;
  }

  .drop-zone:hover,
  .drop-zone:focus-visible {
    border-color: var(--accent);
    background-color: rgba(37, 99, 235, 0.05);
    outline: none;
  }

  .drop-zone.drag-over {
    border-color: var(--accent);
    background-color: rgba(37, 99, 235, 0.1);
    border-style: solid;
  }

  .drop-zone.loading {
    pointer-events: none;
    opacity: 0.7;
  }

  .file-input {
    display: none;
  }

  .drop-content {
    text-align: center;
    color: var(--text-secondary);
  }

  .label {
    font-size: 14px;
    margin-bottom: 4px;
  }

  .hint {
    font-size: 12px;
    opacity: 0.6;
  }

  .error {
    color: var(--danger);
    font-size: 12px;
    margin-top: 8px;
  }

  .demo-label {
    font-size: 12px;
    opacity: 0.6;
    margin-top: 20px;
    margin-bottom: 4px;
  }

  .demo-row {
    display: flex;
    gap: 8px;
    margin-top: 8px;
    justify-content: center;
  }

  .demo-btn {
    padding: 6px 12px;
    font-size: 12px;
    background: var(--accent);
    color: var(--accent-ink);
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-weight: 500;
    transition: background-color 0.15s ease;
  }

  .demo-btn:hover {
    background: var(--accent-hover);
    color: var(--accent-ink);
  }

  .demo-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
