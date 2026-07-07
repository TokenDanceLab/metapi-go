import { describe, expect, it } from 'vitest';
import { analyzePrimarySiteUrl, safeExternalHref } from './sitePrimaryUrl.js';

describe('sitePrimaryUrl safety', () => {
  it('normalizes http and https URLs', () => {
    expect(analyzePrimarySiteUrl('example.com/v1').persistedUrl).toBe('https://example.com');
    expect(safeExternalHref('https://example.com/path?q=1')).toBe('https://example.com/path?q=1');
  });

  it('rejects non-http schemes for persistence and rendering', () => {
    for (const raw of [
      'javascript://alert(1)',
      'javascript:alert(1)',
      'data:text/html,<script>alert(1)</script>',
      'ftp://example.com/file',
    ]) {
      const analyzed = analyzePrimarySiteUrl(raw);
      expect(analyzed.persistedUrl).toBe('');
      expect(analyzed.action).toBe('invalid_url');
      expect(safeExternalHref(raw)).toBe('');
    }
  });
});
