package database

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// MemoryDB is an in-memory implementation of the Database interface
type MemoryDB struct {
	entries map[string]*model.ServerRecord
	mu      sync.RWMutex
}

// NewMemoryDB creates a new instance of the in-memory database
func NewMemoryDB(e map[string]*model.ServerDetail) *MemoryDB {
	// Convert ServerDetail entries to ServerRecord entries
	serverRecords := make(map[string]*model.ServerRecord)
	for k, v := range e {
		// Convert ServerDetail to ServerRecord for compatibility
		serverJSON, _ := json.Marshal(v)
		serverRecords[k] = &model.ServerRecord{
			ServerJSON:          serverJSON,
			RegistryMetadata:    model.RegistryMetadata{ID: k, IsLatest: true},
			PublisherExtensions: make(map[string]interface{}),
		}
	}
	return &MemoryDB{
		entries: serverRecords,
	}
}

// compareSemanticVersions compares two semantic version strings
// Returns:
//
//	-1 if version1 < version2
//	 0 if version1 == version2
//	+1 if version1 > version2
func compareSemanticVersions(version1, version2 string) int {
	// Simple semantic version comparison
	// Assumes format: major.minor.patch

	parts1 := strings.Split(version1, ".")
	parts2 := strings.Split(version2, ".")

	// Pad with zeros if needed
	maxLen := max(len(parts2), len(parts1))

	for len(parts1) < maxLen {
		parts1 = append(parts1, "0")
	}
	for len(parts2) < maxLen {
		parts2 = append(parts2, "0")
	}

	// Compare each part
	for i := 0; i < maxLen; i++ {
		num1, err1 := strconv.Atoi(parts1[i])
		num2, err2 := strconv.Atoi(parts2[i])

		// If parsing fails, fall back to string comparison
		if err1 != nil || err2 != nil {
			if parts1[i] < parts2[i] {
				return -1
			} else if parts1[i] > parts2[i] {
				return 1
			}
			continue
		}

		if num1 < num2 {
			return -1
		} else if num1 > num2 {
			return 1
		}
	}

	return 0
}

