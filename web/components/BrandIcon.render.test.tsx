import { describe, expect, it } from 'vitest';
import { create } from 'react-test-renderer';
import { BrandIcon } from './BrandIcon.js';

describe('BrandIcon rendering', () => {
  it('does not force a colored background shell for recognized brands', () => {
    const root = create(<BrandIcon model="nvidia/vila" size={44} />);
    const wrappers = root.root.findAll((node) => node.type === 'div');
    const brandedWrapper = wrappers.find((node) => node.props.style?.width === 44 && node.props.style?.height === 44);

    expect(brandedWrapper).toBeDefined();
    expect(brandedWrapper?.props.style?.background).not.toBe('linear-gradient(135deg, #76b900, #4a8c0b)');
  });
});
