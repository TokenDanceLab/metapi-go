import type { InputHTMLAttributes, ReactNode } from 'react';
import { useId } from 'react';
import { cx } from './utils.js';

export type InputProps = {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  className?: string;
  inputClassName?: string;
} & Omit<InputHTMLAttributes<HTMLInputElement>, 'className'>;

export function Input({
  label,
  hint,
  error,
  id,
  className,
  inputClassName,
  disabled,
  ...rest
}: InputProps) {
  const autoId = useId();
  const inputId = id || autoId;
  const describedById = error || hint ? `${inputId}-desc` : undefined;
  const invalid = Boolean(error);

  return (
    <div className={cx('ds-input-field', className)}>
      {label != null && (
        <label className="ds-input-label" htmlFor={inputId}>
          {label}
        </label>
      )}
      <input
        id={inputId}
        className={cx('ds-input', inputClassName)}
        disabled={disabled}
        aria-invalid={invalid || undefined}
        aria-describedby={describedById}
        {...rest}
      />
      {(error || hint) && (
        <p
          id={describedById}
          className={cx('ds-input-hint', invalid && 'ds-input-hint--error')}
        >
          {error || hint}
        </p>
      )}
    </div>
  );
}

export default Input;
