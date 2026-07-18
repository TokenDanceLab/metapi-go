import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Badge,
  Button,
  Card,
  Inline,
  Input,
  Stack,
  Surface,
} from '../design-system/index.js';

type ThemeChoice = 'light' | 'dark';

function readDocumentTheme(): ThemeChoice {
  if (typeof document === 'undefined') return 'light';
  return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
}

function setDocumentTheme(theme: ThemeChoice) {
  document.documentElement.setAttribute('data-theme', theme);
  try {
    document.documentElement.style.colorScheme = theme;
  } catch {
    /* ignore incomplete CSSOM */
  }
}

/**
 * Dev/visual acceptance gallery for design-system primitives (#533).
 * Routed at /__design__ behind DEV or localStorage.metapi_design_gallery === '1'.
 */
export default function DesignSystemGallery() {
  const [theme, setTheme] = useState<ThemeChoice>(() => readDocumentTheme());
  const [inputValue, setInputValue] = useState('metapi-design');

  useEffect(() => {
    setDocumentTheme(theme);
  }, [theme]);

  const setLight = useCallback(() => setTheme('light'), []);
  const setDark = useCallback(() => setTheme('dark'), []);

  const sampleSurfaces = useMemo(
    () => (
      [
        { variant: 'solid' as const, label: 'Solid' },
        { variant: 'glass' as const, label: 'Glass' },
        { variant: 'sunken' as const, label: 'Sunken' },
      ]
    ),
    [],
  );

  const kpis = useMemo(
    () => [
      { label: 'Requests / min', value: '12.4k', delta: '+6.2%', tone: 'success' as const },
      { label: 'Error rate', value: '0.18%', delta: '−0.04%', tone: 'success' as const },
      { label: 'Active sites', value: '103', delta: '+2', tone: 'info' as const },
      { label: 'Pool pressure', value: '1 / 1', delta: 'shared-tiny', tone: 'warn' as const },
    ],
    [],
  );

  return (
    <div className="ds-gallery" data-testid="design-system-gallery">
      <header className="ds-gallery__header">
        <div>
          <p className="ds-gallery__eyebrow">UI-REFRESH · M51</p>
          <h1 className="ds-gallery__title">MetAPI Design System</h1>
          <p className="ds-gallery__subtitle">
            GCP console calm + frosted glass + Apple detail. Primitives use the <code>ds-</code> prefix
            and token vars only — no page-level hex.
          </p>
        </div>
        <Inline gap={2}>
          <Button
            variant={theme === 'light' ? 'primary' : 'secondary'}
            size="sm"
            onClick={setLight}
            aria-pressed={theme === 'light'}
          >
            Light
          </Button>
          <Button
            variant={theme === 'dark' ? 'primary' : 'secondary'}
            size="sm"
            onClick={setDark}
            aria-pressed={theme === 'dark'}
          >
            Dark
          </Button>
        </Inline>
      </header>

      <section className="ds-gallery__section" aria-labelledby="ds-kpis">
        <h2 id="ds-kpis" className="ds-gallery__section-title">KPI cards</h2>
        <div className="ds-gallery__kpi-grid">
          {kpis.map((kpi) => (
            <article key={kpi.label} className="ds-gallery__kpi">
              <div className="ds-gallery__kpi-label">{kpi.label}</div>
              <div className="ds-gallery__kpi-value">{kpi.value}</div>
              <Badge tone={kpi.tone}>{kpi.delta}</Badge>
            </article>
          ))}
        </div>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-buttons">
        <h2 id="ds-buttons" className="ds-gallery__section-title">Button</h2>
        <Surface variant="solid" padding="md">
          <Stack gap={4}>
            <div>
              <p className="ds-gallery__swatch-meta" style={{ marginBottom: 8 }}>variant × size=md</p>
              <Inline gap={2}>
                <Button variant="primary">Primary</Button>
                <Button variant="secondary">Secondary</Button>
                <Button variant="ghost">Ghost</Button>
                <Button variant="danger">Danger</Button>
                <Button variant="primary" disabled>Disabled</Button>
              </Inline>
            </div>
            <div>
              <p className="ds-gallery__swatch-meta" style={{ marginBottom: 8 }}>size=sm · calm motion</p>
              <Inline gap={2}>
                <Button variant="primary" size="sm">Primary</Button>
                <Button variant="secondary" size="sm">Secondary</Button>
                <Button variant="ghost" size="sm">Ghost</Button>
                <Button variant="danger" size="sm">Danger</Button>
              </Inline>
            </div>
          </Stack>
        </Surface>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-surfaces">
        <h2 id="ds-surfaces" className="ds-gallery__section-title">Surface material</h2>
        <div className="ds-gallery__grid">
          {sampleSurfaces.map((item) => (
            <Surface key={item.variant} variant={item.variant} padding="md">
              <Stack gap={2}>
                <strong>{item.label}</strong>
                <span className="ds-gallery__swatch-meta">
                  Glass for shell/modal only; solid for dense tables; sunken for wells.
                </span>
              </Stack>
            </Surface>
          ))}
        </div>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-cards">
        <h2 id="ds-cards" className="ds-gallery__section-title">Card hierarchy</h2>
        <div className="ds-gallery__grid">
          <Card title="Sites" description="Primary data surface with hairline border and soft dual shadow.">
            <Inline gap={2}>
              <Badge tone="success">healthy</Badge>
              <Badge tone="info">103 online</Badge>
            </Inline>
            <div className="ds-gallery__fake-table" aria-hidden="true">
              <div /><div /><div />
              <div /><div /><div />
            </div>
          </Card>
          <Card title="Routes" description="Secondary card with actions.">
            <Inline gap={2}>
              <Button size="sm" variant="primary">Create</Button>
              <Button size="sm" variant="secondary">Import</Button>
            </Inline>
          </Card>
          <Card title="Alerts" description="Soft semantic badges stay dark-safe.">
            <Inline gap={2}>
              <Badge tone="warn">degraded</Badge>
              <Badge tone="danger">53300</Badge>
              <Badge tone="neutral">idle</Badge>
            </Inline>
          </Card>
        </div>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-badges">
        <h2 id="ds-badges" className="ds-gallery__section-title">Badge</h2>
        <Surface variant="solid" padding="md">
          <Inline gap={2}>
            <Badge tone="success">success</Badge>
            <Badge tone="warn">warn</Badge>
            <Badge tone="danger">danger</Badge>
            <Badge tone="info">info</Badge>
            <Badge tone="neutral">neutral</Badge>
            <span className="badge badge-purple">purple</span>
          </Inline>
        </Surface>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-data">
        <h2 id="ds-data" className="ds-gallery__section-title">Data surfaces</h2>
        <Surface variant="solid" padding="md">
          <Stack gap={4}>
            <div className="ds-gallery__filter-row" role="group" aria-label="Sample filters">
              <button type="button" className="filter-chip active">
                <span className="filter-chip-label">All sites</span>
                <span className="filter-chip-count">103</span>
              </button>
              <button type="button" className="filter-chip">
                <span className="filter-chip-label">Healthy</span>
                <span className="filter-chip-count">98</span>
              </button>
              <button type="button" className="filter-chip">
                <span className="filter-chip-label">Degraded</span>
                <span className="filter-chip-count">5</span>
              </button>
              <div className="pill-tabs" role="tablist" aria-label="View mode">
                <button type="button" className="pill-tab active" role="tab" aria-selected="true">
                  Table
                </button>
                <button type="button" className="pill-tab" role="tab" aria-selected="false">
                  Cards
                </button>
              </div>
            </div>

            <div className="ds-gallery__table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th scope="col">Site</th>
                    <th scope="col">Platform</th>
                    <th scope="col">Status</th>
                    <th scope="col">Pool</th>
                  </tr>
                </thead>
                <tbody>
                  <tr className="row-selected">
                    <td>hk3-prod</td>
                    <td>New API</td>
                    <td><span className="badge badge-success">healthy</span></td>
                    <td>shared-tiny</td>
                  </tr>
                  <tr>
                    <td>us1-standby</td>
                    <td>OneHub</td>
                    <td><span className="badge badge-warning">idle</span></td>
                    <td>normal</td>
                  </tr>
                  <tr>
                    <td>azure-pg</td>
                    <td>Meta</td>
                    <td><span className="badge badge-error">53300</span></td>
                    <td>1 / 1</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div className="pagination" aria-label="Pagination sample">
              <button type="button" className="pagination-btn" disabled aria-label="Previous page">
                ‹
              </button>
              <button type="button" className="pagination-btn active" aria-current="page">
                1
              </button>
              <button type="button" className="pagination-btn">
                2
              </button>
              <button type="button" className="pagination-btn" aria-label="Next page">
                ›
              </button>
              <span className="pagination-info">1–3 of 103</span>
            </div>
          </Stack>
        </Surface>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-inputs">
        <h2 id="ds-inputs" className="ds-gallery__section-title">Input</h2>
        <Surface variant="solid" padding="md">
          <div className="ds-gallery__form-grid">
            <Input
              label="Application name"
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              hint="Token-backed control radius 10px · 36px height"
            />
            <Input label="Disabled" value="read-only pool" disabled />
            <Input label="Invalid" value="" error="Required for Azure role LIMIT" />
          </div>
        </Surface>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-tokens">
        <h2 id="ds-tokens" className="ds-gallery__section-title">Token swatches</h2>
        <div className="ds-gallery__grid">
          {[
            ['--color-primary', 'var(--color-primary)'],
            ['--color-bg', 'var(--color-bg)'],
            ['--color-bg-card', 'var(--color-bg-card)'],
            ['--color-success', 'var(--color-success)'],
            ['--color-warn', 'var(--color-warn)'],
            ['--color-danger', 'var(--color-danger)'],
          ].map(([name, value]) => (
            <div
              key={name}
              className="ds-gallery__swatch"
              style={{ background: value }}
            >
              <span className="ds-gallery__swatch-name">{name}</span>
              <span className="ds-gallery__swatch-meta">token-only</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
