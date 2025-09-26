package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// PostgreSQL is an implementation of the Database interface using PostgreSQL
type PostgreSQL struct {
	pool *pgxpool.Pool
}

// Executor is an interface for executing queries (satisfied by both pgx.Tx and pgxpool.Pool)
type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// getExecutor returns the appropriate executor (transaction or pool)
func (db *PostgreSQL) getExecutor(tx pgx.Tx) Executor {
	if tx != nil {
		return tx
	}
	return db.pool
}

// NewPostgreSQL creates a new instance of the PostgreSQL database
func NewPostgreSQL(ctx context.Context, connectionURI string) (*PostgreSQL, error) {
	// Parse connection config for pool settings
	config, err := pgxpool.ParseConfig(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PostgreSQL config: %w", err)
	}

	// Configure pool for stability-focused defaults
	config.MaxConns = 30                      // Handle good concurrent load
	config.MinConns = 5                       // Keep connections warm for fast response
	config.MaxConnIdleTime = 30 * time.Minute // Keep connections available for bursts
	config.MaxConnLifetime = 2 * time.Hour    // Refresh connections regularly for stability

	// Create connection pool with configured settings
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}

	// Test the connection
	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	// Run migrations using a single connection from the pool
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection for migrations: %w", err)
	}
	defer conn.Release()

	migrator := NewMigrator(conn.Conn())
	if err := migrator.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	return &PostgreSQL{
		pool: pool,
	}, nil
}

//nolint:cyclop // Database filtering logic is inherently complex but clear
func (db *PostgreSQL) List(
	ctx context.Context,
	tx pgx.Tx,
	filter *ServerFilter,
	cursor string,
	limit int,
) ([]*apiv0.ServerResponse, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause for filtering using dedicated columns
	var whereConditions []string
	args := []any{}
	argIndex := 1

	// Add filters using dedicated columns (much faster than JSON operators)
	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_name = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_name ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("version = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("is_latest = $%d", argIndex))
			args = append(args, *filter.IsLatest)
			argIndex++
		}
	}

	// Add cursor pagination using server_name (primary key part)
	if cursor != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("server_name > $%d", argIndex))
		args = append(args, cursor)
		argIndex++
	}

	// Build the WHERE clause
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Query using dedicated columns + JSON for publisher data
	query := fmt.Sprintf(`
        SELECT server_name, version, status, published_at, updated_at, is_latest, value
        FROM servers
        %s
        ORDER BY server_name, version
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var serverName, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte

		err := rows.Scan(&serverName, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the ServerJSON (immutable publisher data) from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		// Construct ServerResponse with separated metadata
		serverResponse := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, serverResponse)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Determine next cursor using server name
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		nextCursor = lastResult.Server.Name
	}

	return results, nextCursor, nil
}

// GetByServerName retrieves the latest version of a server by server name
func (db *PostgreSQL) GetByServerName(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND is_latest = true
		LIMIT 1
	`

	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, serverName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name: %w", err)
	}

	// Parse the ServerJSON (immutable publisher data) from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Construct ServerResponse with separated metadata
	return &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

// GetByServerNameAndVersion retrieves a specific version of a server by server name and version
func (db *PostgreSQL) GetByServerNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND version = $2
		LIMIT 1
	`

	var name, ver, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, serverName, version).Scan(&name, &ver, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name and version: %w", err)
	}

	// Parse the ServerJSON (immutable publisher data) from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Construct ServerResponse with separated metadata
	return &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (db *PostgreSQL) GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string) ([]*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1
		ORDER BY published_at DESC
	`

	rows, err := db.getExecutor(tx).Query(ctx, query, serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to query server versions: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte

		err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the ServerJSON (immutable publisher data) from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		// Construct ServerResponse with separated metadata
		serverResponse := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, serverResponse)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	return results, nil
}

