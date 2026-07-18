import type { HTMLAttributes, ReactNode } from 'react';
import { cx } from './utils.js';

export type CardProps = {
  title?: ReactNode;
  description?: ReactNode;
  footer?: ReactNode;
  glass?: boolean;
  children?: ReactNode;
  className?: string;
} & Omit<HTMLAttributes<HTMLDivElement>, 'className' | 'children' | 'title'>;

export function Card({
  title,
  description,
  footer,
  glass = false,
  className,
  children,
  ...rest
}: CardProps) {
  const showHeader = title != null || description != null;

  return (
    <div
      className={cx('ds-card', glass && 'ds-card--glass', className)}
      {...rest}
    >
      {showHeader && (
        <div className="ds-card__header">
          {title != null && <h3 className="ds-card__title">{title}</h3>}
          {description != null && (
            <p className="ds-card__description">{description}</p>
          )}
        </div>
      )}
      {children != null && <div className="ds-card__body">{children}</div>}
      {footer != null && <div className="ds-card__footer">{footer}</div>}
    </div>
  );
}

export default Card;
