import { describe, expect, it } from 'vitest';
import {
  buildRouteMissingTokenIndex,
  normalizeMissingTokenModels,
  type MissingTokenModelsByName,
} from './routeMissingTokenHints.js';

function matchesModelPattern(model: string, pattern: string): boolean {
  if (!pattern) return false;
  if (pattern === '*') return true;
  if (pattern.endsWith('*')) return model.startsWith(pattern.slice(0, -1));
  return model === pattern;
}

describe('buildRouteMissingTokenIndex', () => {
  it('matches missing-token models by route pattern and deduplicates accounts', () => {
    const routes = [
      { id: 1, modelPattern: 'claude-*' },
      { id: 2, modelPattern: 'gpt-4o-mini' },
      { id: 3, modelPattern: '' },
    ];

    const missingByModel: MissingTokenModelsByName = {
      'claude-opus-4-6': [
        { accountId: 11, username: 'alice', siteId: 1, siteName: 'site-a' },
        { accountId: 11, username: 'alice', siteId: 1, siteName: 'site-a' },
      ],
      'claude-code-4-6': [
        { accountId: 22, username: 'bob', siteId: 2, siteName: 'site-b' },
      ],
      'gpt-4o-mini': [
        { accountId: 33, username: 'charlie', siteId: 3, siteName: 'site-c' },
      ],
    };

    const index = buildRouteMissingTokenIndex(routes, missingByModel, matchesModelPattern);
    expect(index[1].map((item) => item.modelName)).toEqual(['claude-code-4-6', 'claude-opus-4-6']);
    expect(index[1][1].accounts).toEqual([{ accountId: 11, username: 'alice', siteId: 1, siteName: 'site-a' }]);
    expect(index[2].map((item) => item.modelName)).toEqual(['gpt-4o-mini']);
    expect(index[3]).toEqual([]);
  });

  it('returns empty entries when missing model map is empty', () => {
    const index = buildRouteMissingTokenIndex([{ id: 1, modelPattern: 'claude-*' }], {}, matchesModelPattern);
    expect(index[1]).toEqual([]);
  });

  it('normalizes missing-token map by trimming model name and deduplicating account', () => {
    const merged = normalizeMissingTokenModels({
      '  claude-opus-4-6  ': [
        { accountId: 1, username: 'alice', siteId: 11, siteName: 'site-a' },
        { accountId: 1, username: 'alice', siteId: 11, siteName: 'site-a' },
      ],
    });

    expect(merged['claude-opus-4-6']).toEqual([
      {
        accountId: 1,
        username: 'alice',
        siteId: 11,
        siteName: 'site-a',
      },
    ]);
  });
});
