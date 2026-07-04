import { describe, expect, it } from 'vitest';
import {
  mergeMarketplaceMetadata,
  shouldHydrateMarketplaceMetadata,
  type MarketplaceModelMetadataShape,
} from './modelsMarketplaceMetadata.js';

interface TestModel extends MarketplaceModelMetadataShape {
  name: string;
  accountCount: number;
}

function createModel(overrides: Partial<TestModel>): TestModel {
  return {
    name: 'gpt-4o',
    accountCount: 1,
    description: null,
    tags: [],
    supportedEndpointTypes: [],
    pricingSources: [],
    ...overrides,
  };
}

describe('modelsMarketplaceMetadata', () => {
  it('identifies when metadata hydration is needed', () => {
    const models = [
      createModel({ name: 'gpt-4o', description: 'base model' }),
      createModel({ name: 'claude-sonnet-4' }),
    ];

    expect(shouldHydrateMarketplaceMetadata(models)).toBe(true);
  });

  it('identifies when metadata is already complete', () => {
    const models = [
      createModel({
        name: 'gpt-4o',
        description: 'base model',
        tags: ['chat'],
        supportedEndpointTypes: ['chat'],
        pricingSources: [{ source: 'site-a' }],
      }),
      createModel({
        name: 'claude-sonnet-4',
        description: 'anthropic model',
        tags: ['reasoning'],
        supportedEndpointTypes: ['chat'],
        pricingSources: [{ source: 'site-b' }],
      }),
    ];

    expect(shouldHydrateMarketplaceMetadata(models)).toBe(false);
  });

  it('merges metadata into current models without overriding non-metadata fields', () => {
    const base = [
      createModel({ name: 'GPT-4O', accountCount: 3 }),
      createModel({ name: 'claude-sonnet-4', accountCount: 5 }),
    ];
    const detail = [
      createModel({
        name: 'gpt-4o',
        accountCount: 99,
        description: 'OpenAI flagship model',
        tags: ['openai'],
        supportedEndpointTypes: ['chat', 'responses'],
        pricingSources: [{ source: 'site-a' }],
      }),
    ];

    const merged = mergeMarketplaceMetadata(base, detail);

    expect(merged).toEqual([
      {
        ...base[0],
        description: 'OpenAI flagship model',
        tags: ['openai'],
        supportedEndpointTypes: ['chat', 'responses'],
        pricingSources: [{ source: 'site-a' }],
      },
      base[1],
    ]);
  });
});
