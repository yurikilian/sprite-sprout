/**
 * Optional Wails bridge.
 *
 * The editor also runs as a normal Vite/Svelte site.  Do not import generated
 * Wails bindings here: those modules assume `window.go` exists while the
 * browser tests and the hosted editor do not provide it.  The small adapter
 * below discovers the generated runtime lazily and keeps the wire format
 * explicit for raw RGBA buffers.
 */

const BASE64_ALPHABET =
  'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

export const WAILS_FILE_OPEN_EVENT = 'sprite-sprout:file-open';
export const WAILS_FILE_ERROR_EVENT = 'sprite-sprout:file-error';

export interface WailsPixelBuffer {
  width: number;
  height: number;
  /** Base64 encoded RGBA bytes, exactly width * height * 4 bytes. */
  data: string;
}

export interface WailsColor {
  r: number;
  g: number;
  b: number;
  a: number;
}

export interface WailsGridCandidate {
  size: number;
  score: number;
}

export interface WailsAnalysis {
  width: number;
  height: number;
  uniqueColorCount: number;
  suggestedGridSizes: number[];
  looksLikePixelArt: boolean;
  detectedGrid: number;
  confidence: number;
  candidates: WailsGridCandidate[];
}

export interface WailsNativeImage {
  name: string;
  image: WailsPixelBuffer;
  analysis: WailsAnalysis;
}

export interface WailsTransformResult {
  image: WailsPixelBuffer;
  palette?: WailsColor[];
}

export interface WailsProcessResult {
  image: WailsPixelBuffer;
  baseImage?: WailsPixelBuffer;
  gridSize: number;
  originalWidth: number;
  originalHeight: number;
  originalColors: number;
  outputColors: number;
  method: string;
  scale: number;
  palette?: WailsColor[];
}

export interface WailsRecipe {
  version?: number;
  grid?: number;
  colors?: number;
  method?: string;
  scale?: number;
}

export interface WailsAppBindings {
  Version?: () => Promise<string>;
  AnalyzeImage?: (input: WailsPixelBuffer) => Promise<WailsAnalysis>;
  SnapToGrid?: (
    input: WailsPixelBuffer,
    grid: number,
  ) => Promise<WailsTransformResult>;
  QuantizeImage?: (
    input: WailsPixelBuffer,
    colors: number,
    method: string,
  ) => Promise<WailsTransformResult>;
  ScaleImage?: (input: WailsPixelBuffer, scale: number) => Promise<WailsTransformResult>;
  AutoCleanImage?: (input: WailsPixelBuffer) => Promise<WailsProcessResult>;
  ProcessImage?: (
    input: WailsPixelBuffer,
    recipe: WailsRecipe,
  ) => Promise<WailsProcessResult>;
  OpenImage?: () => Promise<WailsNativeImage | null>;
  OpenImageAtPath?: (path: string) => Promise<WailsNativeImage>;
  SavePNG?: (
    input: WailsPixelBuffer,
    suggestedName: string,
  ) => Promise<boolean>;
  OpenRecipe?: () => Promise<string | null>;
  SaveRecipe?: (data: string, suggestedName: string) => Promise<boolean>;
}

interface WailsRuntime {
  EventsOn?: (
    eventName: string,
    callback: (...data: unknown[]) => void,
  ) => () => void;
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: WailsAppBindings;
      };
    };
    runtime?: WailsRuntime;
  }
}

export class WailsUnavailableError extends Error {
  constructor(method?: string) {
    super(
      method
        ? `Wails method ${method} is unavailable in browser mode`
        : 'Wails desktop runtime is unavailable',
    );
    this.name = 'WailsUnavailableError';
  }
}

/** Return the live generated binding, or null when running in a browser. */
export function getWailsApp(): WailsAppBindings | null {
  if (typeof window === 'undefined') return null;
  return window.go?.main?.App ?? null;
}

export function isWailsAvailable(): boolean {
  return getWailsApp() !== null;
}

export function hasWailsMethod(
  method: keyof WailsAppBindings,
): boolean {
  const app = getWailsApp();
  return typeof app?.[method] === 'function';
}

