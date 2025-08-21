package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDB is an implementation of the Database interface using MongoDB
type MongoDB struct {
	client     *mongo.Client
	database   *mongo.Database
	collection *mongo.Collection
}

// NewMongoDB creates a new instance of the MongoDB database
func NewMongoDB(ctx context.Context, connectionURI, databaseName, collectionName string) (*MongoDB, error) {
	// Set client options and connect to MongoDB
	clientOptions := options.Client().ApplyURI(connectionURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	// Ping the MongoDB server to verify the connection
	if err = client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	// Get database and collection
	database := client.Database(databaseName)
	collection := database.Collection(collectionName)

	// Create indexes for better query performance
	models := []mongo.IndexModel{
		{
			// Index on registry metadata ID for individual server lookups and pagination
			Keys:    bson.D{bson.E{Key: "registry_metadata._id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			// Index on is_latest for filtering current versions
			Keys: bson.D{bson.E{Key: "registry_metadata.is_latest", Value: 1}},
		},
		{
			// Compound index for is_latest + ID for efficient pagination
			Keys: bson.D{
				bson.E{Key: "registry_metadata.is_latest", Value: 1},
				bson.E{Key: "registry_metadata._id", Value: 1},
			},
		},
		{
			// Index on server name for lookups
			Keys: bson.D{bson.E{Key: "server_json.name", Value: 1}},
		},
		{
			// Unique index on name + version for preventing duplicates
			Keys: bson.D{
				bson.E{Key: "server_json.name", Value: 1},
				bson.E{Key: "server_json.version_detail.version", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
	}

	_, err = collection.Indexes().CreateMany(ctx, models)
	if err != nil {
		// Mongo will error if the index already exists, we can ignore this and continue.
		var commandError mongo.CommandError
		if errors.As(err, &commandError) && commandError.Code != 86 {
			return nil, err
		}
		log.Printf("Indexes already exists, skipping.")
	}

	return &MongoDB{
		client:     client,
		database:   database,
		collection: collection,
	}, nil
}

// List retrieves MCPRegistry entries with optional filtering and pagination
func (db *MongoDB) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*model.ServerRecord, string, error) {
	if limit <= 0 {
		// Set default limit if not provided
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Convert Go map to MongoDB filter
	mongoFilter := bson.M{
		"registry_metadata.is_latest": true,
	}
	// Map common filter keys to MongoDB document paths
	for k, v := range filter {
		// Handle nested fields with dot notation
		switch k {
		case "version":
			mongoFilter["server_json.version_detail.version"] = v
		case "name":
			mongoFilter["server_json.name"] = v
		default:
			mongoFilter[k] = v
		}
	}

	// Setup pagination options
	findOptions := options.Find()

	// If cursor is provided, add condition to filter to only get records after the cursor
	if cursor != "" {
		// Validate that the cursor is a valid UUID
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}

		// Fetch the document at the cursor to get its sort values
		var cursorDoc model.ServerRecord
		err := db.collection.FindOne(ctx, bson.M{"registry_metadata._id": cursor}).Decode(&cursorDoc)
		if err != nil {
			if !errors.Is(err, mongo.ErrNoDocuments) {
				return nil, "", err
			}
			// If cursor document not found, start from beginning
		} else {
			// Use the cursor document's ID to paginate (records with ID > cursor's ID)
			mongoFilter["registry_metadata._id"] = bson.M{"$gt": cursor}
		}
	}

	// Set sort order by ID (for consistent pagination)
	findOptions.SetSort(bson.M{"registry_metadata._id": 1})

	// Set limit if provided and valid
	if limit > 0 {
		findOptions.SetLimit(int64(limit))
	}

	// Execute find operation with options
	mongoCursor, err := db.collection.Find(ctx, mongoFilter, findOptions)
	if err != nil {
		return nil, "", err
	}
	defer mongoCursor.Close(ctx)

	// Decode results
	var results []*model.ServerRecord
	if err = mongoCursor.All(ctx, &results); err != nil {
		return nil, "", err
	}

	// Determine the next cursor
	nextCursor := ""
	if len(results) > 0 && limit > 0 && len(results) >= limit {
		// Use the last item's ID as the next cursor
		nextCursor = results[len(results)-1].RegistryMetadata.ID
	}

	return results, nextCursor, nil
}

// GetByID retrieves a single ServerRecord by its ID
func (db *MongoDB) GetByID(ctx context.Context, id string) (*model.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Create a filter for the registry metadata ID
	filter := bson.M{"registry_metadata._id": id}

	// Find the entry in the database
	var entry model.ServerRecord
	err := db.collection.FindOne(ctx, filter).Decode(&entry)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("error retrieving entry: %w", err)
	}

	// Return the ServerRecord
	return &entry, nil
}

// Publish adds a new server to the database with separated server.json and extensions
func (db *MongoDB) Publish(ctx context.Context, serverJSON []byte, publisherExtensions map[string]interface{}) (*model.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Parse serverJSON to extract name and version
	var serverData map[string]interface{}
	if err := json.Unmarshal(serverJSON, &serverData); err != nil {
		return nil, fmt.Errorf("invalid server JSON: %w", err)
	}
	
	// Extract name
	name, ok := serverData["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required in server JSON")
	}
	
	// Extract version
	versionDetail, ok := serverData["version_detail"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("version_detail is required in server JSON")
	}
	
	version, ok := versionDetail["version"].(string)
	if !ok || version == "" {
		return nil, fmt.Errorf("version is required in version_detail")
	}

	// Check for existing entry with same name
	filter := bson.M{
		"server_json.name":            name,
		"registry_metadata.is_latest": true,
	}

	var existingEntry model.ServerRecord
	err := db.collection.FindOne(ctx, filter).Decode(&existingEntry)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("error checking existing entry: %w", err)
	}

	// Version comparison logic (if existing entry found)
	if existingEntry.RegistryMetadata.ID != "" {
		var existingServerData map[string]interface{}
		if err := json.Unmarshal(existingEntry.ServerJSON, &existingServerData); err == nil {
			if existingVersionDetail, ok := existingServerData["version_detail"].(map[string]interface{}); ok {
				if existingVersion, ok := existingVersionDetail["version"].(string); ok {
					if version <= existingVersion {
						return nil, fmt.Errorf("version must be greater than existing version %s", existingVersion)
					}
				}
			}
		}
	}

	// Create new registry metadata
	now := time.Now()
	registryMetadata := model.RegistryMetadata{
		ID:          uuid.New().String(),
		PublishedAt: now,
		UpdatedAt:   now,
		IsLatest:    true,
		ReleaseDate: now.Format(time.RFC3339),
	}

	// Create server record
	record := &model.ServerRecord{
		ServerJSON:          serverJSON,
		RegistryMetadata:    registryMetadata,
		PublisherExtensions: publisherExtensions,
	}

	// Insert the new record
	_, err = db.collection.InsertOne(ctx, record)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("error inserting entry: %w", err)
	}

	// Update existing entry to not be latest
	if existingEntry.RegistryMetadata.ID != "" {
		_, err = db.collection.UpdateOne(
			ctx,
			bson.M{"registry_metadata._id": existingEntry.RegistryMetadata.ID},
			bson.M{"$set": bson.M{"registry_metadata.is_latest": false}})
		if err != nil {
			return nil, fmt.Errorf("error updating existing entry: %w", err)
		}
	}

	return record, nil
}

// ImportSeed imports initial data from a seed file into MongoDB
func (db *MongoDB) ImportSeed(ctx context.Context, seedFilePath string) error {
	// Read the migrated seed data (should be in ServerRecord format)
	seedRecords, err := ReadSeedFile(ctx, seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	// Clear existing data
	_, err = db.collection.DeleteMany(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to clear existing data: %w", err)
	}

	// Insert all seed records
	if len(seedRecords) > 0 {
		// Convert to interface{} slice for insertion
		docs := make([]interface{}, len(seedRecords))
		for i, record := range seedRecords {
			docs[i] = record
		}
		
		_, err = db.collection.InsertMany(ctx, docs)
		if err != nil {
			return fmt.Errorf("failed to insert seed data: %w", err)
		}
	}

	log.Printf("Successfully imported %d servers from seed file", len(seedRecords))
	return nil
}
// Close closes the database connection
func (db *MongoDB) Close() error {
	return db.client.Disconnect(context.Background())
}

// Connection returns information about the database connection
func (db *MongoDB) Connection() *ConnectionInfo {
	isConnected := false
	// Check if the client is connected
	if db.client != nil {
		// A quick ping with 1 second timeout to verify connection
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := db.client.Ping(ctx, nil)
		isConnected = (err == nil)
	}

	return &ConnectionInfo{
		Type:        ConnectionTypeMongoDB,
		IsConnected: isConnected,
		Raw:         db.client,
	}
}