// CreateServer inserts a new server version using name-based approach with automatic isLatest management
func (db *PostgreSQL) CreateServer(ctx context.Context, tx pgx.Tx, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate required fields
	if serverJSON.Name == "" || serverJSON.Version == "" {
		return nil, fmt.Errorf("server must have name and version")
	}

	if officialMeta == nil {
		return nil, fmt.Errorf("server must have official metadata")
	}

	// Marshal the immutable server JSON (without status or official metadata)
	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// First, set all existing versions of this server to NOT latest (to avoid unique constraint violation)
	updateOldLatestQuery := `
		UPDATE servers
		SET is_latest = false
		WHERE server_name = $1 AND is_latest = true
	`

	_, err = db.getExecutor(tx).Exec(ctx, updateOldLatestQuery, serverJSON.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to update old latest versions: %w", err)
	}

	// Insert new version as latest (isLatest is always true for newly published versions)
	insertQuery := `
		INSERT INTO servers (server_name, version, status, published_at, updated_at, is_latest, value)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = db.getExecutor(tx).Exec(ctx, insertQuery,
		serverJSON.Name,
		serverJSON.Version,
		string(officialMeta.Status),
		officialMeta.PublishedAt,
		officialMeta.UpdatedAt,
		true, // New versions are always latest
		valueJSON,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	// Return the ServerResponse format with isLatest=true
	return &apiv0.ServerResponse{
		Server: *serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      officialMeta.Status,
				PublishedAt: officialMeta.PublishedAt,
				UpdatedAt:   officialMeta.UpdatedAt,
				IsLatest:    true, // Always true for newly published versions
			},
		},
	}, nil
}

// UpdateServerStatus updates only the status for a server (isLatest is registry-controlled)
func (db *PostgreSQL) UpdateServerStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status model.Status) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Update status and updated_at, returning all fields in one query
	query := `
		UPDATE servers
		SET status = $1, updated_at = $2
		WHERE server_name = $3 AND version = $4
		RETURNING server_name, version, status, published_at, updated_at, is_latest, value
	`

	updatedAt := time.Now()
	var name, ver, updatedStatus string
	var publishedAt, returnedUpdatedAt time.Time
	var isLatest bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query,
		string(status),
		updatedAt,
		serverName,
		version,
	).Scan(&name, &ver, &updatedStatus, &publishedAt, &returnedUpdatedAt, &isLatest, &valueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server status: %w", err)
	}

	// Parse the ServerJSON (immutable publisher data) from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Return ServerResponse with updated metadata
	return &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(updatedStatus),
				PublishedAt: publishedAt,
				UpdatedAt:   returnedUpdatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

// EditServer allows admin to update server data and metadata (including any status transitions)
func (db *PostgreSQL) EditServer(ctx context.Context, tx pgx.Tx, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate that name and version in JSON match the URL parameters
	if serverJSON.Name != serverName || serverJSON.Version != version {
		return nil, fmt.Errorf("server name/version mismatch: URL has %s/%s but JSON has %s/%s", serverName, version, serverJSON.Name, serverJSON.Version)
	}

	// Marshal the updated server JSON (immutable publisher data)
	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Update the server JSON (admin can update immutable publisher data), returning all fields
	query := `
		UPDATE servers
		SET value = $1, updated_at = $2
		WHERE server_name = $3 AND version = $4
		RETURNING server_name, version, status, published_at, updated_at, is_latest, value
	`

	updatedAt := time.Now()
	var name, ver, status string
	var publishedAt, returnedUpdatedAt time.Time
	var isLatest bool
	var returnedValueJSON []byte

	err = db.getExecutor(tx).QueryRow(ctx, query,
		valueJSON,
		updatedAt,
		serverName,
		version,
	).Scan(&name, &ver, &status, &publishedAt, &returnedUpdatedAt, &isLatest, &returnedValueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to edit server: %w", err)
	}

	// Parse the updated ServerJSON from the returned value
	var updatedServerJSON apiv0.ServerJSON
	if err := json.Unmarshal(returnedValueJSON, &updatedServerJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal updated server JSON: %w", err)
	}

	// Return ServerResponse with updated data
	return &apiv0.ServerResponse{
		Server: updatedServerJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   returnedUpdatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

// InTransaction executes a function within a database transaction
func (db *PostgreSQL) InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:contextcheck // Intentionally using separate context for rollback to ensure cleanup even if request is cancelled
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if rbErr := tx.Rollback(rollbackCtx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			log.Printf("failed to rollback transaction: %v", rbErr)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// AcquirePublishLock acquires an exclusive advisory lock for publishing a server
// This prevents race conditions when multiple versions are published concurrently
// Using pg_advisory_xact_lock which auto-releases on transaction end
func (db *PostgreSQL) AcquirePublishLock(ctx context.Context, tx pgx.Tx, serverName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	lockID := hashServerName(serverName)

	if _, err := db.getExecutor(tx).Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockID); err != nil {
		return fmt.Errorf("failed to acquire publish lock: %w", err)
	}

	return nil
}

// hashServerName creates a consistent hash of the server name for advisory locking
// We use FNV-1a hash and mask to 63 bits to fit in PostgreSQL's bigint range
func hashServerName(name string) int64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	hash := uint64(offset64)
	for i := 0; i < len(name); i++ {
		hash ^= uint64(name[i])
		hash *= prime64
	}
	//nolint:gosec // Intentional conversion with masking to 63 bits
	return int64(hash & 0x7FFFFFFFFFFFFFFF)
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}
