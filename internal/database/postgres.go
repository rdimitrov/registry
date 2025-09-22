package database

import (
	"context"
	"fmt"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// PostgreSQL stub - needs proper implementation later
type PostgreSQL struct{}

func NewPostgreSQL(ctx context.Context, connectionURI string) (*PostgreSQL, error) {
	return nil, fmt.Errorf("PostgreSQL implementation not yet updated for ServerResponse pattern")
}

func (db *PostgreSQL) List(ctx context.Context, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return nil, "", fmt.Errorf("not implemented")
}

func (db *PostgreSQL) GetByVersionID(ctx context.Context, versionID string) (*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) GetByServerID(ctx context.Context, serverID string) (*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) GetByServerIDAndVersion(ctx context.Context, serverID string, version string) (*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) GetAllVersionsByServerID(ctx context.Context, serverID string) ([]*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) CreateServer(ctx context.Context, server *apiv0.ServerJSON, serverID, versionID string, isLatest bool) (*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) UpdateServer(ctx context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) UpdateServerStatus(ctx context.Context, versionID string, status string) (*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) UpdateIsLatest(ctx context.Context, versionID string, isLatest bool) (*apiv0.ServerResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (db *PostgreSQL) WithPublishLock(ctx context.Context, serverName string, fn func(ctx context.Context) error) error {
	return fmt.Errorf("not implemented")
}

func (db *PostgreSQL) Close() error {
	return fmt.Errorf("not implemented")
}