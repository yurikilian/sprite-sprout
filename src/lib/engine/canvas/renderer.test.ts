import { describe, expect, it } from 'vitest';
import {
  calculateFitZoom,
  nextZoomLevel,
  prevZoomLevel,
  ZOOM_LEVELS,
} from './renderer';

describe('canvas zoom levels', () => {
  it('fits the large Fishing Cat sample below 1x', () => {
    expect(calculateFitZoom(2816, 1536, 1010, 790)).toBe(0.25);
  });

  it('keeps a reduced sprite sheet inside the viewport', () => {
    expect(calculateFitZoom(1408, 768, 1010, 790)).toBe(0.5);
  });

  it('uses the largest discrete level that fits', () => {
    expect(calculateFitZoom(64, 64, 500, 500)).toBe(6);
    expect(calculateFitZoom(4096, 4096, 320, 320)).toBe(0.0625);
  });

  it('supports stepping through fractional levels', () => {
    expect(ZOOM_LEVELS[0]).toBe(0.0625);
    expect(prevZoomLevel(0.25)).toBe(0.125);
    expect(nextZoomLevel(0.25)).toBe(0.5);
  });
});
