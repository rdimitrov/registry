package v0

import (
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// RegistryExtensions represents registry-generated metadata
type RegistryExtensions struct {
	ServerID    string    `json:"serverId"`  // Consistent ID across all versions of a server
	VersionID   string    `json:"versionId"` // Unique ID for this specific version
	Status      model.Status `json:"status"`
	PublishedAt time.Time `json:"publishedAt"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
	IsLatest    bool      `json:"isLatest"`
}

// ResponseMeta represents the registry-managed metadata structure
type ResponseMeta struct {
	Official *RegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// ServerResponse represents the API response format with separated server.json and registry metadata
type ServerResponse struct {
	Server ServerJSON  `json:"server"` // Immutable server configuration
	Meta   ResponseMeta `json:"_meta"`  // Registry-managed metadata
}

// ServerListResponse represents the paginated server list response
type ServerListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata Metadata         `json:"metadata"`
}

// ServerMeta represents the structured metadata with known extension fields
type ServerMeta struct {
	Official          *RegistryExtensions    `json:"io.modelcontextprotocol.registry/official,omitempty"`
	PublisherProvided map[string]interface{} `json:"io.modelcontextprotocol.registry/publisher-provided,omitempty"`
}

// ServerJSON represents complete server information as defined in the MCP spec, with extension support
// Note: Status is now part of registry metadata, not server configuration
type ServerJSON struct {
	Schema      string            `json:"$schema,omitempty"`
	Name        string            `json:"name" minLength:"1" maxLength:"200"`
	Description string            `json:"description" minLength:"1" maxLength:"100"`
	Repository  model.Repository  `json:"repository,omitempty"`
	Version     string            `json:"version"`
	WebsiteURL  string            `json:"websiteUrl,omitempty"`
	Packages    []model.Package   `json:"packages,omitempty"`
	Remotes     []model.Transport `json:"remotes,omitempty"`
	Meta        *ServerMeta       `json:"_meta,omitempty"`
}

// Metadata represents pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count"`
}

func (s *ServerJSON) GetServerID() string {
	if s.Meta != nil && s.Meta.Official != nil {
		return s.Meta.Official.ServerID
	}
	return ""
}

func (s *ServerJSON) GetVersionID() string {
	if s.Meta != nil && s.Meta.Official != nil {
		return s.Meta.Official.VersionID
	}
	return ""
}

// Helper methods for ServerResponse
func (sr *ServerResponse) GetServerID() string {
	if sr.Meta.Official != nil {
		return sr.Meta.Official.ServerID
	}
	return ""
}

func (sr *ServerResponse) GetVersionID() string {
	if sr.Meta.Official != nil {
		return sr.Meta.Official.VersionID
	}
	return ""
}
