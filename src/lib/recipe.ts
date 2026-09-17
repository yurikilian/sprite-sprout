import type { QuantizeMethod } from './engine/color/quantize-dispatch';

export const SPRITE_RECIPE_VERSION = 1;

export interface SpriteRecipe {
  version: typeof SPRITE_RECIPE_VERSION;
  /** 0 means automatic detection. */
  grid: number;
  /** 0 preserves the colours from the snapped image. */
  colors: number;
  method: QuantizeMethod;
  scale: number;
}

const methods: readonly QuantizeMethod[] = [
  'octree',
  'weighted-octree',
  'median-cut',
  'octree-refine',
  'oklab-refine',
];

export function makeRecipe(
  grid: number,
  colors: number,
  method: QuantizeMethod,
  scale = 1,
): SpriteRecipe {
  const recipe: SpriteRecipe = {
    version: SPRITE_RECIPE_VERSION,
    grid,
    colors,
    method,
    scale,
  };
  validateRecipe(recipe);
  return recipe;
}

export function validateRecipe(value: unknown): asserts value is SpriteRecipe {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('Recipe must be a JSON object');
  }
  const recipe = value as Record<string, unknown>;
  if (recipe.version !== SPRITE_RECIPE_VERSION) {
    throw new Error(`Unsupported recipe version ${String(recipe.version)}`);
  }
  if (!Number.isInteger(recipe.grid) || Number(recipe.grid) < 0 || Number(recipe.grid) > 32) {
    throw new Error('Recipe grid must be auto (0) or an integer from 1 to 32');
  }
  if (!Number.isInteger(recipe.colors) || Number(recipe.colors) < 0 || Number(recipe.colors) > 256) {
    throw new Error('Recipe colors must be an integer from 0 to 256');
  }
  if (typeof recipe.method !== 'string' || !methods.includes(recipe.method as QuantizeMethod)) {
    throw new Error('Recipe method is not supported');
  }
  if (!Number.isInteger(recipe.scale) || Number(recipe.scale) < 1 || Number(recipe.scale) > 64) {
    throw new Error('Recipe scale must be an integer from 1 to 64');
  }
}

export function parseRecipe(text: string): SpriteRecipe {
  const value: unknown = JSON.parse(text);
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('Recipe must be a JSON object');
  }
  const input = value as Record<string, unknown>;
  const allowed = new Set(['version', 'grid', 'colors', 'method', 'scale']);
  for (const key of Object.keys(input)) {
    if (!allowed.has(key)) throw new Error(`Unknown recipe field ${key}`);
  }
  const grid = input.grid === undefined || input.grid === 'auto' ? 0 : input.grid;
  const normalized: unknown = {
    version: input.version === undefined ? SPRITE_RECIPE_VERSION : input.version,
    grid,
    colors: input.colors === undefined ? 0 : input.colors,
    method: input.method === undefined ? 'octree-refine' : input.method,
    scale: input.scale === undefined ? 1 : input.scale,
  };
  validateRecipe(normalized);
  return normalized;
}

export function stringifyRecipe(recipe: SpriteRecipe): string {
  validateRecipe(recipe);
  return `${JSON.stringify(recipe, null, 2)}\n`;
}
