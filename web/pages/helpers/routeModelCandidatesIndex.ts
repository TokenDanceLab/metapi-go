export type IndexedRouteModelCandidate = {
  modelName: string;
  accountId: number;
  tokenId: number;
  tokenName: string;
  isDefault: boolean;
  username: string | null;
  siteId: number;
  siteName: string;
};

export type RouteModelCandidatesByModelName = Record<string, IndexedRouteModelCandidate[]>;

export type RouteAccountOption = {
  id: number;
  label: string;
};

export type RouteTokenOption = {
  id: number;
  name: string;
  isDefault: boolean;
  sourceModel?: string;
};

export type RouteCandidateView = {
  routeCandidates: IndexedRouteModelCandidate[];
  accountOptions: RouteAccountOption[];
  tokenOptionsByAccountId: Record<number, RouteTokenOption[]>;
};

export type RouteModelPatternLike = {
  id: number;
  modelPattern: string;
  routeMode?: string | null;
};

const EMPTY_ROUTE_CANDIDATE_VIEW: RouteCandidateView = {
  routeCandidates: [],
  accountOptions: [],
  tokenOptionsByAccountId: {},
};

export function buildRouteModelCandidatesIndex(
  routes: RouteModelPatternLike[],
  modelCandidates: RouteModelCandidatesByModelName,
  matchesModelPattern: (model: string, pattern: string) => boolean,
): Record<number, RouteCandidateView> {
  const index: Record<number, RouteCandidateView> = {};

  for (const route of routes || []) {
    if (route.routeMode === 'explicit_group') {
      index[route.id] = EMPTY_ROUTE_CANDIDATE_VIEW;
      continue;
    }
    const modelPattern = (route.modelPattern || '').trim();
    if (!modelPattern) {
      index[route.id] = EMPTY_ROUTE_CANDIDATE_VIEW;
      continue;
    }

    const deduped = new Map<string, IndexedRouteModelCandidate>();
    for (const [modelName, candidates] of Object.entries(modelCandidates || {})) {
      if (!matchesModelPattern(modelName, modelPattern)) continue;
      for (const candidate of candidates || []) {
        const key = `${candidate.tokenId}::${modelName}`;
        if (!deduped.has(key)) {
          deduped.set(key, {
            ...candidate,
            modelName,
          });
        }
      }
    }

    const routeCandidates = Array.from(deduped.values()).sort((a, b) => {
      if (a.accountId === b.accountId) return a.tokenId - b.tokenId;
      return a.accountId - b.accountId;
    });

    const accountMap = new Map<number, string>();
    const tokenOptionsByAccountId: Record<number, RouteTokenOption[]> = {};
    for (const candidate of routeCandidates) {
      if (!accountMap.has(candidate.accountId)) {
        accountMap.set(candidate.accountId, `${candidate.username || `account-${candidate.accountId}`} @ ${candidate.siteName}`);
      }
      if (!tokenOptionsByAccountId[candidate.accountId]) {
        tokenOptionsByAccountId[candidate.accountId] = [];
      }
      tokenOptionsByAccountId[candidate.accountId].push({
        id: candidate.tokenId,
        name: candidate.tokenName,
        isDefault: candidate.isDefault,
        sourceModel: candidate.modelName,
      });
    }

    for (const accountIdText of Object.keys(tokenOptionsByAccountId)) {
      const accountId = Number.parseInt(accountIdText, 10);
      tokenOptionsByAccountId[accountId].sort((a, b) => {
        if (a.isDefault === b.isDefault) {
          if (a.id === b.id) return (a.sourceModel || '').localeCompare(b.sourceModel || '', undefined, { sensitivity: 'base' });
          return a.id - b.id;
        }
        return a.isDefault ? -1 : 1;
      });
    }

    const accountOptions = Array.from(accountMap.entries()).map(([id, label]) => ({ id, label }));
    index[route.id] = {
      routeCandidates,
      accountOptions,
      tokenOptionsByAccountId,
    };
  }

  return index;
}
