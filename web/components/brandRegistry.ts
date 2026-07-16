export {
  collectBrandCandidates,
  getAllBrandNames,
  getAllBrands,
  getBrand,
  stripCommonWrappers,
  type BrandInfo,
  type BrandMatchContext,
} from '../shared/modelBrand.js';

const LEGACY_ICON_ALIASES: Record<string, string> = {
  anthropic: 'claude-color',
  'claude.color': 'claude-color',
  'cohere.color': 'cohere-color',
  'doubao.color': 'doubao-color',
  'gemini.color': 'gemini-color',
  'hunyuan.color': 'hunyuan-color',
  meta: 'meta-color',
  'meta-brand-color': 'meta-color',
  'minimax.color': 'minimax-color',
  'qwen.color': 'qwen-color',
  'spark.color': 'spark-color',
  stability: 'stability-color',
  'stability-brand-color': 'stability-color',
  stepfun: 'stepfun-color',
  'wenxin.color': 'wenxin-color',
  xai: 'xai',
  'yi.color': 'yi-color',
  'zhipu.color': 'zhipu-color',
  azure: 'microsoft-color',
  'bytedance-brand-color': 'bytedance-color',
};

function normalizeInput(value: string): string {
  return String(value || '').trim().toLowerCase();
}

const FALLBACK_COLORS = [
  'linear-gradient(135deg, var(--color-chart-1), color-mix(in srgb, var(--color-chart-1) 55%, white))',
  'linear-gradient(135deg, var(--color-chart-3), color-mix(in srgb, var(--color-chart-3) 55%, white))',
  'linear-gradient(135deg, var(--color-chart-2), color-mix(in srgb, var(--color-chart-2) 55%, white))',
  'linear-gradient(135deg, var(--color-chart-7), color-mix(in srgb, var(--color-chart-7) 55%, white))',
  'linear-gradient(135deg, var(--color-stat-orange-ink), color-mix(in srgb, var(--color-stat-orange-ink) 55%, white))',
  'linear-gradient(135deg, var(--color-stat-cyan-ink), color-mix(in srgb, var(--color-stat-cyan-ink) 55%, white))',
  'linear-gradient(135deg, var(--color-chart-6), color-mix(in srgb, var(--color-chart-6) 55%, white))',
  'linear-gradient(135deg, var(--color-chart-5), color-mix(in srgb, var(--color-chart-5) 55%, white))',
];

export function normalizeBrandIconKey(icon: string | null | undefined): string | null {
  const normalized = normalizeInput(icon || '').replace(/\./g, '-');
  if (!normalized) return null;
  return LEGACY_ICON_ALIASES[normalized] || normalized;
}

export function getBrandIconUrl(icon: string | null | undefined, cdn: string): string | null {
  const normalized = normalizeBrandIconKey(icon);
  if (!normalized) return null;
  return `${cdn}/${normalized}.png`;
}

export function hashColor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i += 1) h = (h * 31 + name.charCodeAt(i)) | 0;
  return FALLBACK_COLORS[Math.abs(h) % FALLBACK_COLORS.length]!;
}

export function avatarLetters(name: string): string {
  const parts = name.replace(/[-_/.]/g, ' ').trim().split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]![0] + parts[1]![0]).toUpperCase();
  return name.slice(0, 2).toUpperCase();
}
