function normalize(value: string): string {
  return (value || '').toLowerCase().trim();
}

function compact(value: string): string {
  return normalize(value).replace(/[^a-z0-9]/g, '');
}

function isSubsequence(text: string, query: string): boolean {
  if (!query) return true;
  let qi = 0;
  for (let i = 0; i < text.length; i += 1) {
    if (text[i] === query[qi]) qi += 1;
    if (qi >= query.length) return true;
  }
  return false;
}

export function fuzzyMatch(text: string, query: string): boolean {
  const q = normalize(query);
  if (!q) return true;

  const t = normalize(text);
  if (t.includes(q)) return true;

  const compactText = compact(t);
  const compactQuery = compact(q);
  if (!compactQuery) return true;
  if (compactText.includes(compactQuery)) return true;

  return isSubsequence(compactText, compactQuery);
}
