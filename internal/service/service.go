package service

import "github.com/modelcontextprotocol/registry/internal/model"

// RegistryService defines the interface for registry operations
type RegistryService interface {
	List(cursor string, limit int) ([]*model.ServerRecord, string, error)
	GetByID(id string) (*model.ServerRecord, error)
	Publish(serverJSON []byte, publisherExtensions map[string]interface{}) (*model.ServerRecord, error)
}
