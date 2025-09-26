package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxServerVersionsPerServer = 10000

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db  database.Database
	cfg *config.Config
}

// NewRegistryService creates a new registry service with the provided database
func NewRegistryService(db database.Database, cfg *config.Config) RegistryService {
	return &registryServiceImpl{
		db:  db,
		cfg: cfg,
	}
}

// List returns registry entries with cursor-based pagination and optional filtering
func (s *registryServiceImpl) List(filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's List method with pagination and filtering
	return s.db.List(ctx, nil, filter, cursor, limit)
}

// GetByServerName retrieves the latest version of a server by its server name
func (s *registryServiceImpl) GetByServerName(serverName string) (*apiv0.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.db.GetByServerName(ctx, nil, serverName)
}

// GetByServerNameAndVersion retrieves a specific version of a server by server name and version
func (s *registryServiceImpl) GetByServerNameAndVersion(serverName string, version string) (*apiv0.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.db.GetByServerNameAndVersion(ctx, nil, serverName, version)
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (s *registryServiceImpl) GetAllVersionsByServerName(serverName string) ([]*apiv0.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.db.GetAllVersionsByServerName(ctx, nil, serverName)
}

// UpdateServerStatus updates only the status for a server (author only - active ↔ deprecated)
func (s *registryServiceImpl) UpdateServerStatus(serverName string, version string, status string) (*apiv0.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate status value
	var modelStatus model.Status
	switch status {
	case "active":
		modelStatus = model.StatusActive
	case "deprecated":
		modelStatus = model.StatusDeprecated
	default:
		return nil, fmt.Errorf("invalid status: %s (allowed: active, deprecated)", status)
	}

	return database.InTransactionT(ctx, s.db, func(txCtx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		// Get current server to check existing status
		currentServer, err := s.db.GetByServerNameAndVersion(txCtx, tx, serverName, version)
		if err != nil {
			return nil, err
		}

		// Business logic: cannot change FROM deleted
		if currentServer.Meta.Official != nil && currentServer.Meta.Official.Status == model.StatusDeleted {
			return nil, fmt.Errorf("cannot change status from deleted")
		}

		// Author can only toggle between active and deprecated
		if modelStatus != model.StatusActive && modelStatus != model.StatusDeprecated {
			return nil, fmt.Errorf("authors can only set status to active or deprecated")
		}

		// Update status in database
		return s.db.UpdateServerStatus(txCtx, tx, serverName, version, modelStatus)
	})
}

// EditServer allows admin to update server data and metadata (including any status transitions)
func (s *registryServiceImpl) EditServer(serverName string, version string, req apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req, s.cfg); err != nil {
		return nil, err
	}

	return database.InTransactionT(ctx, s.db, func(txCtx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		// Get the current server to preserve metadata
		currentServer, err := s.db.GetByServerNameAndVersion(txCtx, tx, serverName, version)
		if err != nil {
			return nil, err
		}

		// Acquire advisory lock to prevent concurrent edits of servers with same name
		if err := s.db.AcquirePublishLock(txCtx, tx, currentServer.Server.Name); err != nil {
			return nil, err
		}

		// Check for duplicate remote URLs using the updated server
		if err := s.validateNoDuplicateRemoteURLs(txCtx, tx, req, serverName); err != nil {
			return nil, err
		}

		// Update server in database (admin can edit all fields)
		return s.db.EditServer(txCtx, tx, serverName, version, &req)
	})
}

// Publish publishes a server with sophisticated version comparison logic
func (s *registryServiceImpl) Publish(req apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req, s.cfg); err != nil {
		return nil, err
	}

	return database.InTransactionT(ctx, s.db, func(txCtx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		publishTime := time.Now()
		serverJSON := req

		// Acquire advisory lock to prevent concurrent publishes of the same server
		if err := s.db.AcquirePublishLock(txCtx, tx, serverJSON.Name); err != nil {
			return nil, err
		}

		// Check for duplicate remote URLs
		if err := s.validateNoDuplicateRemoteURLs(txCtx, tx, serverJSON, ""); err != nil {
			return nil, err
		}

		// Get existing versions for this server name
		existingServerVersions, err := s.db.GetAllVersionsByServerName(txCtx, tx, serverJSON.Name)
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return nil, err
		}

		// Check we haven't exceeded the maximum versions allowed for a server
		if len(existingServerVersions) >= maxServerVersionsPerServer {
			return nil, database.ErrMaxServersReached
		}

		// Check this isn't a duplicate version
		for _, serverResponse := range existingServerVersions {
			if serverResponse.Server.Version == serverJSON.Version {
				return nil, database.ErrInvalidVersion
			}
		}

		// Determine if this version should be marked as latest using sophisticated version comparison
		currentLatest := s.getCurrentLatestVersion(existingServerVersions)
		isNewLatest := true
		if currentLatest != nil {
			var currentLatestPublishedAt time.Time
			if currentLatest.Meta.Official != nil {
				currentLatestPublishedAt = currentLatest.Meta.Official.PublishedAt
			}
			// Use sophisticated version comparison (semver-aware)
			isNewLatest = CompareVersions(
				req.Version,
				currentLatest.Server.Version,
				publishTime,
				currentLatestPublishedAt,
			) > 0
		}

		// Create registry metadata
		officialMeta := &apiv0.RegistryExtensions{
			Status:      model.StatusActive, // Default status for new publications
			PublishedAt: publishTime,
			UpdatedAt:   publishTime,
			IsLatest:    isNewLatest,
		}

		// Database layer handles isLatest field management automatically
		// No need to manually unmark previous latest - CreateServer handles this
		return s.db.CreateServer(txCtx, tx, &serverJSON, officialMeta)
	})
}

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs
func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, tx pgx.Tx, serverDetail apiv0.ServerJSON, skipServerName string) error {
	// Check each remote URL in the new server for conflicts
	for _, remote := range serverDetail.Remotes {
		// Use filter to find servers with this remote URL
		filter := &database.ServerFilter{RemoteURL: &remote.URL}

		conflictingServers, _, err := s.db.List(ctx, tx, filter, "", 1000)
		if err != nil {
			return fmt.Errorf("failed to check remote URL conflict: %w", err)
		}

		// Check if any conflicting server has a different name
		for _, conflictingServer := range conflictingServers {
			if conflictingServer.Server.Name != serverDetail.Name && conflictingServer.Server.Name != skipServerName {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Server.Name)
			}
		}
	}

	return nil
}

// getCurrentLatestVersion finds the current latest version from existing server versions
func (s *registryServiceImpl) getCurrentLatestVersion(existingServerVersions []*apiv0.ServerResponse) *apiv0.ServerResponse {
	for _, serverResponse := range existingServerVersions {
		if serverResponse.Meta.Official != nil && serverResponse.Meta.Official.IsLatest {
			return serverResponse
		}
	}
	return nil
}