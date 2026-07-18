import type { HTMLAttributes, ReactNode } from 'react';
import { cx } from './utils.js';

export type StackGap = 0 | 1 | 2 | 3 | 4 | 5 | 6 | 8;
export type StackAlign = 'start' | 'center' | 'end' | 'stretch' | 'baseline';
export type StackJustify = 'start' | 'center' | 'end' | 'between' | 'around';

export type StackProps = {
  gap?: StackGap;
  align?: StackAlign;
  justify?: StackJustify;
  children?: ReactNode;
  className?: string;
  as?: 'div' | 'section' | 'article' | 'ul' | 'ol' | 'nav' | 'form';
} & Omit<HTMLAttributes<HTMLElement>, 'className' | 'children'>;

export function Stack({
  gap = 3,
  align = 'stretch',
  justify = 'start',
  as: Tag = 'div',
  className,
  children,
  ...rest
}: StackProps) {
  return (
    <Tag
      className={cx(
        'ds-stack',
        `ds-gap-${gap}`,
        `ds-align-${align}`,
        `ds-justify-${justify}`,
        className,
      )}
      {...rest}
    >
      {children}
    </Tag>
  );
}

export default Stack;