/** Convert a clamped RGBA buffer to the JSON-safe bridge payload. */
export function toWailsPixelBuffer(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): WailsPixelBuffer {
  if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0) {
    throw new RangeError(`invalid image dimensions ${width}x${height}`);
  }
  if (data.length !== width * height * 4) {
    throw new RangeError('pixel buffer length does not match image dimensions');
  }
  return { width, height, data: encodeBase64(data) };
}

/** Decode a bridge payload and validate that its byte count is trustworthy. */
export function fromWailsPixelBuffer(input: WailsPixelBuffer): {
  data: Uint8ClampedArray;
  width: number;
  height: number;
} {
  if (
    !Number.isInteger(input.width) ||
    !Number.isInteger(input.height) ||
    input.width <= 0 ||
    input.height <= 0
  ) {
    throw new RangeError(`invalid image dimensions ${input.width}x${input.height}`);
  }
  const data = decodeBase64(input.data);
  if (data.length !== input.width * input.height * 4) {
    throw new RangeError('pixel buffer length does not match image dimensions');
  }
  return { data, width: input.width, height: input.height };
}

/** Convert bridge RGBA data into the ImageData shape used by the editor. */
export function toImageData(input: WailsPixelBuffer): ImageData {
  const decoded = fromWailsPixelBuffer(input);
  if (typeof ImageData === 'function') {
    return new ImageData(
      decoded.data as unknown as ImageDataArray,
      decoded.width,
      decoded.height,
    );
  }
  // This fallback keeps the adapter testable in Vitest's node environment.
  return {
    data: decoded.data,
    width: decoded.width,
    height: decoded.height,
    colorSpace: 'srgb',
  } as ImageData;
}

/** Invoke a method only when Wails is present; callers can fall back locally. */
export async function invokeWails<K extends keyof WailsAppBindings>(
  method: K,
  ...args: Parameters<NonNullable<WailsAppBindings[K]>>
): Promise<Awaited<ReturnType<NonNullable<WailsAppBindings[K]>>>> {
  const app = getWailsApp();
  const fn = app?.[method];
  if (typeof fn !== 'function') {
    throw new WailsUnavailableError(String(method));
  }
  return (fn as (...values: unknown[]) => Promise<unknown>)(...args) as Promise<
    Awaited<ReturnType<NonNullable<WailsAppBindings[K]>>>
  >;
}

export function analyzeWithWails(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): Promise<WailsAnalysis> {
  return invokeWails('AnalyzeImage', toWailsPixelBuffer(data, width, height));
}

export async function snapToGridWithWails(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  grid: number,
): Promise<{
  data: Uint8ClampedArray;
  width: number;
  height: number;
}> {
  const result = await invokeWails(
    'SnapToGrid',
    toWailsPixelBuffer(data, width, height),
    grid,
  );
  return fromWailsPixelBuffer(result.image);
}

export async function quantizeWithWails(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  colors: number,
  method: string,
): Promise<{
  data: Uint8ClampedArray;
  width: number;
  height: number;
  palette: WailsColor[];
}> {
  const result = await invokeWails(
    'QuantizeImage',
    toWailsPixelBuffer(data, width, height),
    colors,
    method,
  );
  const decoded = fromWailsPixelBuffer(result.image);
  return { ...decoded, palette: result.palette ?? [] };
}

export async function scaleWithWails(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  scale: number,
): Promise<{ data: Uint8ClampedArray; width: number; height: number }> {
  const result = await invokeWails(
    'ScaleImage',
    toWailsPixelBuffer(data, width, height),
    scale,
  );
  return fromWailsPixelBuffer(result.image);
}

export function autoCleanWithWails(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): Promise<WailsProcessResult> {
  return invokeWails(
    'AutoCleanImage',
    toWailsPixelBuffer(data, width, height),
  );
}

export function processWithWails(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  recipe: WailsRecipe,
): Promise<WailsProcessResult> {
  return invokeWails(
    'ProcessImage',
    toWailsPixelBuffer(data, width, height),
    recipe,
  );
}

