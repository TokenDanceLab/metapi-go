import { describe, expect, it, vi } from 'vitest';
import { act, create } from 'react-test-renderer';
import { Button } from './Button.js';

describe('Button', () => {
  it('renders primary medium defaults with ds- classes', () => {
    const root = create(<Button>Save</Button>);
    const btn = root.root.findByType('button');

    expect(btn.props.type).toBe('button');
    expect(btn.props.className).toContain('ds-btn');
    expect(btn.props.className).toContain('ds-btn--primary');
    expect(btn.props.className).toContain('ds-btn--md');
    expect(btn.children).toContain('Save');
  });

  it('applies variant and size modifiers', () => {
    const root = create(
      <Button variant="danger" size="sm">
        Delete
      </Button>,
    );
    const btn = root.root.findByType('button');
    expect(btn.props.className).toContain('ds-btn--danger');
    expect(btn.props.className).toContain('ds-btn--sm');
  });

  it('forwards click handlers and disabled state', () => {
    const onClick = vi.fn();
    let root = create(
      <Button onClick={onClick}>
        Click me
      </Button>,
    );

    act(() => {
      root.root.findByType('button').props.onClick({ preventDefault() {} });
    });
    expect(onClick).toHaveBeenCalledTimes(1);

    act(() => {
      root.update(
        <Button onClick={onClick} disabled>
          Click me
        </Button>,
      );
    });

    const disabledBtn = root.root.findByType('button');
    expect(disabledBtn.props.disabled).toBe(true);
    expect(disabledBtn.props['aria-disabled']).toBe(true);
  });
});
