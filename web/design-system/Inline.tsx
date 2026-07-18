import type { HTMLAttributes, ReactNode } from 'react';
import { cx } from './utils.js';

export type InlineGap = 0 | 1 | 2 | 3 | 4 | 5 | 6 | 8;
export type InlineAlign = 'start' | 'center' | 'end' | 'stretch' | 'baseline';
export type InlineJustify = 'start' | 'center' | 'end' | 'between' | 'around';

export type InlineProps = {
  gap?: InlineGap;
  align?: InlineAlign;
  justify?: InlineJustify;
  wrap?: boolean;
  children?: ReactNode;
  className?: string;
  as?: 'div' | 'span' | 'section' | 'nav' | 'ul' | 'ol';
} & Omit<HTMLAttributes<HTMLElement>, 'className' | 'children'>;

export function Inline({
  gap = 2,
  align = 'center',
  justify = 'start',
  wrap = true,
  as: Tag = 'div',
  className,
  children,
  ...rest
}: InlineProps) {
  return (
    <Tag
      className={cx(
        'ds-inline',
        `ds-gap-${gap}`,
        `ds-align-${align}`,
        `ds-justify-${justify}`,
        wrap ? 'ds-wrap' : 'ds-nowrap',
        className,
      )}
      {...rest}
    >
      {children}
    </Tag>
  );
}

export default Inline;
