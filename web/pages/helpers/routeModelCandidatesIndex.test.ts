import { describe, expect, it } from 'vitest';
import {
  buildRouteModelCandidatesIndex,
  type IndexedRouteModelCandidate,
  type RouteModelCandidatesByModelName,
} from './routeModelCandidatesIndex.js';

function matchesModelPattern(model: string, pattern: string): boolean {
  if (!pattern) return false;
  if (pattern === '*') return true;
  if (pattern.endsWith('*')) return model.startsWith(pattern.slice(0, -1));
  return model === pattern;
}

describe('buildRouteModelCandidatesIndex', () => {
  it('precomputes route candidates, account options and token options by account', () => {
    const routes = [
      { id: 1, modelPattern: 'claude-*' },
      { id: 2, modelPattern: 'gpt-4o-mini' },
    ];

    const modelCandidates: RouteModelCandidatesByModelName = {
      'claude-opus-4-6': [
        {
          modelName: 'will-be-overwritten',
          accountId: 11,
          tokenId: 101,
          tokenName: 'tk-opus',
          isDefault: false,
          username: 'alice',
          siteId: 1,
          siteName: 'site-a',
        },
        {
          modelName: 'dup',
          accountId: 11,
          tokenId: 101,
          tokenName: 'tk-opus',
          isDefault: false,
          username: 'alice',
          siteId: 1,
          siteName: 'site-a',
        },
      ],
      'claude-sonnet-4-6': [
        {
          modelName: 'will-be-overwritten',
          accountId: 11,
          tokenId: 102,
          tokenName: 'tk-sonnet',
          isDefault: true,
          username: 'alice',
          siteId: 1,
          siteName: 'site-a',
        },
      ],
      'gpt-4o-mini': [
        {
          modelName: 'will-be-overwritten',
          accountId: 22,
          tokenId: 201,
          tokenName: 'tk-gpt',
          isDefault: true,
          username: 'bob',
          siteId: 2,
          siteName: 'site-b',
        },
      ],
    };

    const index = buildRouteModelCandidatesIndex(routes, modelCandidates, matchesModelPattern);

    expect(index[1]).toBeTruthy();
    expect(index[2]).toBeTruthy();

    const route1Candidates = index[1].routeCandidates;
    expect(route1Candidates).toHaveLength(2);
    expect(route1Candidates.map((item) => item.modelName).sort()).toEqual([
      'claude-opus-4-6',
      'claude-sonnet-4-6',
    ]);

    expect(index[1].accountOptions).toEqual([{ id: 11, label: 'alice @ site-a' }]);

    const route1TokenOptions = index[1].tokenOptionsByAccountId[11];
    expect(route1TokenOptions.map((item) => `${item.id}:${item.sourceModel}:${item.isDefault}`)).toEqual([
      '102:claude-sonnet-4-6:true',
      '101:claude-opus-4-6:false',
    ]);

    expect(index[2].routeCandidates).toHaveLength(1);
    expect(index[2].routeCandidates[0].modelName).toBe('gpt-4o-mini');
    expect(index[2].accountOptions).toEqual([{ id: 22, label: 'bob @ site-b' }]);
  });

  it('returns empty structures when route model pattern is blank', () => {
    const routes = [{ id: 1, modelPattern: '   ' }];
    const modelCandidates: RouteModelCandidatesByModelName = {
      'gpt-4o-mini': [{
        modelName: 'ignored',
        accountId: 1,
        tokenId: 2,
        tokenName: 'tk',
        isDefault: false,
        username: null,
        siteId: 1,
        siteName: 'site',
      } satisfies IndexedRouteModelCandidate],
    };

    const index = buildRouteModelCandidatesIndex(routes, modelCandidates, matchesModelPattern);
    expect(index[1].routeCandidates).toEqual([]);
    expect(index[1].accountOptions).toEqual([]);
    expect(index[1].tokenOptionsByAccountId).toEqual({});
  });
});
