package v0_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestEditServerEndpoint(t *testing.T) {
	// Test if removing URL escaping fixes path parameter parsing
	// Create registry service and insert a common test server
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig())

	// Publish a test server that will be used across test cases
	testServer := apiv0.ServerJSON{
		Name:        "io.github.domdomegg/test-server",
		Description: "Original test server",
		Repository: model.Repository{
			URL:    "https://github.com/domdomegg/test-server",
			Source: "github",
			ID:     "domdomegg/test-server",
		},
		Version: "1.0.0",
	}
	published, err := registryService.Publish(testServer)
	assert.NoError(t, err)
	assert.NotNil(t, published)
	assert.NotNil(t, published.Meta)
	assert.NotNil(t, published.Meta.Official)

	testServerName := published.Server.Name

	// Publish a second server for permission testing
	otherServer := apiv0.ServerJSON{
		Name:        "io.github.other/test-server",
		Description: "Other test server",
		Repository: model.Repository{
			URL:    "https://github.com/other/test-server",
			Source: "github",
			ID:     "other/test-server",
		},
		Version: "1.0.0",
	}
	otherPublished, err := registryService.Publish(otherServer)
	assert.NoError(t, err)
	assert.NotNil(t, otherPublished)
	assert.NotNil(t, otherPublished.Meta)
	assert.NotNil(t, otherPublished.Meta.Official)

	otherServerName := otherPublished.Server.Name

	// Publish a deleted server for undelete testing
	deletedServer := apiv0.ServerJSON{
		Name:        "io.github.domdomegg/deleted-server",
		Description: "Deleted test server",
		Repository: model.Repository{
			URL:    "https://github.com/domdomegg/deleted-server",
			Source: "github",
			ID:     "domdomegg/deleted-server",
		},
		Version: "1.0.0",
	}
	deletedPublished, err := registryService.Publish(deletedServer)
	assert.NoError(t, err)
	assert.NotNil(t, deletedPublished)
	assert.NotNil(t, deletedPublished.Meta)
	assert.NotNil(t, deletedPublished.Meta.Official)

	deletedServerName := deletedPublished.Server.Name

	testCases := []struct {
		name           string
		authHeader     string
		requestBody    interface{}
		serverName     string
		version        string
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful edit with valid token and permissions",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: v0.EditServerInput{
				Server: apiv0.ServerJSON{
					Name:        "io.github.domdomegg/test-server",
					Description: "Updated test server",
					Repository: model.Repository{
						URL:    "https://github.com/domdomegg/test-server",
						Source: "github",
						ID:     "domdomegg/test-server",
					},
					Version: "1.0.0",
				},
				Status: func() *string { s := "deprecated"; return &s }(),
			},
			serverName:     testServerName,
			version:        "1.0.0",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing authorization header",
			authHeader:     "",
			requestBody:    apiv0.ServerJSON{},
			serverName:     testServerName,
			version:        "1.0.0",
			expectedStatus: 422,
			expectedError:  "required header parameter is missing",
		},
		{
			name:       "invalid authorization header format",
			authHeader: "InvalidFormat token123",
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/test-server",
				Description: "Test server",
				Version:     "1.0.0",
			},
			serverName:     testServerName,
			version:        "1.0.0",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name:       "invalid token",
			authHeader: "Bearer invalid-token",
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/test-server",
				Description: "Test server",
				Version:     "1.0.0",
			},
			serverName:     testServerName,
			version:        "1.0.0",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "Unauthorized",
		},
		{
			name: "permission denied - no edit permissions",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/test-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			serverName:     testServerName,
			version:        "1.0.0",
			expectedStatus: http.StatusForbidden,
			expectedError:  "Forbidden",
		},
		{
			name: "permission denied - wrong resource",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.other/test-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			serverName:     otherServerName,
			version:        "1.0.0",
			expectedStatus: http.StatusForbidden,
			expectedError:  "Forbidden",
		},
		{
			name: "server not found",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "io.github.domdomegg/*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "io.github.domdomegg/nonexistent-server",
				Description: "Updated test server",
				Version:     "1.0.0",
			},
			serverName:     "io.github.domdomegg/nonexistent-server", // Non-existent server
			version:        "1.0.0",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Not Found",
		},
		{
			name: "validation error - invalid server name",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: apiv0.ServerJSON{
				Name:        "invalid-name-format", // Missing namespace/name format
				Description: "Test server",
				Version:     "1.0.0",
			},
			serverName:     testServerName,
			version:        "1.0.0",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Bad Request",
		},
		{
			name: "cannot undelete server",
			authHeader: func() string {
				cfg := &config.Config{JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c"}
				token, _ := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod:        auth.MethodGitHubAT,
					AuthMethodSubject: "domdomegg",
					Permissions: []auth.Permission{
						{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
					},
				})
				return "Bearer " + token
			}(),
			requestBody: v0.EditServerInput{
				Server: apiv0.ServerJSON{
					Name:        "io.github.domdomegg/deleted-server",
					Description: "Trying to undelete server",
					Repository: model.Repository{
						URL:    "https://github.com/domdomegg/deleted-server",
						Source: "github",
						ID:     "domdomegg/deleted-server",
					},
					Version: "1.0.1",
				},
				Status: func() *string { s := "active"; return &s }(),
			},
			serverName:     deletedServerName,
			version:        "1.0.0",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Cannot change status of deleted server",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create Huma API
			mux := http.NewServeMux()
			humaConfig := huma.DefaultConfig("Test API", "1.0.0")
			api := humago.New(mux, humaConfig)

			// Register server endpoints
			cfg := &config.Config{
				JWTPrivateKey: "bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c",
			}
			v0.RegisterServersEndpoints(api, registryService, cfg)

			// Create request body
			var requestBody []byte
			var err error
			if str, ok := tc.requestBody.(string); ok {
				requestBody = []byte(str)
			} else {
				requestBody, err = json.Marshal(tc.requestBody)
				assert.NoError(t, err)
			}

			// Create request - try URL escaping back
			urlPath := "/v0/servers/" + url.PathEscape(tc.serverName) + "/versions/" + tc.version
			t.Logf("Request URL: %s", urlPath)
			req := httptest.NewRequest(http.MethodPut, urlPath, bytes.NewReader(requestBody))
			req.Header.Set("Content-Type", "application/json")
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Call the endpoint
			mux.ServeHTTP(w, req)

			// Check status code
			if w.Code != tc.expectedStatus {
				t.Logf("Response body: %s", w.Body.String())
			}
			assert.Equal(t, tc.expectedStatus, w.Code)

			// Check error message if expected
			if tc.expectedError != "" {
				assert.Contains(t, w.Body.String(), tc.expectedError)
			}

			// No mock assertions needed with real service
		})
	}
}
