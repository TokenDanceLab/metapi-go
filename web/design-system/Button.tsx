import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { cx } from './utils.js';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'sm' | 'md';

export type ButtonProps = {
  variant?: ButtonVariant;
  size?: ButtonSize;
  children?: ReactNode;
  className?: string;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'className' | 'children'>;

export function Button({
  variant = 'primary',
  size = 'md',
  type = 'button',
  className,
  children,
  disabled,
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={cx(
        'ds-btn',
        `ds-btn--${variant}`,
        `ds-btn--${size}`,
        className,
      )}
      disabled={disabled}
      aria-disabled={disabled || undefined}
      {...rest}
    >
      {children}
    </button>
  );
}

export default Button;
