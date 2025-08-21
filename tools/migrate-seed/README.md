# Seed Migration Tool

A CLI tool to migrate MCP Registry seed data from the old format to the new ServerRecord format that supports the extension wrapper system.

## Overview

This tool converts seed data from the old format (with direct fields like `id`, `name`, `description`) to the new format that separates:

- **ServerJSON**: Pure MCP server.json specification (immutable)
- **RegistryMetadata**: Registry-generated metadata (ID, timestamps, etc.)
- **PublisherExtensions**: Empty for migrated data (x-publisher extensions)

## Building

From the repository root:

```bash
make migrate-seed
```

Or from this directory:

```bash
./build.sh
```

## Usage

```bash
./bin/migrate-seed -input <path-to-old-seed> -output <path-for-new-seed>
```

### Examples

```bash
# Migrate the default seed data
./bin/migrate-seed -input ../../data/seed.json -output ../../data/seed-migrated.json

# Migrate custom seed file
./bin/migrate-seed -input my-old-seed.json -output my-new-seed.json
```

## Output Format

The migrated data will be in ServerRecord format:

```json
[
  {
    "ServerJSON": {
      "name": "io.github.example/server",
      "description": "Example MCP server",
      "status": "active",
      "repository": {
        "url": "https://github.com/example/server",
        "source": "github",
        "id": "123456"
      },
      "version_detail": {
        "version": "1.0.0"
      },
      "packages": [...]
    },
    "RegistryMetadata": {
      "id": "uuid-here",
      "published_at": "2025-05-16T18:56:49Z",
      "updated_at": "2025-05-16T18:56:49Z",
      "is_latest": true,
      "release_date": "2025-05-16T18:56:49Z"
    },
    "PublisherExtensions": {}
  }
]
```

## Migration Process

1. **Parses old format**: Reads the existing seed data with direct fields
2. **Extracts pure MCP spec**: Removes registry-specific fields to create clean server.json
3. **Generates metadata**: Creates RegistryMetadata with preserved IDs and timestamps
4. **Creates wrapper**: Combines everything into the new ServerRecord format
5. **Preserves data**: Maintains all original information while restructuring

## Integration

After migration, update your registry configuration to use the new seed file:

```bash
# Set the environment variable to use migrated data
export MCP_REGISTRY_SEED_FROM=data/seed-migrated.json

# Or pass it directly when starting the registry
./bin/registry --seed-file data/seed-migrated.json
```

The registry's ImportSeed functions have been updated to handle the new format automatically.