import React, { forwardRef, useEffect, useRef, useCallback } from 'react';
import clsx from 'clsx';
import { twMerge } from 'tailwind-merge';

export interface TextAreaProps
  extends Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, 'size'> {
  /** Field label text */
  label?: string;
  /** Secondary label text (shown on right side) */
  labelAlt?: string;
  /** Helper text displayed below the textarea */
  description?: string;
  /** Secondary helper text (shown on right side below textarea) */
  descriptionAlt?: string;
  /** Error message displayed below the textarea */
  error?: string;
  /** Success message displayed below the textarea */
  success?: string;
  /** Warning message displayed below the textarea */
  warning?: string;
  /** Mark field as required with asterisk */
  required?: boolean;
  /** Container className for the form-control wrapper */
  containerClassName?: string;
  /** Textarea size variant */
  textareaSize?: 'xs' | 'sm' | 'md' | 'lg';
  /** Textarea color variant */
  textareaColor?:
    | 'primary'
    | 'secondary'
    | 'accent'
    | 'info'
    | 'success'
    | 'warning'
    | 'error';
  /** Show bordered style */
  bordered?: boolean;
  /** Show ghost style */
  ghost?: boolean;
  /** Enable auto-resize based on content */
  autoResize?: boolean;
  /** Minimum number of rows when auto-resize is enabled */
  minRows?: number;
  /** Maximum number of rows when auto-resize is enabled */
  maxRows?: number;
  /** Show character counter */
  showCharCount?: boolean;
}

/**
 * TextArea Molecule
 *
 * A reusable textarea component that follows DaisyUI patterns.
 * Provides consistent styling for labels, textareas, descriptions, and validation states.
 * Supports auto-resize and character counting.
 *
 * @example
 * ```tsx
 * <TextArea
 *   label="Description"
 *   placeholder="Enter your description"
 *   description="Maximum 500 characters"
 *   maxLength={500}
 *   showCharCount
 *   autoResize
 * />
 * ```
 */
export const TextArea = forwardRef<HTMLTextAreaElement, TextAreaProps>(
  (
    {
      label,
      labelAlt,
      description,
      descriptionAlt,
      error,
      success,
      warning,
      required,
      containerClassName,
      textareaSize,
      textareaColor,
      bordered = true,
      ghost = false,
      disabled,
      readOnly,
      className,
      autoResize = false,
      minRows = 2,
      maxRows = 10,
      showCharCount = false,
      maxLength,
      value,
      defaultValue,
      onChange,
      rows,
      ...textareaProps
    },
    ref
  ) => {
    const internalRef = useRef<HTMLTextAreaElement>(null);
    const textareaRef =
      (ref as React.RefObject<HTMLTextAreaElement>) || internalRef;

    // Determine validation state
    const hasError = !!error;
    const hasSuccess = !!success;
    const hasWarning = !!warning;

    // Calculate character count
    const currentLength =
      typeof value === 'string'
        ? value.length
        : typeof defaultValue === 'string'
        ? defaultValue.length
        : 0;

    // Auto-resize logic
    const adjustHeight = useCallback(() => {
      const textarea = textareaRef.current;
      if (!textarea || !autoResize) return;

      // Reset height to calculate scrollHeight
      textarea.style.height = 'auto';

      // Calculate line height (approximate)
      const lineHeight = parseInt(getComputedStyle(textarea).lineHeight) || 24;
      const minHeight = lineHeight * minRows;
      const maxHeight = lineHeight * maxRows;

      // Set new height within bounds
      const newHeight = Math.min(
        Math.max(textarea.scrollHeight, minHeight),
        maxHeight
      );
      textarea.style.height = `${newHeight}px`;
    }, [autoResize, minRows, maxRows, textareaRef]);

    // Adjust height on mount and value change
    useEffect(() => {
      adjustHeight();
    }, [value, adjustHeight]);

    // Handle change with auto-resize
    const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      if (autoResize) {
        adjustHeight();
      }
      onChange?.(e);
    };

    // Build textarea classes using DaisyUI pattern
    const textareaClasses = twMerge(
      'textarea',
      'w-full',
      className,
      clsx({
        'textarea-bordered': bordered,
        'textarea-ghost': ghost,
        'textarea-xs': textareaSize === 'xs',
        'textarea-sm': textareaSize === 'sm',
        'textarea-md': textareaSize === 'md',
        'textarea-lg': textareaSize === 'lg',
        'textarea-primary': textareaColor === 'primary',
        'textarea-secondary': textareaColor === 'secondary',
        'textarea-accent': textareaColor === 'accent',
        'textarea-info': textareaColor === 'info',
        'textarea-success':
          textareaColor === 'success' || (hasSuccess && !textareaColor),
        'textarea-warning':
          textareaColor === 'warning' || (hasWarning && !textareaColor),
        'textarea-error':
          textareaColor === 'error' || (hasError && !textareaColor),
        'bg-base-200/50 cursor-not-allowed': disabled || readOnly,
        'resize-none': autoResize,
      })
    );

    // Helper text content and styling
    const primaryHelper = error || success || warning || description;

    // Character count display
    const charCountDisplay =
      showCharCount && maxLength
        ? `${currentLength}/${maxLength}`
        : showCharCount
        ? `${currentLength} characters`
        : descriptionAlt;

    const primaryHelperClass = clsx(
      'label-text-alt',
      hasError && 'text-error',
      hasSuccess && 'text-success',
      hasWarning && 'text-warning',
      !hasError && !hasSuccess && !hasWarning && 'text-base-content/60'
    );

    return (
      <div className={twMerge('form-control', containerClassName)}>
        {/* Top label row - follows DaisyUI pattern */}
        {(label || labelAlt) && (
          <label className="label">
            {label && (
              <span className="label-text font-medium">
                {label}
                {required && <span className="text-error ml-1">*</span>}
              </span>
            )}
            {labelAlt && <span className="label-text-alt">{labelAlt}</span>}
          </label>
        )}

        {/* Textarea element */}
        <textarea
          ref={textareaRef}
          className={textareaClasses}
          disabled={disabled}
          readOnly={readOnly}
          aria-invalid={hasError}
          aria-describedby={
            primaryHelper ? `${textareaProps.id}-helper` : undefined
          }
          rows={autoResize ? minRows : rows}
          maxLength={maxLength}
          value={value}
          defaultValue={defaultValue}
          onChange={handleChange}
          {...textareaProps}
        />

        {/* Bottom helper text row - follows DaisyUI pattern */}
        {(primaryHelper || charCountDisplay) && (
          <label className="label">
            {primaryHelper && (
              <span
                id={textareaProps.id ? `${textareaProps.id}-helper` : undefined}
                className={primaryHelperClass}
              >
                {primaryHelper}
              </span>
            )}
            {charCountDisplay && (
              <span className="label-text-alt text-base-content/60">
                {charCountDisplay}
              </span>
            )}
          </label>
        )}
      </div>
    );
  }
);

TextArea.displayName = 'TextArea';

export default TextArea;
