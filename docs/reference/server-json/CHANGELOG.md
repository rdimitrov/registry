# Server JSON Schema Changelog

Changes to the server.json schema and format.

## 2025-09-22

### ⚠️ BREAKING CHANGES

#### Status Field Removal: Immutable server.json

The `status` field has been **removed** from server.json schema. Server lifecycle status is now managed by the registry independently from the immutable server configuration.

**What changed:**
- `status` field removed from server.json
- Server.json is now immutable - cannot be modified after publishing
- Registry metadata (status, timestamps) managed separately from server configuration
- Schema version updated to `2025-09-22`

**Migration required:**
- Remove `status` field from your server.json files
- Update `$schema` to `https://static.modelcontextprotocol.io/schemas/2025-09-22/server.schema.json`

### Changed
- Schema version: `2025-09-16` → `2025-09-22`
- Removed `status` field from server.json schema

## 2025-09-16

### ⚠️ BREAKING CHANGES

#### Field Names: snake_case → camelCase ([#428](https://github.com/modelcontextprotocol/registry/issues/428))

All JSON field names standardized to camelCase. **All existing `server.json` files must be updated.**

**Changed fields:**
- `registry_type` → `registryType`
- `registry_base_url` → `registryBaseUrl`
- `file_sha256` → `fileSha256`
- `runtime_hint` → `runtimeHint`
- `runtime_arguments` → `runtimeArguments`
- `package_arguments` → `packageArguments`
- `environment_variables` → `environmentVariables`
- `is_required` → `isRequired`
- `is_secret` → `isSecret`
- `value_hint` → `valueHint`
- `is_repeated` → `isRepeated`
- `website_url` → `websiteUrl`

#### Migration Examples

**Package Configuration:**
```json
// OLD - Will be rejected
{
  "packages": [{
    "registry_type": "npm",
    "registry_base_url": "https://registry.npmjs.org",
    "file_sha256": "abc123...",
    "runtime_hint": "node",
    "runtime_arguments": [...],
    "package_arguments": [...],
    "environment_variables": [...]
  }]
}

// NEW - Required format
{
  "packages": [{
    "registryType": "npm",
    "registryBaseUrl": "https://registry.npmjs.org",
    "fileSha256": "abc123...",
    "runtimeHint": "node",
    "runtimeArguments": [...],
    "packageArguments": [...],
    "environmentVariables": [...]
  }]
}
```

**Arguments Configuration:**
```json
// OLD - Will be rejected
{
  "runtime_arguments": [
    {
      "name": "port",
      "is_required": true,
      "is_repeated": false,
      "value_hint": "8080"
    }
  ]
}

// NEW - Required format
{
  "runtimeArguments": [
    {
      "name": "port",
      "isRequired": true,
      "isRepeated": false,
      "valueHint": "8080"
    }
  ]
}
```

**Environment Variables:**
```json
// OLD - Will be rejected
{
  "environment_variables": [
    {
      "name": "API_KEY",
      "is_required": true,
      "is_secret": true
    }
  ]
}

// NEW - Required format
{
  "environmentVariables": [
    {
      "name": "API_KEY",
      "isRequired": true,
      "isSecret": true
    }
  ]
}
```

#### Migration Checklist for Publishers

- [ ] Update your `server.json` files to use camelCase field names
- [ ] Test server publishing with new CLI version
- [ ] Update any automation scripts that reference old field names
- [ ] Update documentation referencing old field names

#### Updated Schema Reference

🔗 **Current schema**: https://static.modelcontextprotocol.io/schemas/2025-09-22/server.schema.json

### Changed
- Schema version: `2025-09-16` → `2025-09-22`

## 2025-07-09

Initial release of the server.json schema.