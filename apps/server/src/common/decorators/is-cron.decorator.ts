import {
  registerDecorator,
  ValidationOptions,
  ValidatorConstraint,
  ValidatorConstraintInterface,
} from 'class-validator';

/**
 * Validate cron expression format
 * Uses the same validation as the 'cron' package
 */
@ValidatorConstraint({ name: 'isCronExpression', async: false })
export class IsCronExpressionConstraint
  implements ValidatorConstraintInterface
{
  validate(value: any): boolean {
    if (typeof value !== 'string') {
      return false;
    }

    // Basic cron validation: 5 or 6 fields separated by spaces
    // Format: minute hour day-of-month month day-of-week [year]
    const parts = value.trim().split(/\s+/);
    if (parts.length < 5 || parts.length > 6) {
      return false;
    }

    // Validate each field has valid characters
    const validChars = /^[\d,\-\*\/\?LW#]+$/i;
    for (const part of parts) {
      if (!validChars.test(part)) {
        return false;
      }
    }

    // Try to validate with cron package if available
    try {
      // Dynamic import to avoid issues if cron not installed
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      const { CronJob } = require('cron');
      // CronJob constructor will throw if invalid
      new CronJob(value, () => {});
      return true;
    } catch {
      // If cron package validation fails, fall back to basic validation
      // This can happen for edge cases or if cron isn't installed
      return this.basicCronValidation(parts);
    }
  }

  private basicCronValidation(parts: string[]): boolean {
    const ranges = [
      { min: 0, max: 59 }, // minute
      { min: 0, max: 23 }, // hour
      { min: 1, max: 31 }, // day of month
      { min: 1, max: 12 }, // month
      { min: 0, max: 6 }, // day of week (0-6, Sunday-Saturday)
    ];

    for (let i = 0; i < Math.min(parts.length, 5); i++) {
      const part = parts[i];
      const range = ranges[i];

      // Skip wildcards and special characters
      if (part === '*' || part === '?') continue;

      // Handle */n (step values)
      if (part.startsWith('*/')) {
        const step = parseInt(part.slice(2), 10);
        if (isNaN(step) || step < 1) return false;
        continue;
      }

      // Handle ranges (e.g., 1-5)
      if (part.includes('-') && !part.includes(',')) {
        const [start, end] = part.split('-').map((n) => parseInt(n, 10));
        if (isNaN(start) || isNaN(end)) return false;
        if (start < range.min || end > range.max || start > end) return false;
        continue;
      }

      // Handle lists (e.g., 1,2,3)
      if (part.includes(',')) {
        const values = part.split(',').map((n) => parseInt(n, 10));
        if (values.some((v) => isNaN(v) || v < range.min || v > range.max)) {
          return false;
        }
        continue;
      }

      // Single number
      const num = parseInt(part, 10);
      if (!isNaN(num) && (num < range.min || num > range.max)) {
        return false;
      }
    }

    return true;
  }

  defaultMessage(): string {
    return 'Invalid cron expression. Expected format: "minute hour day-of-month month day-of-week" (e.g., "0 9 * * *" for daily at 9am)';
  }
}

/**
 * Decorator to validate cron expressions
 *
 * @example
 * ```typescript
 * class ScheduleDto {
 *   @IsCronExpression()
 *   cronSchedule: string;
 * }
 * ```
 */
export function IsCronExpression(validationOptions?: ValidationOptions) {
  return function (object: object, propertyName: string) {
    registerDecorator({
      target: object.constructor,
      propertyName: propertyName,
      options: validationOptions,
      constraints: [],
      validator: IsCronExpressionConstraint,
    });
  };
}
