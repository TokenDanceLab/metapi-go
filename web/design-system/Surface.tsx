import type { HTMLAttributes, ReactNode } from 'react';
import { cx } from './utils.js';

export type SurfaceVariant = 'solid' | 'glass' | 'sunken';
export type SurfacePadding = 'none' | 'sm' | 'md' | 'lg';

export type SurfaceProps = {
  variant?: SurfaceVariant;
  padding?: SurfacePadding;
  children?: ReactNode;
  className?: string;
  as?: 'div' | 'section' | 'article' | 'aside' | 'header' | 'footer' | 'main';
} & Omit<HTMLAttributes<HTMLElement>, 'className' | 'children'>;

export function Surface({
  variant = 'solid',
  padding = 'md',
  as: Tag = 'div',
  className,
  children,
  ...rest
}: SurfaceProps) {
  return (
    <Tag
      className={cx(
        'ds-surface',
        `ds-surface--${variant}`,
        `ds-surface--padding-${padding}`,
        className,
      )}
      {...rest}
    >
      {children}
    </Tag>
  );
}

export default Surface;
