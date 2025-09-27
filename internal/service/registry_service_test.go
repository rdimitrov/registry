//nolint:testpackage
package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateNoDuplicateRemoteURLs(t *testing.T) {
	// Create test data
	existingServers := map[string]apiv0.ServerJSON{
		"existing1": {
			Name:        "com.example/existing-server",
			Description: "An existing server",
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://api.example.com/mcp"},
				{Type: "sse", URL: "https://webhook.example.com/sse"},
			},
		},
		"existing2": {
			Name:        "com.microsoft/another-server",
			Description: "Another existing server",
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://api.microsoft.com/mcp"},
			},
		},
	}

	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	for _, server := range existingServers {
		_, err := service.Publish(server)
		if err != nil {
			t.Fatalf("failed to publish server: %v", err)
		}
	}

	tests := []struct {
		name         string
		serverDetail apiv0.ServerJSON
		expectError  bool
		errorMsg     string
	}{
		{
			name: "no remote URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server with no remotes",
				Version:     "1.0.0",
				Remotes:     []model.Transport{},
			},
			expectError: false,
		},
		{
			name: "new unique remote URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://new.example.com/mcp"},
					{Type: "sse", URL: "https://unique.example.com/sse"},
				},
			},
			expectError: false,
		},
		{
			name: "duplicate remote URL - should fail",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server with duplicate URL",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://api.example.com/mcp"}, // This URL already exists
				},
			},
			expectError: true,
			errorMsg:    "remote URL https://api.example.com/mcp is already used by server com.example/existing-server",
		},
		{
			name: "updating same server with same URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/existing-server", // Same name as existing
				Description: "Updated existing server",
				Version:     "1.1.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://api.example.com/mcp"}, // Same URL as before
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			impl := service.(*registryServiceImpl)

			err := impl.validateNoDuplicateRemoteURLs(ctx, nil, tt.serverDetail, tt.serverDetail.Name)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetByServerName(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Publish multiple versions of the same server
	_, err := service.Publish(apiv0.ServerJSON{
		Name:        "com.example/test-server",
		Description: "Test server v1",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/test-server",
		Description: "Test server v2",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:        "get latest version by server name",
			serverName:  "com.example/test-server",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Server.Version) // Should get latest version
				assert.Equal(t, "Test server v2", result.Server.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "server not found",
			serverName:  "com.example/nonexistent",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetByServerName(tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestGetByServerNameAndVersion(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Publish multiple versions of the same server
	_, err := service.Publish(apiv0.ServerJSON{
		Name:        "com.example/versioned-server",
		Description: "Versioned server v1",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/versioned-server",
		Description: "Versioned server v2",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		version     string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:        "get specific version 1.0.0",
			serverName:  "com.example/versioned-server",
			version:     "1.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "1.0.0", result.Server.Version)
				assert.Equal(t, "Versioned server v1", result.Server.Description)
				assert.False(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "get specific version 2.0.0",
			serverName:  "com.example/versioned-server",
			version:     "2.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Server.Version)
				assert.Equal(t, "Versioned server v2", result.Server.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "version not found",
			serverName:  "com.example/versioned-server",
			version:     "3.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
		{
			name:        "server not found",
			serverName:  "com.example/nonexistent",
			version:     "1.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetByServerNameAndVersion(tt.serverName, tt.version)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestGetAllVersionsByServerName(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Publish multiple versions of the same server
	_, err := service.Publish(apiv0.ServerJSON{
		Name:        "com.example/multi-version-server",
		Description: "Multi-version server v1",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/multi-version-server",
		Description: "Multi-version server v2",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/multi-version-server",
		Description: "Multi-version server v2.1",
		Version:     "2.1.0",
	})
	assert.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, []*apiv0.ServerResponse)
	}{
		{
			name:        "get all versions of server",
			serverName:  "com.example/multi-version-server",
			expectError: false,
			checkResult: func(t *testing.T, result []*apiv0.ServerResponse) {
				t.Helper()
				assert.Len(t, result, 3)

				// Collect versions
				versions := make([]string, 0, len(result))
				latestCount := 0
				for _, serverResp := range result {
					versions = append(versions, serverResp.Server.Version)
					assert.Equal(t, "com.example/multi-version-server", serverResp.Server.Name)
					if serverResp.Meta.Official.IsLatest {
						latestCount++
					}
				}

				// Verify all versions are present
				assert.Contains(t, versions, "1.0.0")
				assert.Contains(t, versions, "2.0.0")
				assert.Contains(t, versions, "2.1.0")

				// Only one should be marked as latest
				assert.Equal(t, 1, latestCount)
			},
		},
		{
			name:        "server not found",
			serverName:  "com.example/nonexistent",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetAllVersionsByServerName(tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestPublishConcurrentVersionsNoRace(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	const concurrency = 100
	results := make([]*apiv0.ServerResponse, concurrency)
	errors := make([]error, concurrency)

	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := service.Publish(apiv0.ServerJSON{
				Name:        "com.example/test-concurrent",
				Description: fmt.Sprintf("Version %d", idx),
				Version:     fmt.Sprintf("1.0.%d", idx),
			})
			results[idx] = result
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		assert.NoError(t, err, "publish %d failed", i)
	}

	// Verify all results have the same server name
	for i, result := range results {
		if result != nil {
			assert.Equal(t, "com.example/test-concurrent", result.Server.Name,
				"version %d has different server name", i)
		}
	}

	// Query database to check the final state after all publishes complete
	// This is necessary because the returned results reflect state at publish time,
	// but concurrent publishes may have updated the latest flag since then
	filter := &database.ServerFilter{Name: strPtr("com.example/test-concurrent")}
	dbResults, _, err := testDB.List(context.Background(), nil, filter, "", 1000)
	assert.NoError(t, err, "failed to query database")

	latestCount := 0
	var latestVersion string
	for _, r := range dbResults {
		if r.Meta.Official != nil && r.Meta.Official.IsLatest {
			latestCount++
			latestVersion = r.Server.Version
		}
	}

	assert.Equal(t, 1, latestCount, "should have exactly one latest version in database, found version: %s", latestVersion)
}

func strPtr(s string) *string {
	return &s
}
