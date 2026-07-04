import { describe, expect, it } from 'vitest';
import {
  ROUTING_PROFILE_PRESETS,
  applyRoutingProfilePreset,
  resolveRoutingProfilePreset,
} from './routingProfiles.js';

describe('routingProfiles', () => {
  it('applies stable preset weights', () => {
    const stable = applyRoutingProfilePreset('stable');
    expect(stable).toEqual(ROUTING_PROFILE_PRESETS.stable);
  });

  it('resolves known profile by exact weights', () => {
    const preset = resolveRoutingProfilePreset(ROUTING_PROFILE_PRESETS.cost);
    expect(preset).toBe('cost');
  });

  it('returns custom when weights do not match presets', () => {
    const preset = resolveRoutingProfilePreset({
      baseWeightFactor: 0.61,
      valueScoreFactor: 0.39,
      costWeight: 0.22,
      balanceWeight: 0.41,
      usageWeight: 0.37,
    });
    expect(preset).toBe('custom');
  });
});
