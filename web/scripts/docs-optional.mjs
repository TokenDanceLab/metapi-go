#!/usr/bin/env node
/**
 * S3 (#6): VitePress docs tooling is intentionally optional under Vite 8.
 *
 * Stable VitePress 1.6.x still depends on Vite 5 and has no product docs tree
 * under web/docs. Product markdown lives in the repo-root /docs tree and is not
 * built by these scripts. CI does not invoke docs:* — only audit:prod, typecheck,
 * test, and build:web.
 *
 * When a Vite-8-compatible VitePress (and mermaid plugin) are productized with a
 * real web/docs site, replace this stub with real vitepress commands.
 */
const mode = process.argv[2] || 'build';

const lines = [
  `[docs:${mode}] optional — skipped (S3 Vite 8 tooling closure)`,
  '  - Product docs: repo-root /docs (markdown SSOT, not VitePress)',
  '  - No web/docs VitePress site is provisioned in this tree',
  '  - vitepress / vitepress-plugin-mermaid removed to avoid a nested Vite 5 tree',
  '  - CI gate remains: npm run audit:prod && typecheck && test && build:web',
];

console.log(lines.join('\n'));
process.exit(0);
