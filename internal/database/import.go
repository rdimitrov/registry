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
func ReadSeedFile(ctx context.Context, path string) ([]model.ServerRecord, error) {
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

// parseSeedJSON parses JSON content from ServerResponse format into ServerRecord objects
func parseSeedJSON(fileContent []byte) ([]model.ServerRecord, error) {
	var serverResponses []model.ServerResponse
	if err := json.Unmarshal(fileContent, &serverResponses); err != nil {
		return nil, fmt.Errorf("failed to parse seed JSON as ServerResponse format: %w", err)
	}

	// Convert ServerResponse format to ServerRecord format
	var servers []model.ServerRecord
	for _, serverResponse := range serverResponses {
		record := model.ServerRecord{
			ServerJSON:          serverResponse.Server,
			PublisherExtensions: make(map[string]interface{}),
		}
		
		// Extract registry metadata
		if serverResponse.XIOModelContextProtocolRegistry != nil {
			if metaBytes, err := json.Marshal(serverResponse.XIOModelContextProtocolRegistry); err == nil {
				var metadata model.RegistryMetadata
				if err := json.Unmarshal(metaBytes, &metadata); err == nil {
					record.RegistryMetadata = metadata
				}
			}
		}
		
		// Extract publisher extensions
		if serverResponse.XPublisher != nil {
			record.PublisherExtensions["x-publisher"] = serverResponse.XPublisher
		}
		
		servers = append(servers, record)
	}

	log.Printf("Found %d server entries in seed data", len(servers))
	return servers, nil
}

// PaginatedResponse represents the paginated response from /v0/servers endpoint
// PaginatedResponse represents the structure of a paginated response from /v0/servers endpoint
type PaginatedResponse struct {
	Data     []model.ServerResponse `json:"servers"`
	Metadata Metadata               `json:"metadata,omitempty"`
}

// Metadata contains pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// readFromRegistryWithContext reads all servers from a registry by paginating through /v0/servers endpoint
// readFromRegistryWithContext reads all servers from a registry by paginating through /v0/servers endpoint
func readFromRegistryWithContext(ctx context.Context, registryURL string) ([]model.ServerRecord, error) {
	var allServers []model.ServerRecord
	cursor := ""
	
	for {
		// Build URL with cursor if we have one
		url := strings.TrimSuffix(registryURL, "/") + "/v0/servers"
		if cursor != "" {
			url += "?cursor=" + cursor
		}
		
		// Fetch the page
		data, err := readFromHTTP(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to read page from registry: %w", err)
		}
		
		// Parse the response
		var response PaginatedResponse  
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("failed to parse registry response: %w", err)
		}
		
		// Convert ServerResponse back to ServerRecord
		for _, serverResponse := range response.Data {
			record := model.ServerRecord{
				ServerJSON:          serverResponse.Server,
				PublisherExtensions: make(map[string]interface{}),
			}
			
			// Extract registry metadata
			if serverResponse.XIOModelContextProtocolRegistry != nil {
				if metaBytes, err := json.Marshal(serverResponse.XIOModelContextProtocolRegistry); err == nil {
					var metadata model.RegistryMetadata
					if err := json.Unmarshal(metaBytes, &metadata); err == nil {
						record.RegistryMetadata = metadata
					}
				}
			}
			
			// Extract publisher extensions
			if serverResponse.XPublisher != nil {
				record.PublisherExtensions["x-publisher"] = serverResponse.XPublisher
			}
			
			allServers = append(allServers, record)
		}
		
		// Check if there are more pages
		if response.Metadata.NextCursor == "" {
			break
		}
		cursor = response.Metadata.NextCursor
	}
	
	log.Printf("Retrieved %d servers from registry", len(allServers))
	return allServers, nil
}