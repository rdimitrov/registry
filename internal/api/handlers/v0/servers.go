package v0

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ListServersInput represents the input for listing servers
type ListServersInput struct {
	Cursor       string `query:"cursor" doc:"Pagination cursor (server name)" required:"false" example:"io.github.user/server-name"`
	Limit        int    `query:"limit" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince string `query:"updated_since" doc:"Filter servers updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search       string `query:"search" doc:"Search servers by name (substring match)" required:"false" example:"filesystem"`
	Version      string `query:"version" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
}

// ServerDetailInput represents the input for getting latest version of a server
type ServerDetailInput struct {
	ServerName string `path:"server_name" doc:"Server name (e.g., 'io.github.user/server-name')" example:"io.github.domdomegg/filesystem"`
}

// ServerVersionsInput represents the input for listing all versions of a server
type ServerVersionsInput struct {
	ServerName string `path:"server_name" doc:"Server name (e.g., 'io.github.user/server-name')" example:"io.github.domdomegg/filesystem"`
}

// ServerVersionDetailInput represents the input for getting a specific version
type ServerVersionDetailInput struct {
	ServerName string `path:"server_name" doc:"Server name (e.g., 'io.github.user/server-name')" example:"io.github.domdomegg/filesystem"`
	Version    string `path:"version" doc:"Specific version to retrieve (e.g., '1.0.0')" example:"1.0.0"`
}

// UpdateServerStatusInput represents the input for updating server status (author only)
type UpdateServerStatusInput struct {
	ServerName string `path:"server_name" doc:"Server name (e.g., 'io.github.user/server-name')" example:"io.github.domdomegg/filesystem"`
	Version    string `path:"version" doc:"Version to update (e.g., '1.0.0')" example:"1.0.0"`
	Status     string `json:"status" doc:"New status (active or deprecated)" example:"deprecated" enum:"active,deprecated"`
}

// EditServerInput represents the input for editing server data (admin only)
type EditServerInput struct {
	ServerName string            `path:"server_name" doc:"Server name (e.g., 'io.github.user/server-name')" example:"io.github.domdomegg/filesystem"`
	Version    string            `path:"version" doc:"Version to edit (e.g., '1.0.0')" example:"1.0.0"`
	Server     apiv0.ServerJSON `json:"server" doc:"Updated server data"`
	Status     *string           `json:"status,omitempty" doc:"Optional: update status (active, deprecated, deleted)" example:"deprecated" enum:"active,deprecated,deleted"`
}

// RegisterServersEndpoints registers all server-related endpoints
func RegisterServersEndpoints(api huma.API, registry service.RegistryService) {
	// List servers endpoint
	huma.Register(api, huma.Operation{
		OperationID: "list-servers",
		Method:      http.MethodGet,
		Path:        "/v0/servers",
		Summary:     "List MCP servers",
		Description: "Get a paginated list of MCP servers from the registry",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *ListServersInput) (*Response[apiv0.ServerListResponse], error) {
		// Build filter from input parameters
		filter := &database.ServerFilter{}

		// Parse updated_since parameter
		if input.UpdatedSince != "" {
			// Parse RFC3339 format
			if updatedTime, err := time.Parse(time.RFC3339, input.UpdatedSince); err == nil {
				filter.UpdatedSince = &updatedTime
			} else {
				return nil, huma.Error400BadRequest("Invalid updated_since format: expected RFC3339 timestamp (e.g., 2025-08-07T13:15:04.280Z)")
			}
		}

		// Handle search parameter
		if input.Search != "" {
			filter.SubstringName = &input.Search
		}

		// Handle version parameter
		if input.Version != "" {
			if input.Version == "latest" {
				// Special case: filter for latest versions
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				// Future: exact version matching
				filter.Version = &input.Version
			}
		}

		// Get paginated results with filtering
		servers, nextCursor, err := registry.List(filter, input.Cursor, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get registry list", err)
		}

		// Convert from []*ServerResponse to []ServerResponse for API response
		serverList := make([]apiv0.ServerResponse, len(servers))
		for i, server := range servers {
			serverList[i] = *server
		}

		return &Response[apiv0.ServerListResponse]{
			Body: apiv0.ServerListResponse{
				Servers: serverList,
				Metadata: apiv0.Metadata{
					NextCursor: nextCursor,
					Count:      len(serverList),
				},
			},
		}, nil
	})

	// Get server details endpoint (latest version)
	huma.Register(api, huma.Operation{
		OperationID: "get-server",
		Method:      http.MethodGet,
		Path:        "/v0/servers/{server_name}",
		Summary:     "Get latest version of MCP server",
		Description: "Get detailed information about the latest version of a specific MCP server.",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *ServerDetailInput) (*Response[apiv0.ServerResponse], error) {
		// Get latest version by server name
		serverDetail, err := registry.GetByServerName(input.ServerName)
		if err != nil {
			if err.Error() == "record not found" || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server details", err)
		}

		return &Response[apiv0.ServerResponse]{
			Body: *serverDetail,
		}, nil
	})

	// Get server versions endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server-versions",
		Method:      http.MethodGet,
		Path:        "/v0/servers/{server_name}/versions",
		Summary:     "Get all versions of an MCP server",
		Description: "Get all available versions for a specific MCP server",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *ServerVersionsInput) (*Response[apiv0.ServerListResponse], error) {
		// Get all versions for this server
		servers, err := registry.GetAllVersionsByServerName(input.ServerName)
		if err != nil {
			if err.Error() == "record not found" || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server versions", err)
		}

		// Convert from []*ServerResponse to []ServerResponse for API response
		serverList := make([]apiv0.ServerResponse, len(servers))
		for i, server := range servers {
			serverList[i] = *server
		}

		return &Response[apiv0.ServerListResponse]{
			Body: apiv0.ServerListResponse{
				Servers: serverList,
				Metadata: apiv0.Metadata{
					Count: len(serverList),
				},
			},
		}, nil
	})

	// Get specific server version endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server-version",
		Method:      http.MethodGet,
		Path:        "/v0/servers/{server_name}/versions/{version}",
		Summary:     "Get specific version of MCP server",
		Description: "Get detailed information about a specific version of an MCP server",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *ServerVersionDetailInput) (*Response[apiv0.ServerResponse], error) {
		// Get specific version by server name and version
		serverDetail, err := registry.GetByServerNameAndVersion(input.ServerName, input.Version)
		if err != nil {
			if err.Error() == "record not found" || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server version not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server version", err)
		}

		return &Response[apiv0.ServerResponse]{
			Body: *serverDetail,
		}, nil
	})

	// Update server status endpoint (author only)
	huma.Register(api, huma.Operation{
		OperationID: "update-server-status",
		Method:      http.MethodPatch,
		Path:        "/v0/servers/{server_name}/versions/{version}/status",
		Summary:     "Update server status",
		Description: "Update the status of a specific server version (author only - active ↔ deprecated)",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *UpdateServerStatusInput) (*Response[apiv0.ServerResponse], error) {
		// TODO: Add auth middleware to verify caller is the author of the server (or admin)
		// TODO: Verify namespace ownership (e.g., GitHub user owns the server name)

		// Update server status
		serverDetail, err := registry.UpdateServerStatus(input.ServerName, input.Version, input.Status)
		if err != nil {
			if err.Error() == "record not found" || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server version not found")
			}
			return nil, huma.Error500InternalServerError("Failed to update server status", err)
		}

		return &Response[apiv0.ServerResponse]{
			Body: *serverDetail,
		}, nil
	})

	// Edit server endpoint (admin only)
	huma.Register(api, huma.Operation{
		OperationID: "edit-server",
		Method:      http.MethodPut,
		Path:        "/v0/servers/{server_name}/versions/{version}",
		Summary:     "Edit server data",
		Description: "Edit server data and metadata (admin only - can edit all fields including any status transitions)",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *EditServerInput) (*Response[apiv0.ServerResponse], error) {
		// TODO: Add auth middleware to verify caller is an admin
		// TODO: Admin role verification

		// Update the server data
		serverDetail, err := registry.EditServer(input.ServerName, input.Version, input.Server)
		if err != nil {
			if err.Error() == "record not found" || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server version not found")
			}
			return nil, huma.Error500InternalServerError("Failed to edit server", err)
		}

		// Optionally update status if provided (admin can set any status)
		if input.Status != nil {
			serverDetail, err = registry.UpdateServerStatus(input.ServerName, input.Version, *input.Status)
			if err != nil {
				return nil, huma.Error500InternalServerError("Failed to update server status", err)
			}
		}

		return &Response[apiv0.ServerResponse]{
			Body: *serverDetail,
		}, nil
	})
}