// List retrieves all MCPRegistry entries with optional filtering and pagination
//
//gocognit:ignore
func (db *MemoryDB) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*model.ServerRecord, string, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Convert all entries to a slice for pagination, filter by is_latest
	var allEntries []*model.ServerRecord
	for _, entry := range db.entries {
		if entry.RegistryMetadata.IsLatest {
			allEntries = append(allEntries, entry)
		}
	}

	// Simple filtering implementation
	var filteredEntries []*model.ServerRecord
	for _, entry := range allEntries {
		include := true

		// Parse server JSON for filtering
		var serverData map[string]interface{}
		if err := json.Unmarshal(entry.ServerJSON, &serverData); err != nil {
			continue // Skip invalid entries
		}

		// Apply filters if any
		for key, value := range filter {
			switch key {
			case "name":
				if serverName, ok := serverData["name"].(string); !ok || serverName != value.(string) {
					include = false
				}
			case "version":
				if versionDetail, ok := serverData["version_detail"].(map[string]interface{}); ok {
					if version, ok := versionDetail["version"].(string); !ok || version != value.(string) {
						include = false
					}
				} else {
					include = false
				}
			case "serverDetail.id":
				if entry.RegistryMetadata.ID != value.(string) {
					include = false
				}
			}
		}

		if include {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Sort filteredEntries by ID for consistent pagination
	sort.Slice(filteredEntries, func(i, j int) bool {
		return filteredEntries[i].RegistryMetadata.ID < filteredEntries[j].RegistryMetadata.ID
	})

	// Find starting point for cursor-based pagination
	startIdx := 0
	if cursor != "" {
		for i, entry := range filteredEntries {
			if entry.RegistryMetadata.ID == cursor {
				startIdx = i + 1 // Start after the cursor
				break
			}
		}
	}

	// Apply pagination
	endIdx := min(startIdx+limit, len(filteredEntries))

	var result []*model.ServerRecord
	if startIdx < len(filteredEntries) {
		result = filteredEntries[startIdx:endIdx]
	} else {
		result = []*model.ServerRecord{}
	}

	// Determine next cursor
	nextCursor := ""
	if endIdx < len(filteredEntries) {
		nextCursor = filteredEntries[endIdx-1].RegistryMetadata.ID
	}

	return result, nextCursor, nil
}

// GetByID retrieves a single ServerDetail by its ID
func (db *MemoryDB) GetByID(ctx context.Context, id string) (*model.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Find entry by registry metadata ID
	for _, entry := range db.entries {
		if entry.RegistryMetadata.ID == id {
			return entry, nil
		}
	}

	return nil, ErrNotFound
}

// Publish adds a new ServerRecord to the database
func (db *MemoryDB) Publish(ctx context.Context, serverJSON []byte, publisherExtensions map[string]interface{}) (*model.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Parse serverJSON to extract name and version
	var serverData map[string]interface{}
	if err := json.Unmarshal(serverJSON, &serverData); err != nil {
		return nil, fmt.Errorf("invalid server JSON: %w", err)
	}
	
	// Extract name
	name, ok := serverData["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required in server JSON")
	}
	
	// Extract version
	versionDetail, ok := serverData["version_detail"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("version_detail is required in server JSON")
	}
	
	version, ok := versionDetail["version"].(string)
	if !ok || version == "" {
		return nil, fmt.Errorf("version is required in version_detail")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// Check for existing entry with same name and compare versions
	var existingRecord *model.ServerRecord
	for _, entry := range db.entries {
		if entry.RegistryMetadata.IsLatest {
			var existingServerData map[string]interface{}
			if err := json.Unmarshal(entry.ServerJSON, &existingServerData); err == nil {
				if existingName, ok := existingServerData["name"].(string); ok && existingName == name {
					existingRecord = entry
					break
				}
			}
		}
	}

	// Version comparison
	if existingRecord != nil {
		var existingServerData map[string]interface{}
		if err := json.Unmarshal(existingRecord.ServerJSON, &existingServerData); err == nil {
			if existingVersionDetail, ok := existingServerData["version_detail"].(map[string]interface{}); ok {
				if existingVersion, ok := existingVersionDetail["version"].(string); ok {
					if version <= existingVersion {
						return nil, fmt.Errorf("version must be greater than existing version %s", existingVersion)
					}
				}
			}
		}
	}

	// Create new registry metadata
	now := time.Now()
	registryMetadata := model.RegistryMetadata{
		ID:          uuid.New().String(),
		PublishedAt: now,
		UpdatedAt:   now,
		IsLatest:    true,
		ReleaseDate: now.Format(time.RFC3339),
	}

	// Create server record
	record := &model.ServerRecord{
		ServerJSON:          serverJSON,
		RegistryMetadata:    registryMetadata,
		PublisherExtensions: publisherExtensions,
	}

	// Mark existing record as not latest
	if existingRecord != nil {
		existingRecord.RegistryMetadata.IsLatest = false
	}

	// Store the record
	db.entries[registryMetadata.ID] = record

	return record, nil
}

// ImportSeed imports initial data from a seed file into memory database
func (db *MemoryDB) ImportSeed(ctx context.Context, seedFilePath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	
	// Read the migrated seed data (should be in ServerRecord format)
	seedRecords, err := ReadSeedFile(ctx, seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}
	
	db.mu.Lock()
	defer db.mu.Unlock()
	
	// Clear existing data
	db.entries = make(map[string]*model.ServerRecord)
	
	// Import all seed records
	for _, record := range seedRecords {
		db.entries[record.RegistryMetadata.ID] = &record
	}
	
	return nil
}

// Close closes the database connection
// For an in-memory database, this is a no-op
func (db *MemoryDB) Close() error {
	return nil
}

// Connection returns information about the database connection
func (db *MemoryDB) Connection() *ConnectionInfo {
	return &ConnectionInfo{
		Type:        ConnectionTypeMemory,
		IsConnected: true, // Memory DB is always connected
		Raw:         db.entries,
	}
}
