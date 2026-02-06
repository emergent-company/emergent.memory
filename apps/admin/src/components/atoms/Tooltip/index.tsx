import React, {
  type ReactNode,
  type CSSProperties,
  useRef,
  useId,
  useCallback,
} from 'react';

export type TooltipPlacement = 'top' | 'bottom' | 'left' | 'right';
export type TooltipAlign = 'start' | 'center' | 'end';
export type TooltipColor =
  | 'neutral'
  | 'primary'
  | 'secondary'
  | 'accent'
  | 'info'
  | 'success'
  | 'warning'
  | 'error';

export interface TooltipProps {
  content: ReactNode;
  placement?: TooltipPlacement;
  align?: TooltipAlign;
  color?: TooltipColor;
  className?: string;
  children: ReactNode;
}

// Color mappings for tooltip backgrounds
const colorClasses: Record<TooltipColor, string> = {
  neutral: 'bg-neutral text-neutral-content',
  primary: 'bg-primary text-primary-content',
  secondary: 'bg-secondary text-secondary-content',
  accent: 'bg-accent text-accent-content',
  info: 'bg-info text-info-content',
  success: 'bg-success text-success-content',
  warning: 'bg-warning text-warning-content',
  error: 'bg-error text-error-content',
};

// Arrow color mappings (border color for the triangle)
const arrowColorClasses: Record<TooltipColor, string> = {
  neutral: 'border-t-neutral',
  primary: 'border-t-primary',
  secondary: 'border-t-secondary',
  accent: 'border-t-accent',
  info: 'border-t-info',
  success: 'border-t-success',
  warning: 'border-t-warning',
  error: 'border-t-error',
};

export const Tooltip: React.FC<TooltipProps> = ({
  content,
  placement = 'top',
  align = 'center',
  color = 'neutral',
  className,
  children,
}) => {
  const id = useId();
  const anchorName = `--tooltip-anchor${id.replace(/:/g, '-')}`;
  const popoverRef = useRef<HTMLDivElement>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showTooltip = useCallback(() => {
    timerRef.current = setTimeout(() => {
      popoverRef.current?.showPopover();
    }, 200);
  }, []);

  const hideTooltip = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    popoverRef.current?.hidePopover();
  }, []);

  // Calculate position styles based on placement and alignment
  const getPositionStyles = (): CSSProperties => {
    const gap = '8px';

    // Base anchor positioning
    const styles: CSSProperties = {
      position: 'fixed',
      positionAnchor: anchorName,
    } as CSSProperties;

    // Position based on placement
    switch (placement) {
      case 'top':
        Object.assign(styles, {
          bottom: `anchor(top)`,
          marginBottom: gap,
        });
        break;
      case 'bottom':
        Object.assign(styles, {
          top: `anchor(bottom)`,
          marginTop: gap,
        });
        break;
      case 'left':
        Object.assign(styles, {
          right: `anchor(left)`,
          marginRight: gap,
        });
        break;
      case 'right':
        Object.assign(styles, {
          left: `anchor(right)`,
          marginLeft: gap,
        });
        break;
    }

    // Alignment for top/bottom placements (horizontal alignment)
    if (placement === 'top' || placement === 'bottom') {
      switch (align) {
        case 'start':
          Object.assign(styles, { left: `anchor(left)` });
          break;
        case 'center':
          Object.assign(styles, {
            left: `anchor(center)`,
            transform: 'translateX(-50%)',
          });
          break;
        case 'end':
          Object.assign(styles, { right: `anchor(right)` });
          break;
      }
    }

    // Alignment for left/right placements (vertical alignment)
    if (placement === 'left' || placement === 'right') {
      switch (align) {
        case 'start':
          Object.assign(styles, { top: `anchor(top)` });
          break;
        case 'center':
          Object.assign(styles, {
            top: `anchor(center)`,
            transform: 'translateY(-50%)',
          });
          break;
        case 'end':
          Object.assign(styles, { bottom: `anchor(bottom)` });
          break;
      }
    }

    return styles;
  };

  // Arrow styles based on placement
  const getArrowStyles = (): CSSProperties => {
    const arrowSize = '6px';
    const base: CSSProperties = {
      position: 'absolute',
      width: 0,
      height: 0,
      borderLeft: `${arrowSize} solid transparent`,
      borderRight: `${arrowSize} solid transparent`,
      borderTop: `${arrowSize} solid`, // Color set via class
    };

    switch (placement) {
      case 'top':
        return {
          ...base,
          bottom: `-${arrowSize}`,
          left: '50%',
          transform: 'translateX(-50%)',
        };
      case 'bottom':
        return {
          ...base,
          top: `-${arrowSize}`,
          left: '50%',
          transform: 'translateX(-50%) rotate(180deg)',
        };
      case 'left':
        return {
          ...base,
          right: `-${arrowSize}`,
          top: '50%',
          transform: 'translateY(-50%) rotate(90deg)',
        };
      case 'right':
        return {
          ...base,
          left: `-${arrowSize}`,
          top: '50%',
          transform: 'translateY(-50%) rotate(-90deg)',
        };
    }
  };

  const bgColorClass = colorClasses[color];
  const arrowColorClass = arrowColorClasses[color];

  return (
    <>
      {/* Anchor wrapper */}
      <span
        style={{ anchorName } as CSSProperties}
        className={`inline-block ${className || ''}`}
        onMouseEnter={showTooltip}
        onMouseLeave={hideTooltip}
        onFocus={showTooltip}
        onBlur={hideTooltip}
      >
        {children}
      </span>

      {/* Tooltip popover - renders in top layer */}
      <div
        ref={popoverRef}
        popover="hint"
        role="tooltip"
        className={`rounded-md px-4 py-3 text-sm shadow-lg ${bgColorClass} z-[9999] w-auto max-w-xs`}
        style={getPositionStyles()}
      >
        {content}
        {/* Arrow */}
        <span className={arrowColorClass} style={getArrowStyles()} />
      </div>
    </>
  );
};

export default Tooltip;
