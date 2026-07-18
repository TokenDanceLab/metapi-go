import type { HTMLAttributes, ReactNode } from 'react';
import { cx } from './utils.js';

export type EmptyStateTone = 'neutral' | 'info' | 'warn' | 'danger';

export type EmptyStateProps = {
  /** Visual tone for icon well / emphasis */
  tone?: EmptyStateTone;
  /** Optional leading icon (emoji, SVG, or glyph) */
  icon?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  /** Single primary action (ops empty/error pattern) */
  action?: ReactNode;
  className?: string;
} & Omit<HTMLAttributes<HTMLDivElement>, 'className' | 'title' | 'children'>;

/**
 * Calm empty / soft-error surface for ops tables and panels (#541).
 * Token-only; no decorative illustration requirement.
 */
export function EmptyState({
  tone = 'neutral',
  icon,
  title,
  description,
  action,
  className,
  ...rest
}: EmptyStateProps) {
  return (
    <div
      className={cx('ds-empty', `ds-empty--${tone}`, className)}
      role={tone === 'danger' ? 'alert' : 'status'}
      {...rest}
    >
      {icon != null && (
        <div className="ds-empty__icon" aria-hidden={typeof icon === 'string' ? true : undefined}>
          {icon}
        </div>
      )}
      <div className="ds-empty__title">{title}</div>
      {description != null && <p className="ds-empty__description">{description}</p>}
      {action != null && <div className="ds-empty__action">{action}</div>}
    </div>
  );
}

export default EmptyState;
