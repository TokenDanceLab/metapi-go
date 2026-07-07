export const ROUTE_DECISION_REFRESH_TASK_TYPE: 'route-decision.refresh';

export type RouteMode = 'pattern' | 'explicit_group';

export type RouteDecisionCandidate = {
  channelId: number;
  eligible: boolean;
  probability?: number | null;
  reason: string;
  status?: string | null;
  available?: boolean | null;
  cooldownUntil?: string | null;
  failureCount?: number | null;
  avoidedByRecentFailure?: boolean;
  recentlyFailed?: boolean;
};

export type RouteDecision = {
  candidates?: RouteDecisionCandidate[] | null;
  selectedChannelId?: number | null;
  refreshedAt?: string | null;
  reason?: string | null;
};

export function normalizeTokenRouteMode(routeMode: unknown): RouteMode;
