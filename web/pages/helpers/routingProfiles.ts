export type RoutingWeights = {
  baseWeightFactor: number;
  valueScoreFactor: number;
  costWeight: number;
  balanceWeight: number;
  usageWeight: number;
};

type RoutingProfilePresetId = 'balanced' | 'stable' | 'cost' | 'custom';

export const ROUTING_PROFILE_PRESETS: Record<Exclude<RoutingProfilePresetId, 'custom'>, RoutingWeights> = {
  balanced: {
    baseWeightFactor: 0.5,
    valueScoreFactor: 0.5,
    costWeight: 0.4,
    balanceWeight: 0.3,
    usageWeight: 0.3,
  },
  stable: {
    baseWeightFactor: 0.7,
    valueScoreFactor: 0.3,
    costWeight: 0.2,
    balanceWeight: 0.6,
    usageWeight: 0.2,
  },
  cost: {
    baseWeightFactor: 0.35,
    valueScoreFactor: 0.65,
    costWeight: 0.75,
    balanceWeight: 0.15,
    usageWeight: 0.1,
  },
};

const EPSILON = 1e-6;

export function applyRoutingProfilePreset(presetId: Exclude<RoutingProfilePresetId, 'custom'>): RoutingWeights {
  return {
    ...ROUTING_PROFILE_PRESETS[presetId],
  };
}

function weightsAlmostEqual(a: RoutingWeights, b: RoutingWeights): boolean {
  return (
    Math.abs(a.baseWeightFactor - b.baseWeightFactor) <= EPSILON
    && Math.abs(a.valueScoreFactor - b.valueScoreFactor) <= EPSILON
    && Math.abs(a.costWeight - b.costWeight) <= EPSILON
    && Math.abs(a.balanceWeight - b.balanceWeight) <= EPSILON
    && Math.abs(a.usageWeight - b.usageWeight) <= EPSILON
  );
}

export function resolveRoutingProfilePreset(weights: RoutingWeights): RoutingProfilePresetId {
  if (weightsAlmostEqual(weights, ROUTING_PROFILE_PRESETS.balanced)) return 'balanced';
  if (weightsAlmostEqual(weights, ROUTING_PROFILE_PRESETS.stable)) return 'stable';
  if (weightsAlmostEqual(weights, ROUTING_PROFILE_PRESETS.cost)) return 'cost';
  return 'custom';
}
