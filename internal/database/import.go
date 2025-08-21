package database

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/internal/model"
)

// ReadSeedFile reads seed data from various sources:
// 1. Local file paths (*.json files)
// 2. Direct HTTP URLs to seed.json files
// 3. Registry root URLs (automatically appends /v0/servers and paginates)
func ReadSeedFile(ctx context.Context, path string) ([]model.ServerDetail, error) {
	log.Printf("Reading seed data from %s", path)

	// Set default seed file path if not provided
	if path == "" {
		// Try to find the seed.json in the data directory
		path = filepath.Join("data", "seed.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("seed file not found at %s", path)
		}
	}

	// Check if path is an HTTP URL
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// Determine if this is a direct seed file URL or a registry root URL
		if strings.HasSuffix(path, ".json") || strings.Contains(path, "seed.json") {
			// Direct seed file URL - read directly
			fileContent, err := readFromHTTP(ctx, path)
			if err != nil {
				return nil, fmt.Errorf("failed to read from HTTP URL: %w", err)
			}
			return parseSeedJSON(fileContent)
		}
		// Registry root URL - paginate through /v0/servers endpoint
		return readFromRegistryWithContext(ctx, path)
	}
	// Read from local file
	fileContent, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return parseSeedJSON(fileContent)
}

// readFromHTTP reads content from an HTTP URL with timeout
func readFromHTTP(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// parseSeedJSON parses JSON content into ServerDetail objects
func parseSeedJSON(fileContent []byte) ([]model.ServerDetail, error) {
	var servers []model.ServerDetail
	if err := json.Unmarshal(fileContent, &servers); err != nil {
		// Try parsing as a raw JSON array and then convert to our model
		var rawData []map[string]any
		if jsonErr := json.Unmarshal(fileContent, &rawData); jsonErr != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w (original error: %w)", jsonErr, err)
		}
	}

	log.Printf("Found %d server entries in seed data", len(servers))
	return servers, nil
}

// PaginatedResponse represents the paginated response from /v0/servers endpoint
// PaginatedResponse represents the structure of a paginated response from /v0/servers endpoint
type PaginatedResponse struct {
	Data     []model.ServerDetail `json:"servers"`
	Metadata Metadata             `json:"metadata,omitempty"`
}

// Metadata contains pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// readFromRegistryWithContext reads all servers from a registry by paginating through /v0/servers endpoint
// readFromRegistryWithContext reads all servers from a registry by paginating through /v0/servers endpoint
func readFromRegistryWithContext(ctx context.Context, registryURL string) ([]model.ServerDetail, error) {
	// TODO: Update for new wrapper API format after Phase 4 completion
	return nil, fmt.Errorf("registry import not yet updated for new API format")
}