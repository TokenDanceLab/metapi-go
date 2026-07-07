export type BrandRuleMode = 'includes' | 'startsWith' | 'segment' | 'boundary';

export type BrandRule = {
  keyword: string;
  mode: BrandRuleMode | string;
};

export type BrandInfo = {
  name: string;
  icon: string;
  color: string;
};

export type BrandMatchContext = {
  raw: string;
  cleaned: string;
  segments: string[];
  candidates: string[];
};

type BrandDefinition = BrandInfo & {
  rules: BrandRule[];
};

const BRAND_DEFINITIONS: BrandDefinition[] = [
  { name: 'OpenAI', icon: 'openai', color: '#10a37f', rules: ['gpt', 'chatgpt', 'dall-e', 'whisper', 'davinci', 'babbage', 'codex-mini', 'o1', 'o3', 'o4', 'tts'].map((keyword) => ({ keyword, mode: 'startsWith' })) },
  { name: 'Anthropic', icon: 'claude-color', color: '#d4a574', rules: [{ keyword: 'claude', mode: 'includes' }] },
  { name: 'Google', icon: 'gemini-color', color: '#4285f4', rules: ['gemini', 'gemma', 'google/', 'palm', 'paligemma', 'shieldgemma', 'recurrentgemma', 'deplot', 'codegemma', 'imagen', 'learnlm', 'aqa'].map((keyword) => ({ keyword, mode: 'includes' })).concat([{ keyword: 'veo', mode: 'startsWith' }]) },
  { name: 'DeepSeek', icon: 'deepseek-color', color: '#4d6bfe', rules: [{ keyword: 'deepseek', mode: 'includes' }, { keyword: 'ds-chat', mode: 'segment' }] },
  { name: '通义千问', icon: 'qwen-color', color: '#615cf7', rules: ['qwen', 'qwq', 'tongyi'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: '智谱 AI', icon: 'zhipu-color', color: '#3b6cf5', rules: ['glm', 'chatglm', 'codegeex', 'cogview', 'cogvideo'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'Meta', icon: 'meta-color', color: '#0668e1', rules: ['llama', 'code-llama', 'codellama'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'Mistral', icon: 'mistral-color', color: '#f7d046', rules: ['mistral', 'mixtral', 'codestral', 'pixtral', 'ministral', 'voxtral', 'magistral'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'Moonshot', icon: 'moonshot', color: '#111827', rules: ['moonshot', 'kimi'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: '零一万物', icon: 'yi-color', color: '#1d4ed8', rules: [{ keyword: 'yi-', mode: 'startsWith' }, { keyword: 'yi', mode: 'boundary' }] },
  { name: '文心一言', icon: 'wenxin-color', color: '#2932e1', rules: ['ernie', 'eb-'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: '讯飞星火', icon: 'spark-color', color: '#0070f3', rules: ['spark', 'generalv'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: '腾讯混元', icon: 'hunyuan-color', color: '#00b7ff', rules: ['hunyuan', 'tencent-hunyuan'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: '豆包', icon: 'doubao-color', color: '#3b5bdb', rules: [{ keyword: 'doubao', mode: 'includes' }] },
  { name: 'MiniMax', icon: 'minimax-color', color: '#6366f1', rules: [{ keyword: 'minimax', mode: 'includes' }, { keyword: 'abab', mode: 'includes' }, { keyword: 'mini2.1', mode: 'segment' }] },
  { name: 'Cohere', icon: 'cohere-color', color: '#39594d', rules: ['command', 'c4ai-', 'aya'].map((keyword) => ({ keyword, mode: 'includes' })).concat([{ keyword: 'embed-', mode: 'startsWith' }]) },
  { name: 'Microsoft', icon: 'microsoft-color', color: '#00bcf2', rules: ['microsoft/', 'phi-', 'kosmos'].map((keyword) => ({ keyword, mode: 'includes' })).concat([{ keyword: 'phi4', mode: 'segment' }]) },
  { name: 'xAI', icon: 'xai', color: '#111827', rules: [{ keyword: 'grok', mode: 'includes' }] },
  { name: '阶跃星辰', icon: 'stepfun-color', color: '#0066ff', rules: [{ keyword: 'stepfun', mode: 'includes' }, { keyword: 'step-', mode: 'startsWith' }, { keyword: 'step3', mode: 'startsWith' }] },
  { name: '百川智能', icon: 'baichuan-color', color: '#0f766e', rules: [{ keyword: 'baichuan', mode: 'includes' }] },
  { name: 'AI21 Labs', icon: 'ai21-brand-color', color: '#7c3aed', rules: [{ keyword: 'ai21', mode: 'includes' }, { keyword: 'jamba', mode: 'startsWith' }, { keyword: 'jamba', mode: 'includes' }] },
  { name: 'AI2', icon: 'ai2-color', color: '#0f766e', rules: ['allenai', 'olmo'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'Amazon Nova', icon: 'nova', color: '#f59e0b', rules: ['amazon/nova', 'nova-', 'nova-lite', 'nova-pro', 'nova-micro', 'nova-canvas', 'nova-reel'].map((keyword) => ({ keyword, mode: 'startsWith' })).concat([{ keyword: 'amazon.nova', mode: 'includes' }, { keyword: 'us.amazon.nova', mode: 'includes' }]) },
  { name: 'Stability', icon: 'stability-color', color: '#8b5cf6', rules: ['flux', 'stablediffusion', 'stable-diffusion', 'sdxl'].map((keyword) => ({ keyword, mode: 'includes' })).concat([{ keyword: 'sd3', mode: 'startsWith' }]) },
  { name: 'NVIDIA', icon: 'nvidia-color', color: '#76b900', rules: ['nvidia/', 'nvclip', 'nemotron', 'nemoretriever', 'neva', 'riva-translate', 'cosmos'].map((keyword) => ({ keyword, mode: 'includes' })).concat([{ keyword: 'nv-', mode: 'startsWith' }]) },
  { name: 'IBM', icon: 'ibm', color: '#0f62fe', rules: ['ibm/', 'granite'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'BAAI', icon: 'baai', color: '#111827', rules: ['baai/', 'bge-'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'ByteDance', icon: 'bytedance-color', color: '#325ab4', rules: ['bytedance', 'seed-oss', 'kolors', 'kwai', 'kwaipilot'].map((keyword) => ({ keyword, mode: 'includes' })).concat([{ keyword: 'wan-', mode: 'startsWith' }, { keyword: 'kat-', mode: 'startsWith' }]) },
  { name: 'InternLM', icon: 'internlm-color', color: '#1b3882', rules: [{ keyword: 'internlm', mode: 'includes' }] },
  { name: 'Midjourney', icon: 'midjourney', color: '#4c6ef5', rules: [{ keyword: 'midjourney', mode: 'includes' }, { keyword: 'mj_', mode: 'startsWith' }] },
  { name: 'DeepL', icon: 'deepl-color', color: '#0f2b46', rules: [{ keyword: 'deepl-', mode: 'startsWith' }, { keyword: 'deepl/', mode: 'includes' }] },
  { name: 'Jina AI', icon: 'jina', color: '#111827', rules: [{ keyword: 'jina', mode: 'includes' }] },
  { name: 'Relace', icon: 'relace', color: '#7c3aed', rules: [{ keyword: 'relace', mode: 'includes' }] },
  { name: 'Arcee', icon: 'arcee-color', color: '#2563eb', rules: ['arcee-ai', 'arcee'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'AionLabs', icon: 'aionlabs-color', color: '#0f766e', rules: ['aion-labs', 'aionlabs'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'DeepCogito', icon: 'deepcogito-color', color: '#2563eb', rules: [{ keyword: 'deepcogito', mode: 'includes' }] },
  { name: 'Essential AI', icon: 'essentialai-color', color: '#0f172a', rules: [{ keyword: 'essentialai', mode: 'includes' }] },
  { name: 'Inception', icon: 'inception', color: '#7c3aed', rules: [{ keyword: 'inception', mode: 'includes' }] },
  { name: 'Inflection', icon: 'inflection', color: '#1d4ed8', rules: [{ keyword: 'inflection', mode: 'includes' }] },
  { name: 'Liquid AI', icon: 'liquid', color: '#0f172a', rules: [{ keyword: 'liquid', mode: 'includes' }, { keyword: 'lfm-', mode: 'startsWith' }] },
  { name: 'LongCat', icon: 'longcat-color', color: '#f97316', rules: [{ keyword: 'longcat', mode: 'includes' }] },
  { name: 'Morph', icon: 'morph-color', color: '#4f46e5', rules: [{ keyword: 'morph/', mode: 'includes' }, { keyword: 'morph-', mode: 'startsWith' }] },
  { name: 'Nous Research', icon: 'nousresearch', color: '#111827', rules: [{ keyword: 'nousresearch', mode: 'includes' }] },
  { name: 'Upstage', icon: 'upstage-color', color: '#2563eb', rules: [{ keyword: 'upstage', mode: 'includes' }] },
  { name: 'Xiaomi MiMo', icon: 'xiaomimimo', color: '#f97316', rules: [{ keyword: 'xiaomi/mimo', mode: 'includes' }, { keyword: 'xiaomimimo', mode: 'includes' }, { keyword: 'mimo-v', mode: 'startsWith' }] },
  { name: 'Z.ai', icon: 'zai', color: '#0f172a', rules: [{ keyword: '2zai', mode: 'startsWith' }, { keyword: 'z-ai', mode: 'startsWith' }] },
  { name: 'SenseNova', icon: 'sensenova-brand-color', color: '#f59e0b', rules: [{ keyword: 'sensenova', mode: 'includes' }] },
  { name: 'OpenRouter', icon: 'openrouter', color: '#7c3aed', rules: ['openrouter', 'openrouter-'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'Groq', icon: 'groq', color: '#111827', rules: [{ keyword: 'groq', mode: 'includes' }] },
  { name: 'DeepInfra', icon: 'deepinfra-color', color: '#4f46e5', rules: [{ keyword: 'deepinfra', mode: 'includes' }] },
  { name: 'Fireworks', icon: 'fireworks-color', color: '#fb7185', rules: ['fireworks-ai', 'fireworks'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'Together AI', icon: 'together-brand-color', color: '#7c3aed', rules: ['together.ai', 'together'].map((keyword) => ({ keyword, mode: 'includes' })) },
  { name: 'Replicate', icon: 'replicate-brand', color: '#111827', rules: [{ keyword: 'replicate', mode: 'includes' }] },
  { name: 'Cerebras', icon: 'cerebras-brand-color', color: '#0f766e', rules: [{ keyword: 'cerebras', mode: 'includes' }] },
  { name: '百炼', icon: 'bailian-color', color: '#7c3aed', rules: ['bailian', 'dashscope'].map((keyword) => ({ keyword, mode: 'includes' })) },
];

function normalize(value: string | null | undefined): string {
  return String(value || '').trim().toLowerCase();
}

export function stripCommonWrappers(value: string): string {
  return normalize(value)
    .replace(/^(?:\[[^\]]+\]|【[^】]+】)\s*/g, '')
    .replace(/^re:\s*/g, '')
    .replace(/^\^+/, '')
    .replace(/\$+$/, '')
    .trim();
}

export function collectBrandCandidates(value: string): string[] {
  const candidates: string[] = [];
  const seen = new Set<string>();
  const add = (candidate: string) => {
    const normalized = normalize(candidate);
    if (!normalized || seen.has(normalized)) return;
    seen.add(normalized);
    candidates.push(normalized);
  };

  add(value);
  for (let index = 0; index < candidates.length; index += 1) {
    const candidate = candidates[index]!;
    const stripped = stripCommonWrappers(candidate);
    add(stripped);
    for (const part of stripped.split(/[/:,\s]+/g)) add(part);
  }
  return candidates;
}

function buildContext(value: string): BrandMatchContext {
  const candidates = collectBrandCandidates(value);
  const raw = candidates[0] || normalize(value);
  const cleaned = stripCommonWrappers(raw);
  const segments = Array.from(new Set(candidates.flatMap((candidate) => candidate.split(/[/:,\s]+/g)).filter(Boolean)));
  return { raw, cleaned, segments, candidates };
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function matchesRule(context: BrandMatchContext, rule: BrandRule): boolean {
  switch (rule.mode) {
    case 'includes':
      return context.raw.includes(rule.keyword)
        || context.cleaned.includes(rule.keyword)
        || context.candidates.some((candidate) => candidate.includes(rule.keyword));
    case 'startsWith':
      return context.raw.startsWith(rule.keyword)
        || context.cleaned.startsWith(rule.keyword)
        || context.segments.some((segment) => segment.startsWith(rule.keyword))
        || context.candidates.some((candidate) => candidate.startsWith(rule.keyword));
    case 'segment':
      return context.segments.includes(rule.keyword);
    case 'boundary': {
      const pattern = new RegExp(`(^|[/:_\\-\\s])${escapeRegExp(rule.keyword)}(?=$|[/:_\\-\\s])`);
      return pattern.test(context.raw)
        || pattern.test(context.cleaned)
        || context.candidates.some((candidate) => pattern.test(candidate));
    }
    default:
      return false;
  }
}

function toBrandInfo(definition: BrandDefinition): BrandInfo {
  return {
    name: definition.name,
    icon: definition.icon,
    color: definition.color,
  };
}

export function getAllBrands(): BrandInfo[] {
  return BRAND_DEFINITIONS.map(toBrandInfo);
}

export function getAllBrandNames(): string[] {
  return BRAND_DEFINITIONS.map((brand) => brand.name);
}

export function getBrand(value: string | null | undefined): BrandInfo | null {
  const context = buildContext(String(value || ''));
  for (const definition of BRAND_DEFINITIONS) {
    if (definition.rules.some((rule) => matchesRule(context, rule))) {
      return toBrandInfo(definition);
    }
  }
  return null;
}
