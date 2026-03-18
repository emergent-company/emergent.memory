-- +goose Up
-- Add role column to core.superadmins table
ALTER TABLE core.superadmins ADD COLUMN role VARCHAR(50);

-- Backfill existing rows with superadmin_full
UPDATE core.superadmins SET role = 'superadmin_full' WHERE role IS NULL;

-- Make column non-null with default
ALTER TABLE core.superadmins ALTER COLUMN role SET NOT NULL;
ALTER TABLE core.superadmins ALTER COLUMN role SET DEFAULT 'superadmin_full';

-- Add check constraint for valid role values
ALTER TABLE core.superadmins ADD CONSTRAINT superadmins_role_check 
    CHECK (role IN ('superadmin_full', 'superadmin_readonly'));

-- +goose Down
-- Remove check constraint
ALTER TABLE core.superadmins DROP CONSTRAINT IF EXISTS superadmins_role_check;

-- Drop role column
ALTER TABLE core.superadmins DROP COLUMN IF EXISTS role;
