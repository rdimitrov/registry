package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// OldServerDetail represents the old seed data format
type OldServerDetail struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	Status        string           `json:"status,omitempty"`
	Repository    model.Repository `json:"repository"`
	VersionDetail OldVersionDetail `json:"version_detail"`
	Packages      []model.Package  `json:"packages,omitempty"`
	Remotes       []model.Remote   `json:"remotes,omitempty"`
	CreatedAt     *time.Time       `json:"created_at,omitempty"`
	UpdatedAt     *time.Time       `json:"updated_at,omitempty"`
	// Any other fields that might exist in the old format
	ExtraFields map[string]interface{} `json:"-"`
}

// OldVersionDetail represents the old version detail format
type OldVersionDetail struct {
	Version     string `json:"version"`
	ReleaseDate string `json:"release_date"`
	IsLatest    bool   `json:"is_latest"`
}

func main() {
	var inputFile string
	var outputFile string

	flag.StringVar(&inputFile, "input", "data/seed.json", "Input seed file path")
	flag.StringVar(&outputFile, "output", "data/seed-migrated.json", "Output file path for migrated data")
	flag.Parse()

	if inputFile == "" || outputFile == "" {
		flag.Usage()
		log.Fatal("Both input and output files are required")
	}

	log.Printf("Migrating seed data from %s to %s", inputFile, outputFile)

	// Read the old seed data
	data, err := os.ReadFile(inputFile)
	if err != nil {
		log.Fatalf("Failed to read input file: %v", err)
	}

	// Parse as array of old server details
	var oldServers []json.RawMessage
	if err := json.Unmarshal(data, &oldServers); err != nil {
		log.Fatalf("Failed to parse input JSON: %v", err)
	}

	log.Printf("Found %d servers to migrate", len(oldServers))

	// Convert each server to the new API response format
	var newServers []model.ServerResponse
	for i, rawServer := range oldServers {
		serverResponse, err := migrateServerToResponse(rawServer)
		if err != nil {
			log.Printf("Warning: Failed to migrate server %d: %v", i+1, err)
			continue
		}
		newServers = append(newServers, *serverResponse)
	}

	log.Printf("Successfully migrated %d servers", len(newServers))

	// Write the new format
	newData, err := json.MarshalIndent(newServers, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal new data: %v", err)
	}

	if err := os.WriteFile(outputFile, newData, 0644); err != nil {
		log.Fatalf("Failed to write output file: %v", err)
	}

	log.Printf("Migration complete! New seed data written to %s", outputFile)
}

// migrateServer converts a single server from old format to new ServerRecord format
func migrateServer(rawServer json.RawMessage) (*model.ServerRecord, error) {
	// First, parse the old format to extract metadata
	var oldServer OldServerDetail
	if err := json.Unmarshal(rawServer, &oldServer); err != nil {
		return nil, fmt.Errorf("failed to parse old server format: %w", err)
	}

	// Convert status string to ServerStatus type
	var status model.ServerStatus
	switch oldServer.Status {
	case "active":
		status = model.ServerStatusActive
	case "deprecated":
		status = model.ServerStatusDeprecated
	default:
		status = model.ServerStatusActive // Default to active if unknown
	}

	// Create the new ServerDetail (pure MCP spec) by removing registry-specific fields
	newServerDetail := model.ServerDetail{
		Name:        oldServer.Name,
		Description: oldServer.Description,
		Status:      status,
		Repository:  oldServer.Repository,
		VersionDetail: model.VersionDetail{
			Version: oldServer.VersionDetail.Version,
		},
		Packages: oldServer.Packages,
		Remotes:  oldServer.Remotes,
	}

	// Convert ServerDetail to JSON (this becomes the immutable server.json)
	serverJSON, err := json.Marshal(newServerDetail)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Create registry metadata from old fields
	var publishedAt, updatedAt time.Time
	var releaseDate string

	// Parse the release date from old format
	if oldServer.VersionDetail.ReleaseDate != "" {
		if parsedTime, err := time.Parse(time.RFC3339, oldServer.VersionDetail.ReleaseDate); err == nil {
			releaseDate = parsedTime.Format(time.RFC3339)
			publishedAt = parsedTime
			updatedAt = parsedTime
		} else {
			// Fallback to current time if parsing fails
			now := time.Now()
			releaseDate = now.Format(time.RFC3339)
			publishedAt = now
			updatedAt = now
		}
	} else {
		now := time.Now()
		releaseDate = now.Format(time.RFC3339)
		publishedAt = now
		updatedAt = now
	}

	// Use existing ID if present, otherwise generate new UUID
	serverID := oldServer.ID
	if serverID == "" {
		serverID = uuid.New().String()
	}

	// Override with old timestamps if they exist
	if oldServer.CreatedAt != nil {
		publishedAt = *oldServer.CreatedAt
	}
	if oldServer.UpdatedAt != nil {
		updatedAt = *oldServer.UpdatedAt
	}

	registryMetadata := model.RegistryMetadata{
		ID:          serverID,
		PublishedAt: publishedAt,
		UpdatedAt:   updatedAt,
		IsLatest:    oldServer.VersionDetail.IsLatest,
		ReleaseDate: releaseDate,
	}

	// Create the new ServerRecord
	serverRecord := &model.ServerRecord{
		ServerJSON:          serverJSON,
		RegistryMetadata:    registryMetadata,
		PublisherExtensions: make(map[string]interface{}), // Empty for migrated data
	}

	return serverRecord, nil
}

// migrateServerToResponse converts a single server from old format to new ServerResponse format (API format)
func migrateServerToResponse(rawServer json.RawMessage) (*model.ServerResponse, error) {
	// First convert to ServerRecord
	serverRecord, err := migrateServer(rawServer)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate server: %w", err)
	}

	// Create the API response format
	response := &model.ServerResponse{
		Server: serverRecord.ServerJSON,
		XIOModelContextProtocolRegistry: map[string]interface{}{
			"id":           serverRecord.RegistryMetadata.ID,
			"published_at": serverRecord.RegistryMetadata.PublishedAt,
			"updated_at":   serverRecord.RegistryMetadata.UpdatedAt,
			"is_latest":    serverRecord.RegistryMetadata.IsLatest,
			"release_date": serverRecord.RegistryMetadata.ReleaseDate,
		},
	}

	// Add publisher extensions if present (usually empty for migrated data)
	if publisherData, exists := serverRecord.PublisherExtensions["x-publisher"]; exists {
		response.XPublisher = publisherData
	}

	return response, nil
}
