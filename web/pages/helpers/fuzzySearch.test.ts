import { describe, expect, it } from 'vitest';
import { fuzzyMatch } from './fuzzySearch.js';

describe('fuzzyMatch', () => {
  it('matches plain substring case-insensitively', () => {
    expect(fuzzyMatch('claude-opus-4-6', 'OPUS')).toBe(true);
    expect(fuzzyMatch('gpt-4o-mini', 'gpt')).toBe(true);
  });

  it('matches after removing separators', () => {
    expect(fuzzyMatch('gpt-5.2-codex', 'gpt52')).toBe(true);
    expect(fuzzyMatch('re:^claude-(opus|sonnet)-4-5$', 'claude45')).toBe(true);
  });

  it('supports subsequence fuzzy matching', () => {
    expect(fuzzyMatch('cerebras-llama-3.1-8b', 'cll318')).toBe(true);
  });

  it('returns false when query cannot be matched', () => {
    expect(fuzzyMatch('claude-opus-4-6', 'gemini')).toBe(false);
  });
});
