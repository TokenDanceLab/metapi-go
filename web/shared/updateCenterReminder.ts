export type UpdateHelperRuntimeLike = {
  imageTag?: string | null;
  imageDigest?: string | null;
};

export type UpdateVersionCandidateLike = {
  source?: string | null;
  tagName?: string | null;
  displayVersion?: string | null;
  normalizedVersion?: string | null;
  digest?: string | null;
};

export type UpdateReminderCandidate = {
  source: 'github-release' | 'docker-hub';
  kind: 'new-version' | 'new-digest';
  version: string;
  digest?: string | null;
};

function clean(value?: string | null): string {
  return String(value || '').trim();
}

function cleanDigest(value?: string | null): string {
  const digest = clean(value);
  return /^sha256:[a-f0-9]{64}$/i.test(digest) ? digest.toLowerCase() : '';
}

export function normalizeStableVersion(value?: string | null): string {
  const raw = clean(value).toLowerCase();
  if (!raw || raw === 'latest') return '';
  return raw.replace(/^v/, '').replace(/[^0-9.].*$/, '');
}

export function compareStableVersions(a?: string | null, b?: string | null): -1 | 0 | 1 | null {
  const left = normalizeStableVersion(a);
  const right = normalizeStableVersion(b);
  if (!left || !right) return null;

  const leftParts = left.split('.').map((part) => Number.parseInt(part, 10) || 0);
  const rightParts = right.split('.').map((part) => Number.parseInt(part, 10) || 0);
  const length = Math.max(leftParts.length, rightParts.length);
  for (let index = 0; index < length; index += 1) {
    const l = leftParts[index] || 0;
    const r = rightParts[index] || 0;
    if (l < r) return -1;
    if (l > r) return 1;
  }
  return 0;
}

export function isSameImageTarget(
  helper: UpdateHelperRuntimeLike | null | undefined,
  target: { tag?: string | null; digest?: string | null },
): boolean {
  const helperDigest = cleanDigest(helper?.imageDigest);
  const targetDigest = cleanDigest(target.digest);
  if (helperDigest && targetDigest) return helperDigest === targetDigest;

  const helperTag = clean(helper?.imageTag);
  const targetTag = clean(target.tag);
  if (!helperTag || !targetTag) return false;
  if (helperTag === targetTag) return true;
  const helperStable = normalizeStableVersion(helperTag);
  const targetStable = normalizeStableVersion(targetTag);
  return Boolean(helperStable && targetStable && helperStable === targetStable);
}

export function buildUpdateReminderCandidateKey(candidate: UpdateReminderCandidate): string {
  return `${candidate.source}:${candidate.kind}:${candidate.version}:${candidate.digest || ''}`;
}

export function resolveUpdateReminderCandidate(input: {
  currentVersion?: string | null;
  helper?: UpdateHelperRuntimeLike | null;
  githubRelease?: UpdateVersionCandidateLike | null;
  dockerHubTag?: UpdateVersionCandidateLike | null;
}): UpdateReminderCandidate | null {
  const githubVersion = clean(input.githubRelease?.normalizedVersion || input.githubRelease?.tagName);
  const helperGithubCompare = compareStableVersions(input.helper?.imageTag, githubVersion);
  if (
    compareStableVersions(input.currentVersion, githubVersion) === -1
    && (helperGithubCompare == null || helperGithubCompare === -1)
  ) {
    return {
      source: 'github-release',
      kind: 'new-version',
      version: githubVersion,
      digest: cleanDigest(input.githubRelease?.digest) || null,
    };
  }

  const dockerVersion = clean(input.dockerHubTag?.normalizedVersion || input.dockerHubTag?.tagName);
  const dockerDigest = cleanDigest(input.dockerHubTag?.digest);
  if (compareStableVersions(input.currentVersion, dockerVersion) === -1) {
    return {
      source: 'docker-hub',
      kind: 'new-version',
      version: dockerVersion,
      digest: dockerDigest || null,
    };
  }

  const helperDigest = cleanDigest(input.helper?.imageDigest);
  if (dockerDigest && helperDigest && dockerDigest !== helperDigest) {
    return {
      source: 'docker-hub',
      kind: 'new-digest',
      version: dockerVersion || clean(input.dockerHubTag?.displayVersion) || 'latest',
      digest: dockerDigest,
    };
  }

  return null;
}
