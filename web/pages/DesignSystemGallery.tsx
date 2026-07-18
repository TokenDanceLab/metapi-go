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

  return (
    <div className="ds-gallery" data-testid="design-system-gallery">
      <header className="ds-gallery__header">
        <div>
          <h1 className="ds-gallery__title">MetAPI Design System</h1>
          <p className="ds-gallery__subtitle">
            Primitive inventory for visual acceptance. Classes use <code>ds-</code> prefix and
            token vars <code>var(--color-*)</code>.
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
              <p className="ds-gallery__swatch-meta" style={{ marginBottom: 8 }}>size=sm</p>
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
        <h2 id="ds-surfaces" className="ds-gallery__section-title">Surface</h2>
        <div className="ds-gallery__grid">
          {sampleSurfaces.map((item) => (
            <Surface key={item.variant} variant={item.variant} padding="md">
              <Stack gap={1}>
                <strong>{item.label}</strong>
                <span className="ds-gallery__swatch-meta">variant=&quot;{item.variant}&quot;</span>
                <span>Token-backed surface for panels and chrome.</span>
              </Stack>
            </Surface>
          ))}
        </div>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-cards">
        <h2 id="ds-cards" className="ds-gallery__section-title">Card</h2>
        <div className="ds-gallery__grid">
          <Card
            title="Solid card"
            description="Default elevated content container."
            footer={(
              <Inline gap={2} justify="end">
                <Button variant="ghost" size="sm">Cancel</Button>
                <Button variant="primary" size="sm">Save</Button>
              </Inline>
            )}
          >
            Body uses design tokens for text, border, and shadow.
          </Card>
          <Card
            glass
            title="Glass card"
            description="Optional glass material for sticky chrome."
            footer={<Badge tone="info">glass</Badge>}
          >
            Falls back to solid when reduced-transparency is preferred.
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
          </Inline>
        </Surface>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-inputs">
        <h2 id="ds-inputs" className="ds-gallery__section-title">Input</h2>
        <Surface variant="solid" padding="md">
          <div className="ds-gallery__grid">
            <Input
              label="Default"
              placeholder="Type a value"
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              hint="Uses --color-border / --color-text-*"
            />
            <Input
              label="With error"
              defaultValue="invalid"
              error="This field is required"
            />
            <Input
              label="Disabled"
              defaultValue="read-only sample"
              disabled
            />
          </div>
        </Surface>
      </section>

      <section className="ds-gallery__section" aria-labelledby="ds-layout">
        <h2 id="ds-layout" className="ds-gallery__section-title">Stack / Inline</h2>
        <div className="ds-gallery__grid">
          <Surface variant="sunken" padding="md">
            <Stack gap={2}>
              <strong>Stack (column)</strong>
              <Badge tone="info">gap=2</Badge>
              <Badge tone="neutral">align=stretch</Badge>
              <Badge tone="success">justify=start</Badge>
            </Stack>
          </Surface>
          <Surface variant="sunken" padding="md">
            <Stack gap={2}>
              <strong>Inline (row)</strong>
              <Inline gap={2} wrap>
                <Badge tone="success">one</Badge>
                <Badge tone="warn">two</Badge>
                <Badge tone="danger">three</Badge>
                <Badge tone="info">four</Badge>
              </Inline>
            </Stack>
          </Surface>
        </div>
      </section>
    </div>
  );
}
