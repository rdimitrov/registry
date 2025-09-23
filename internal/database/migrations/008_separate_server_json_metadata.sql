-- Migration 008: Separate immutable server.json from mutable registry metadata
-- This implements the server.json immutability changes to ensure server configuration
-- cannot be accidentally modified while allowing registry metadata to be updated.
--
-- Changes:
-- 1. Add individual columns for registry metadata (server_id, status, published_at, etc.)
-- 2. Add server_json column for immutable server configuration (without status, _meta)
-- 3. Migrate existing data to separate concerns
-- 4. Update indexes for new structure
-- 5. Drop old value column after migration

BEGIN;

-- Add new columns for separated concerns
ALTER TABLE servers ADD COLUMN server_id VARCHAR(255); -- Server ID (consistent across versions)
ALTER TABLE servers ADD COLUMN status VARCHAR(50) DEFAULT 'active'; -- Server status
ALTER TABLE servers ADD COLUMN published_at TIMESTAMP WITH TIME ZONE; -- When published
ALTER TABLE servers ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE; -- When last updated
ALTER TABLE servers ADD COLUMN is_latest BOOLEAN DEFAULT FALSE; -- Is this the latest version
ALTER TABLE servers ADD COLUMN server_json JSONB; -- Immutable server configuration

-- Create function to migrate existing data
CREATE OR REPLACE FUNCTION migrate_separate_server_metadata()
RETURNS VOID AS $$
DECLARE
    rec RECORD;
    clean_server_json JSONB;
    official_meta JSONB;
BEGIN
    -- Process each existing server record
    FOR rec IN SELECT version_id, value FROM servers ORDER BY version_id LOOP
        -- Extract official registry metadata
        official_meta := rec.value->'_meta'->'io.modelcontextprotocol.registry/official';

        -- Create clean server.json without registry-specific fields
        clean_server_json := rec.value;

        -- Remove registry-specific fields from server.json
        clean_server_json := clean_server_json - 'status'; -- Status goes to separate column
        clean_server_json := clean_server_json - '_meta';  -- All _meta goes to separate columns

        -- Update schema version to 2025-09-22
        clean_server_json := jsonb_set(
            clean_server_json,
            '{$schema}',
            '"https://static.modelcontextprotocol.io/schemas/2025-09-22/server.schema.json"'
        );

        -- Update the record with separated data
        UPDATE servers
        SET
            server_id = official_meta->>'serverId',
            status = COALESCE(rec.value->>'status', 'active'),
            published_at = COALESCE((official_meta->>'publishedAt')::timestamp with time zone, NOW()),
            updated_at = COALESCE((official_meta->>'updatedAt')::timestamp with time zone, NOW()),
            is_latest = COALESCE((official_meta->>'isLatest')::boolean, true),
            server_json = clean_server_json
        WHERE version_id = rec.version_id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Execute the migration
SELECT migrate_separate_server_metadata();

-- Drop the migration function
DROP FUNCTION migrate_separate_server_metadata();

-- Make new columns NOT NULL now that all records have values
ALTER TABLE servers ALTER COLUMN server_id SET NOT NULL;
ALTER TABLE servers ALTER COLUMN status SET NOT NULL;
ALTER TABLE servers ALTER COLUMN published_at SET NOT NULL;
ALTER TABLE servers ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE servers ALTER COLUMN is_latest SET NOT NULL;
ALTER TABLE servers ALTER COLUMN server_json SET NOT NULL;

-- Drop the old value column (no longer needed)
ALTER TABLE servers DROP COLUMN value;

-- Update indexes for new structure
DROP INDEX IF EXISTS idx_servers_name_latest;
DROP INDEX IF EXISTS idx_servers_updated_at;
DROP INDEX IF EXISTS idx_unique_server_version;
DROP INDEX IF EXISTS idx_unique_latest_version;

-- Create new indexes for server_json fields
CREATE INDEX idx_servers_name ON servers ((server_json->>'name'));
CREATE INDEX idx_servers_version ON servers ((server_json->>'version'));
CREATE INDEX idx_servers_remotes_gin ON servers USING GIN((server_json->'remotes'));
CREATE INDEX idx_servers_packages_gin ON servers USING GIN((server_json->'packages'));

-- Create new indexes for registry metadata columns
CREATE INDEX idx_servers_status ON servers (status);
CREATE INDEX idx_servers_published_at ON servers (published_at);
CREATE INDEX idx_servers_updated_at ON servers (updated_at);
CREATE INDEX idx_servers_server_id ON servers (server_id);

-- Recreate unique constraints with new structure
CREATE UNIQUE INDEX idx_unique_server_version
ON servers (server_id, (server_json->>'version'));

-- Only one version per server can be marked as latest
CREATE UNIQUE INDEX idx_unique_latest_version
ON servers (server_id)
WHERE is_latest = true;

-- Composite index for efficient server listing (latest versions by name)
CREATE INDEX idx_servers_name_latest
ON servers ((server_json->>'name'), is_latest)
WHERE is_latest = true;

COMMIT;