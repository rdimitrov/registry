package v0

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// PublishServerInput represents the input for publishing a server
type PublishServerInput struct {
	Authorization string `header:"Authorization" doc:"Registry JWT token (obtained from /v0/auth/token/github)" required:"true"`
	Body          model.PublishRequest
}

// validateExtensions validates that only exactly "x-publisher" extensions are allowed
// and enforces the 4KB size limit on extensions
func validateExtensions(extensions map[string]interface{}) error {
	// Check that only "x-publisher" extensions are present
	for key := range extensions {
		if key != "x-publisher" {
			return fmt.Errorf("only 'x-publisher' extensions are allowed, found: %s", key)
		}
	}

	// Check 4KB size limit on extensions
	extensionsJSON, err := json.Marshal(extensions)
	if err != nil {
		return fmt.Errorf("failed to marshal extensions: %w", err)
	}
	
	const maxExtensionsSize = 4 * 1024 // 4KB
	if len(extensionsJSON) > maxExtensionsSize {
		return fmt.Errorf("extensions exceed 4KB limit: %d bytes", len(extensionsJSON))
	}
	
	return nil
}

// RegisterPublishEndpoint registers the publish endpoint
func RegisterPublishEndpoint(api huma.API, registry service.RegistryService, cfg *config.Config) {
	// Create JWT manager for token validation
	jwtManager := auth.NewJWTManager(cfg)

	huma.Register(api, huma.Operation{
		OperationID: "publish-server",
		Method:      http.MethodPost,
		Path:        "/v0/publish",
		Summary:     "Publish MCP server",
		Description: "Publish a new MCP server to the registry or update an existing one",
		Tags:        []string{"publish"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *PublishServerInput) (*Response[model.ServerResponse], error) {
		// Extract bearer token
		const bearerPrefix = "Bearer "
		authHeader := input.Authorization
		if len(authHeader) < len(bearerPrefix) || !strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
			return nil, huma.Error401Unauthorized("Invalid Authorization header format. Expected 'Bearer <token>'")
		}
		token := authHeader[len(bearerPrefix):]

		// Validate Registry JWT token
		claims, err := jwtManager.ValidateToken(ctx, token)
		if err != nil {
			return nil, huma.Error401Unauthorized("Invalid or expired Registry JWT token", err)
		}

		// Validate extensions (only x-publisher allowed, 4KB limit)
		if err := validateExtensions(input.Body.Extensions); err != nil {
			return nil, huma.Error400BadRequest("Invalid extensions: " + err.Error())
		}

		// Parse server JSON to extract name for permission validation
		serverJSON := []byte(input.Body.Server)
		var serverData map[string]interface{}
		if err := json.Unmarshal(serverJSON, &serverData); err != nil {
			return nil, huma.Error400BadRequest("Invalid server JSON format", err)
		}
		
		serverName, ok := serverData["name"].(string)
		if !ok || serverName == "" {
			return nil, huma.Error400BadRequest("Server name is required in server JSON")
		}

		// Verify that the token's repository matches the server being published
		if !jwtManager.HasPermission(serverName, auth.PermissionActionPublish, claims.Permissions) {
			return nil, huma.Error403Forbidden("You do not have permission to publish this server")
		}

		// Publish the server with separated server.json and extensions
		record, err := registry.Publish(serverJSON, input.Body.Extensions)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to publish server", err)
		}

		// Create wrapper response format
		response := model.ServerResponse{
			Server:     record.ServerJSON,
			Extensions: map[string]interface{}{
				"x-io.modelcontextprotocol.registry": record.RegistryMetadata,
			},
		}
		
		// Add publisher extensions if present
		for key, value := range record.PublisherExtensions {
			response.Extensions[key] = value
		}

		return &Response[model.ServerResponse]{
			Body: response,
		}, nil
	})
}
