import type { HTMLAttributes, ReactNode } from 'react';
import { cx } from './utils.js';

export type BadgeTone = 'success' | 'warn' | 'danger' | 'info' | 'neutral';

export type BadgeProps = {
  tone?: BadgeTone;
  children?: ReactNode;
  className?: string;
} & Omit<HTMLAttributes<HTMLSpanElement>, 'className' | 'children'>;

export function Badge({
  tone = 'neutral',
  className,
  children,
  ...rest
}: BadgeProps) {
  return (
    <span
      className={cx('ds-badge', `ds-badge--${tone}`, className)}
      {...rest}
    >
      {children}
    </span>
  );
}

export default Badge;
