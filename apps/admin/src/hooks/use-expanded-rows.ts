import { useState, useCallback, useMemo } from 'react';

export interface UseExpandedRowsOptions<T extends string | number = string> {
  /** Initial expanded rows */
  initialExpanded?: T[];
  /** Allow multiple rows to be expanded at once (default: true) */
  allowMultiple?: boolean;
  /** Callback when expanded rows change */
  onExpandChange?: (expanded: T[]) => void;
}

export interface UseExpandedRowsReturn<T extends string | number = string> {
  /** Set of currently expanded row IDs */
  expandedRows: Set<T>;
  /** Array of currently expanded row IDs */
  expandedArray: T[];
  /** Check if a specific row is expanded */
  isExpanded: (id: T) => boolean;
  /** Toggle a row's expanded state */
  toggle: (id: T) => void;
  /** Expand a specific row */
  expand: (id: T) => void;
  /** Collapse a specific row */
  collapse: (id: T) => void;
  /** Expand all rows (requires array of all IDs) */
  expandAll: (allIds: T[]) => void;
  /** Collapse all rows */
  collapseAll: () => void;
  /** Set expanded rows directly */
  setExpanded: (ids: T[]) => void;
  /** Get the count of expanded rows */
  count: number;
}

/**
 * Hook for managing expandable table row state
 *
 * Supports both single and multiple expanded rows, with string or numeric IDs.
 * Provides a consistent API for toggle, expand, collapse operations.
 *
 * @example
 * ```tsx
 * // Multiple expanded rows with string IDs
 * const { isExpanded, toggle } = useExpandedRows<string>();
 *
 * // Single expanded row (accordion-style)
 * const { isExpanded, toggle } = useExpandedRows<number>({
 *   allowMultiple: false
 * });
 *
 * // With initial state
 * const { isExpanded, toggle } = useExpandedRows({
 *   initialExpanded: ['row-1', 'row-2']
 * });
 * ```
 */
export function useExpandedRows<T extends string | number = string>(
  options: UseExpandedRowsOptions<T> = {}
): UseExpandedRowsReturn<T> {
  const {
    initialExpanded = [],
    allowMultiple = true,
    onExpandChange,
  } = options;

  const [expandedRows, setExpandedRows] = useState<Set<T>>(
    () => new Set(initialExpanded)
  );

  // Convert Set to Array for convenience
  const expandedArray = useMemo(() => Array.from(expandedRows), [expandedRows]);

  // Update state and notify
  const updateExpanded = useCallback(
    (newSet: Set<T>) => {
      setExpandedRows(newSet);
      onExpandChange?.(Array.from(newSet));
    },
    [onExpandChange]
  );

  // Check if a row is expanded
  const isExpanded = useCallback(
    (id: T): boolean => expandedRows.has(id),
    [expandedRows]
  );

  // Toggle a row's expanded state
  const toggle = useCallback(
    (id: T) => {
      const newSet = new Set(expandedRows);
      if (newSet.has(id)) {
        newSet.delete(id);
      } else {
        if (!allowMultiple) {
          newSet.clear();
        }
        newSet.add(id);
      }
      updateExpanded(newSet);
    },
    [expandedRows, allowMultiple, updateExpanded]
  );

  // Expand a specific row
  const expand = useCallback(
    (id: T) => {
      if (expandedRows.has(id)) return;

      const newSet = allowMultiple ? new Set(expandedRows) : new Set<T>();
      newSet.add(id);
      updateExpanded(newSet);
    },
    [expandedRows, allowMultiple, updateExpanded]
  );

  // Collapse a specific row
  const collapse = useCallback(
    (id: T) => {
      if (!expandedRows.has(id)) return;

      const newSet = new Set(expandedRows);
      newSet.delete(id);
      updateExpanded(newSet);
    },
    [expandedRows, updateExpanded]
  );

  // Expand all rows
  const expandAll = useCallback(
    (allIds: T[]) => {
      if (!allowMultiple && allIds.length > 0) {
        // If only single allowed, just expand the first one
        updateExpanded(new Set([allIds[0]]));
      } else {
        updateExpanded(new Set(allIds));
      }
    },
    [allowMultiple, updateExpanded]
  );

  // Collapse all rows
  const collapseAll = useCallback(() => {
    updateExpanded(new Set());
  }, [updateExpanded]);

  // Set expanded rows directly
  const setExpanded = useCallback(
    (ids: T[]) => {
      if (!allowMultiple && ids.length > 1) {
        // If only single allowed, keep the last one
        updateExpanded(new Set([ids[ids.length - 1]]));
      } else {
        updateExpanded(new Set(ids));
      }
    },
    [allowMultiple, updateExpanded]
  );

  return {
    expandedRows,
    expandedArray,
    isExpanded,
    toggle,
    expand,
    collapse,
    expandAll,
    collapseAll,
    setExpanded,
    count: expandedRows.size,
  };
}

export default useExpandedRows;