export async function openImageWithWails(): Promise<{
  image: ImageData;
  name: string;
  analysis: WailsAnalysis;
} | null> {
  const result = await invokeWails('OpenImage');
  if (!result) return null;
  return {
    image: toImageData(result.image),
    name: result.name,
    analysis: result.analysis,
  };
}

export async function openImageAtPathWithWails(
  path: string,
): Promise<{ image: ImageData; name: string; analysis: WailsAnalysis }> {
  const result = await invokeWails('OpenImageAtPath', path);
  return {
    image: toImageData(result.image),
    name: result.name,
    analysis: result.analysis,
  };
}

export function savePNGWithWails(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  suggestedName: string,
): Promise<boolean> {
  return invokeWails(
    'SavePNG',
    toWailsPixelBuffer(data, width, height),
    suggestedName,
  );
}

export function openRecipeWithWails(): Promise<string | null> {
  return invokeWails('OpenRecipe');
}

export function saveRecipeWithWails(data: string, suggestedName: string): Promise<boolean> {
  return invokeWails('SaveRecipe', data, suggestedName);
}

/** Subscribe to native Wails drag/drop or Finder-open events. */
export function onWailsFileOpen(
  listener: (image: WailsNativeImage) => void,
): () => void {
  if (typeof window === 'undefined' || !window.runtime?.EventsOn) return () => {};
  return window.runtime.EventsOn(WAILS_FILE_OPEN_EVENT, (...data: unknown[]) => {
    if (data[0] && typeof data[0] === 'object') {
      listener(data[0] as WailsNativeImage);
    }
  });
}

export function onWailsFileError(listener: (message: string) => void): () => void {
  if (typeof window === 'undefined' || !window.runtime?.EventsOn) return () => {};
  return window.runtime.EventsOn(WAILS_FILE_ERROR_EVENT, (...data: unknown[]) => {
    if (typeof data[0] === 'string') listener(data[0]);
  });
}

function encodeBase64(bytes: ArrayLike<number>): string {
  let output = '';
  for (let index = 0; index < bytes.length; index += 3) {
    const a = bytes[index];
    const b = index + 1 < bytes.length ? bytes[index + 1] : 0;
    const c = index + 2 < bytes.length ? bytes[index + 2] : 0;
    const remaining = bytes.length - index;
    output += BASE64_ALPHABET[a >> 2];
    output += BASE64_ALPHABET[((a & 0x03) << 4) | (b >> 4)];
    output += remaining > 1 ? BASE64_ALPHABET[((b & 0x0f) << 2) | (c >> 6)] : '=';
    output += remaining > 2 ? BASE64_ALPHABET[c & 0x3f] : '=';
  }
  return output;
}

function decodeBase64(value: string): Uint8ClampedArray {
  if (typeof value !== 'string' || value.length % 4 !== 0) {
    throw new RangeError('invalid base64 pixel buffer');
  }
  const padding = value.endsWith('==') ? 2 : value.endsWith('=') ? 1 : 0;
  const output = new Uint8ClampedArray((value.length / 4) * 3 - padding);
  let outputIndex = 0;
  for (let index = 0; index < value.length; index += 4) {
    const a = BASE64_ALPHABET.indexOf(value[index]);
    const b = BASE64_ALPHABET.indexOf(value[index + 1]);
    const c = value[index + 2] === '=' ? 0 : BASE64_ALPHABET.indexOf(value[index + 2]);
    const d = value[index + 3] === '=' ? 0 : BASE64_ALPHABET.indexOf(value[index + 3]);
    if (a < 0 || b < 0 || c < 0 || d < 0) {
      throw new RangeError('invalid base64 pixel buffer');
    }
    output[outputIndex++] = (a << 2) | (b >> 4);
    if (value[index + 2] !== '=') output[outputIndex++] = ((b & 0x0f) << 4) | (c >> 2);
    if (value[index + 3] !== '=') output[outputIndex++] = ((c & 0x03) << 6) | d;
  }
  return output;
}
