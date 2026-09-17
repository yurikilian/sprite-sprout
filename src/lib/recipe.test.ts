import { describe, expect, it } from 'vitest';
import { parseRecipe, stringifyRecipe } from './recipe';

describe('versioned recipes', () => {
  it('accepts CLI-friendly auto and omitted defaults', () => {
    expect(parseRecipe('{"version":1,"grid":"auto","colors":16}')).toEqual({
      version: 1,
      grid: 0,
      colors: 16,
      method: 'octree-refine',
      scale: 1,
    });
  });

  it('round-trips a complete recipe', () => {
    const recipe = parseRecipe(
      '{"version":1,"grid":4,"colors":16,"method":"median-cut","scale":2}',
    );
    expect(parseRecipe(stringifyRecipe(recipe))).toEqual(recipe);
  });

  it('rejects unknown fields and unsupported values', () => {
    expect(() => parseRecipe('{"version":1,"foo":true}')).toThrow('Unknown recipe field');
    expect(() => parseRecipe('{"version":1,"method":"nope"}')).toThrow('Recipe method');
  });
});
