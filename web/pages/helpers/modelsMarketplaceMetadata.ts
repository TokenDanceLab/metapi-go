export interface MarketplaceModelMetadataShape {
  description: string | null;
  tags: string[];
  supportedEndpointTypes: string[];
  pricingSources: unknown[];
}

interface MarketplaceModelLike extends MarketplaceModelMetadataShape {
  name: string;
}

function normalizeModelName(name: string): string {
  return name.trim().toLowerCase();
}

function hasMarketplaceMetadata(model: MarketplaceModelMetadataShape): boolean {
  if (typeof model.description === 'string' && model.description.trim().length > 0) return true;
  if (Array.isArray(model.tags) && model.tags.length > 0) return true;
  if (Array.isArray(model.supportedEndpointTypes) && model.supportedEndpointTypes.length > 0) return true;
  if (Array.isArray(model.pricingSources) && model.pricingSources.length > 0) return true;
  return false;
}

export function shouldHydrateMarketplaceMetadata<T extends MarketplaceModelMetadataShape>(models: T[]): boolean {
  return models.some((model) => !hasMarketplaceMetadata(model));
}

export function mergeMarketplaceMetadata<T extends MarketplaceModelLike>(baseModels: T[], detailedModels: T[]): T[] {
  const detailedByName = new Map<string, T>();
  for (const model of detailedModels) {
    detailedByName.set(normalizeModelName(model.name), model);
  }

  return baseModels.map((model) => {
    const detail = detailedByName.get(normalizeModelName(model.name));
    if (!detail) return model;

    return {
      ...model,
      description: detail.description ?? null,
      tags: Array.isArray(detail.tags) ? detail.tags : [],
      supportedEndpointTypes: Array.isArray(detail.supportedEndpointTypes) ? detail.supportedEndpointTypes : [],
      pricingSources: Array.isArray(detail.pricingSources) ? detail.pricingSources : [],
    };
  });
}
