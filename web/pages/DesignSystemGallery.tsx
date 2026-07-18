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
type ShellPage = 'dashboard' | 'sites' | 'settings';

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

function ShellNavIcon({ paths }: { paths: string[] }) {
  return (
    <svg className="sidebar-item-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
      {paths.map((d) => (
        <path key={d} strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.75} d={d} />
      ))}
    </svg>
  );
}

/**
 * DEV-only shell chrome mock for interim visual acceptance without backend auth (#538).
 * Reuses production topbar/sidebar/page-header/data-table classes so glass + data
 * surfaces can be scored when live Dashboard/Sites/Settings need credentials.
 */
function ShellChromeMock({ activePage, onPageChange }: {
  activePage: ShellPage;
  onPageChange: (page: ShellPage) => void;
}) {
  const navItems: Array<{ id: ShellPage; label: string; paths: string[] }> = [
    {
      id: 'dashboard',
      label: '仪表盘',
      paths: [
        'M4 5a1 1 0 011-1h4a1 1 0 011 1v5a1 1 0 01-1 1H5a1 1 0 01-1-1V5zM14 5a1 1 0 011-1h4a1 1 0 011 1v2a1 1 0 01-1 1h-4a1 1 0 01-1-1V5zM4 15a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1H5a1 1 0 01-1-1v-4zM14 12a1 1 0 011-1h4a1 1 0 011 1v7a1 1 0 01-1 1h-4a1 1 0 01-1-1v-7z',
      ],
    },
    {
      id: 'sites',
      label: '站点管理',
      paths: [
        'M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9',
      ],
    },
    {
      id: 'settings',
      label: '设置',
      paths: [
        'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z',
        'M15 12a3 3 0 11-6 0 3 3 0 016 0z',
      ],
    },
  ];

  return (
    <div
      className="ds-gallery__shell-frame"
      data-testid="shell-chrome-mock"
      data-shell-page={activePage}
    >
      <header className="topbar" aria-label="Shell mock topbar">
        <div className="topbar-logo">
          <span className="topbar-logo-icon" aria-hidden="true">M</span>
          <span className="topbar-logo-text">Metapi</span>
        </div>
        <nav className="topbar-nav" aria-label="Shell mock top nav">
          <span className="topbar-nav-item active">控制台</span>
          <span className="topbar-nav-item">模型广场</span>
          <span className="topbar-nav-item">关于</span>
        </nav>
        <div className="topbar-right">
          <button type="button" className="topbar-search-trigger" tabIndex={-1}>
            <svg width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <span className="topbar-search-label">搜索</span>
            <kbd className="topbar-search-kbd">Ctrl K</kbd>
          </button>
          <button type="button" className="topbar-icon-btn" aria-label="Notifications mock" tabIndex={-1}>
            <svg width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
            </svg>
            <span className="topbar-badge">3</span>
          </button>
          <span className="topbar-avatar" aria-hidden="true">管</span>
        </div>
      </header>

      <div className="app-layout">
        <aside className="sidebar" aria-label="Shell mock sidebar">
          <div className="sidebar-group">
            <div className="sidebar-group-label">控制台</div>
            {navItems.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`sidebar-item ${activePage === item.id ? 'active' : ''}`}
                onClick={() => onPageChange(item.id)}
                aria-current={activePage === item.id ? 'page' : undefined}
              >
                <ShellNavIcon paths={item.paths} />
                <span>{item.label}</span>
              </button>
            ))}
          </div>
          <div className="sidebar-group">
            <div className="sidebar-group-label">系统</div>
            <span className="sidebar-item">
              <ShellNavIcon paths={[
                'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
              ]} />
              <span>程序日志</span>
            </span>
          </div>
        </aside>

        <main className="main-content">
          {activePage === 'dashboard' && (
            <div data-testid="shell-mock-dashboard">
              <div className="page-header">
                <div>
                  <h1 className="page-title greeting">☀️ 早上好，管理员</h1>
                  <p className="page-subtitle">Mock shell · no API · #538 interim acceptance</p>
                </div>
                <div className="page-actions">
                  <Button size="sm" variant="secondary">刷新</Button>
                  <Button size="sm" variant="primary">新建站点</Button>
                </div>
              </div>
              <div className="ds-gallery__kpi-grid" style={{ marginBottom: 16 }}>
                {[
                  { label: 'Requests / min', value: '12.4k', delta: '+6.2%', tone: 'success' as const },
                  { label: 'Error rate', value: '0.18%', delta: '−0.04%', tone: 'success' as const },
                  { label: 'Active sites', value: '103', delta: '+2', tone: 'info' as const },
                  { label: 'Pool pressure', value: '1 / 1', delta: 'shared-tiny', tone: 'warn' as const },
                ].map((kpi) => (
                  <article key={kpi.label} className="ds-gallery__kpi">
                    <div className="ds-gallery__kpi-label">{kpi.label}</div>
                    <div className="ds-gallery__kpi-value">{kpi.value}</div>
                    <Badge tone={kpi.tone}>{kpi.delta}</Badge>
                  </article>
                ))}
              </div>
              <Card title="Traffic trend" description="Placeholder chart well for shell composition.">
                <div className="ds-gallery__fake-table" aria-hidden="true">
                  <div /><div /><div />
                  <div /><div /><div />
                  <div /><div /><div />
                </div>
              </Card>
            </div>
          )}

          {activePage === 'sites' && (
            <div data-testid="shell-mock-sites">
              <div className="page-header">
                <div>
                  <h1 className="page-title">站点管理</h1>
                  <p className="page-subtitle">Glass chrome + Phase 3 data surfaces (mock)</p>
                </div>
                <div className="page-actions sites-page-actions">
                  <Button size="sm" variant="secondary">导入</Button>
                  <Button size="sm" variant="primary">添加站点</Button>
                </div>
              </div>
              <Surface variant="solid" padding="md">
                <Stack gap={4}>
                  <div className="ds-gallery__filter-row" role="group" aria-label="Shell mock filters">
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
                    <button type="button" className="pagination-btn" disabled aria-label="Previous page">‹</button>
                    <button type="button" className="pagination-btn active" aria-current="page">1</button>
                    <button type="button" className="pagination-btn">2</button>
                    <button type="button" className="pagination-btn" aria-label="Next page">›</button>
                    <span className="pagination-info">1–3 of 103</span>
                  </div>
                </Stack>
              </Surface>
            </div>
          )}

          {activePage === 'settings' && (
            <div data-testid="shell-mock-settings">
              <div className="page-header">
                <div>
                  <h1 className="page-title">设置</h1>
                  <p className="page-subtitle">Runtime controls · mock form density</p>
                </div>
                <div className="page-actions">
                  <Button size="sm" variant="primary">保存</Button>
                </div>
              </div>
              <div className="ds-gallery__grid">
                <Card title="签到调度" description="Cron / interval presets for acceptance density.">
                  <Stack gap={3}>
                    <Input label="Check-in cron" value="0 */6 * * *" readOnly />
                    <Input label="Balance refresh" value="15 */2 * * *" readOnly />
                    <Inline gap={2}>
                      <Badge tone="info">cron</Badge>
                      <Badge tone="neutral">shared-tiny</Badge>
                    </Inline>
                  </Stack>
                </Card>
                <Card title="路由冷却" description="Failure cooldown unit × max value.">
                  <Stack gap={3}>
                    <Input label="Max cooldown" value="30" readOnly />
                    <Input label="Unit" value="minute" readOnly />
                    <Inline gap={2}>
                      <Badge tone="warn">53300 aware</Badge>
                      <Badge tone="success">fail-open</Badge>
                    </Inline>
                  </Stack>
                </Card>
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}

/**
 * Dev/visual acceptance gallery for design-system primitives (#533).
 * Routed at /__design__ behind DEV or localStorage.metapi_design_gallery === '1'.
 * Shell chrome mock (#538) provides CI-friendly Dashboard/Sites/Settings composition
 * without backend auth; capture-ui-shots.mjs can also shoot real routes when token set.
 */
export default function DesignSystemGallery() {
  const [theme, setTheme] = useState<ThemeChoice>(() => readDocumentTheme());
  const [inputValue, setInputValue] = useState('metapi-design');
  const [shellPage, setShellPage] = useState<ShellPage>('sites');

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

      <section className="ds-gallery__section" aria-labelledby="ds-shell">
        <h2 id="ds-shell" className="ds-gallery__section-title">Shell chrome mock</h2>
        <p className="ds-gallery__swatch-meta" style={{ marginBottom: 12 }}>
          Interim Dashboard / Sites / Settings composition for human score without live auth.
          Switch sidebar items; capture via <code>data-testid=&quot;shell-chrome-mock&quot;</code>.
        </p>
        <div style={{ marginBottom: 12 }}>
          <Inline gap={2}>
            <Button
              size="sm"
              variant={shellPage === 'dashboard' ? 'primary' : 'secondary'}
              onClick={() => setShellPage('dashboard')}
              aria-pressed={shellPage === 'dashboard'}
            >
              Dashboard
            </Button>
            <Button
              size="sm"
              variant={shellPage === 'sites' ? 'primary' : 'secondary'}
              onClick={() => setShellPage('sites')}
              aria-pressed={shellPage === 'sites'}
            >
              Sites
            </Button>
            <Button
              size="sm"
              variant={shellPage === 'settings' ? 'primary' : 'secondary'}
              onClick={() => setShellPage('settings')}
              aria-pressed={shellPage === 'settings'}
            >
              Settings
            </Button>
          </Inline>
        </div>
        <ShellChromeMock activePage={shellPage} onPageChange={setShellPage} />
      </section>

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

      <section className="ds-gallery__section" aria-labelledby="ds-form-states">
        <h2 id="ds-form-states" className="ds-gallery__section-title">Form states · Phase 4</h2>
        <div className="ds-gallery__grid">
          <Surface variant="solid" padding="md">
            <Stack gap={4}>
              <div className="ds-gallery__form-states">
                <div className="form-field">
                  <label className="form-label" htmlFor="gallery-form-focus">
                    Focus / default
                  </label>
                  <input
                    id="gallery-form-focus"
                    className="form-input"
                    defaultValue="ops-comfortable · 36px"
                    aria-describedby="gallery-form-focus-hint"
                  />
                  <p id="gallery-form-focus-hint" className="form-hint">
                    Uses --radius-control + --color-focus-ring-strong
                  </p>
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="gallery-form-error">
                    Error
                  </label>
                  <input
                    id="gallery-form-error"
                    className="form-input is-invalid"
                    defaultValue=""
                    placeholder="Missing site endpoint"
                    aria-invalid="true"
                    aria-describedby="gallery-form-error-hint"
                  />
                  <p id="gallery-form-error-hint" className="form-hint--error">
                    Endpoint is required for route binding
                  </p>
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="gallery-form-disabled">
                    Disabled
                  </label>
                  <input
                    id="gallery-form-disabled"
                    className="form-input"
                    value="shared-tiny pool"
                    disabled
                  />
                </div>
                <div className="form-field">
                  <label className="form-label" htmlFor="gallery-form-notes">
                    Textarea
                  </label>
                  <textarea
                    id="gallery-form-notes"
                    className="form-input"
                    rows={3}
                    defaultValue="Ops notes stay readable at comfortable density."
                  />
                </div>
              </div>

              <div className="form-section" aria-label="Settings group sample">
                <div className="form-section-header">
                  <h3 className="form-section-title">Connection defaults</h3>
                  <p className="form-section-desc">
                    Apple-style group list · ops-comfortable spacing · no compact mode
                  </p>
                </div>
                <div className="form-group-list__body" style={{ paddingTop: 0, paddingBottom: 0, gap: 0 }}>
                  <div className="form-group-list__row">
                    <div className="form-group-list__row-copy">
                      <div className="form-group-list__row-title">Default pool</div>
                      <div className="form-group-list__row-meta">shared-tiny · lease-aware</div>
                    </div>
                    <div className="form-group-list__row-control">
                      <select className="form-input" defaultValue="shared-tiny" aria-label="Default pool">
                        <option value="shared-tiny">shared-tiny</option>
                        <option value="shared-std">shared-std</option>
                      </select>
                    </div>
                  </div>
                  <div className="form-group-list__row">
                    <div className="form-group-list__row-copy">
                      <div className="form-group-list__row-title">Fail-closed on pressure</div>
                      <div className="form-group-list__row-meta">Optional guard for 53300 storms</div>
                    </div>
                    <div className="form-group-list__row-control">
                      <input className="form-input" defaultValue="enabled" readOnly aria-label="Fail-closed" />
                    </div>
                  </div>
                </div>
              </div>
            </Stack>
          </Surface>

          <Surface variant="solid" padding="md">
            <Stack gap={4}>
              <div>
                <h3 className="ds-gallery__section-title" style={{ marginBottom: 'var(--space-2)' }}>
                  Drawer surface
                </h3>
                <p className="form-hint" style={{ marginTop: 0 }}>
                  Glass panel + elevated solid fallbacks for reduced transparency
                </p>
              </div>
              <div className="ds-gallery__drawer-mock" aria-hidden="true">
                <div className="ds-gallery__drawer-mock-scrim" />
                <div className="ds-gallery__drawer-mock-panel drawer-surface">
                  <div>
                    <p className="ds-gallery__drawer-mock-title">Downstream key</p>
                    <p className="ds-gallery__drawer-mock-meta">sk-**** · main group</p>
                  </div>
                  <div className="form-field">
                    <label className="form-label">Display name</label>
                    <input className="form-input" defaultValue="prod-edge-01" readOnly />
                  </div>
                  <div className="form-field">
                    <label className="form-label">Notes</label>
                    <textarea className="form-input" rows={3} defaultValue="Glass drawer chrome · token-only" readOnly />
                  </div>
                </div>
              </div>

              <div>
                <h3 className="ds-gallery__section-title" style={{ marginBottom: 'var(--space-2)' }}>
                  Modal surface
                </h3>
              </div>
              <div className="ds-gallery__modal-mock" aria-hidden="true">
                <div className="ds-gallery__modal-mock-card">
                  <div className="form-stack">
                    <div>
                      <div className="modal-title" style={{ fontSize: 'var(--text-lg)' }}>
                        Rotate token
                      </div>
                      <p className="form-hint">Modal content uses glass-strong + focus-safe controls</p>
                    </div>
                    <div className="form-field">
                      <label className="form-label">New token</label>
                      <input className="form-input" defaultValue="••••••••••••" readOnly />
                    </div>
                    <div className="form-field">
                      <label className="form-label">Confirm</label>
                      <input className="form-input is-invalid" defaultValue="" placeholder="Repeat token" aria-invalid="true" readOnly />
                      <p className="form-hint--error">Tokens must match</p>
                    </div>
                    <Inline gap={2} justify="end">
                      <Button variant="secondary" size="sm">
                        Cancel
                      </Button>
                      <Button variant="primary" size="sm">
                        Save
                      </Button>
                    </Inline>
                  </div>
                </div>
              </div>
            </Stack>
          </Surface>
        </div>
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
