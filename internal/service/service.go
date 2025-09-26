package service

import (
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// Retrieve all servers with optional filtering
	List(filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// Retrieve latest version of a server by server name
	GetByServerName(serverName string) (*apiv0.ServerResponse, error)
	// Retrieve specific version of a server by server name and version
	GetByServerNameAndVersion(serverName string, version string) (*apiv0.ServerResponse, error)
	// Retrieve all versions of a server by server name
	GetAllVersionsByServerName(serverName string) ([]*apiv0.ServerResponse, error)
	// Publish a server
	Publish(req apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// Update server status (author only - active ↔ deprecated, cannot change to/from deleted)
	UpdateServerStatus(serverName string, version string, status string) (*apiv0.ServerResponse, error)
	// Edit server (admin only - can edit all fields including any status transitions)
	EditServer(serverName string, version string, req apiv0.ServerJSON) (*apiv0.ServerResponse, error)
}
