package validators

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/config"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func ValidateServerJSON(serverJSON *apiv0.ServerJSON) error {
	// Validate server name exists and format
	if _, err := parseServerName(*serverJSON); err != nil {
		return err
	}

	// Validate repository
	if err := validateRepository(&serverJSON.Repository); err != nil {
		return err
	}

	// Validate all packages (basic field validation)
	// Detailed package validation (including registry checks) is done during publish
	for _, pkg := range serverJSON.Packages {
		if err := validatePackageField(&pkg); err != nil {
			return err
		}
	}

	// Validate all remotes
	for _, remote := range serverJSON.Remotes {
		if err := validateRemote(&remote); err != nil {
			return err
		}
	}

	// Validate reverse-DNS namespace matching for remote URLs
	if err := validateRemoteNamespaceMatch(*serverJSON); err != nil {
		return err
	}

	return nil
}

func validateRepository(obj *model.Repository) error {
	// Skip validation for empty repository (optional field)
	if obj.URL == "" && obj.Source == "" {
		return nil
	}

	// validate the repository source
	repoSource := RepositorySource(obj.Source)
	if !IsValidRepositoryURL(repoSource, obj.URL) {
		return fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL)
	}

	return nil
}

func validatePackageField(obj *model.Package) error {
	if !HasNoSpaces(obj.Identifier) {
		return ErrPackageNameHasSpaces
	}

	// Validate registry type
	if err := validateRegistryType(obj.RegistryType); err != nil {
		return err
	}

	// Validate registry base URL matches registry type
	if err := validateRegistryBaseURL(obj.RegistryType, obj.RegistryBaseURL); err != nil {
		return err
	}

	// Validate MCPB package identifiers must be valid release URLs
	if err := validateMCPBIdentifier(obj.RegistryType, obj.RegistryBaseURL, obj.Identifier); err != nil {
		return err
	}

	// Validate runtime arguments
	for _, arg := range obj.RuntimeArguments {
		if err := validateArgument(&arg); err != nil {
			return fmt.Errorf("invalid runtime argument: %w", err)
		}
	}

	// Validate package arguments
	for _, arg := range obj.PackageArguments {
		if err := validateArgument(&arg); err != nil {
			return fmt.Errorf("invalid package argument: %w", err)
		}
	}

	// Validate transport type with template variable support
	availableVariables := collectAvailableVariables(obj)
	if err := validatePackageTransportType(obj.TransportType, availableVariables); err != nil {
		return fmt.Errorf("invalid transport type: %w", err)
	}

	return nil
}

// validateArgument validates argument details
func validateArgument(obj *model.Argument) error {
	if obj.Type == model.ArgumentTypeNamed {
		// Validate named argument name format
		if err := validateNamedArgumentName(obj.Name); err != nil {
			return err
		}

		// Validate value and default don't start with the name
		if err := validateArgumentValueFields(obj.Name, obj.Value, obj.Default); err != nil {
			return err
		}
	}
	return nil
}

func validateNamedArgumentName(name string) error {
	// Check if name is required for named arguments
	if name == "" {
		return ErrNamedArgumentNameRequired
	}

	// Check for invalid characters that suggest embedded values or descriptions
	// Valid: "--directory", "--port", "-v", "config", "verbose"
	// Invalid: "--directory <absolute_path_to_adfin_mcp_folder>", "--port 8080"
	if strings.Contains(name, "<") || strings.Contains(name, ">") ||
		strings.Contains(name, " ") || strings.Contains(name, "$") {
		return fmt.Errorf("%w: %s", ErrInvalidNamedArgumentName, name)
	}

	return nil
}

func validateArgumentValueFields(name, value, defaultValue string) error {
	// Check if value starts with the argument name (using startsWith, not contains)
	if value != "" && strings.HasPrefix(value, name) {
		return fmt.Errorf("%w: value starts with argument name '%s': %s", ErrArgumentValueStartsWithName, name, value)
	}

	if defaultValue != "" && strings.HasPrefix(defaultValue, name) {
		return fmt.Errorf("%w: default starts with argument name '%s': %s", ErrArgumentDefaultStartsWithName, name, defaultValue)
	}

	return nil
}

// collectAvailableVariables collects all available template variables from a package
func collectAvailableVariables(pkg *model.Package) []string {
	var variables []string
	
	// Add environment variable names
	for _, env := range pkg.EnvironmentVariables {
		variables = append(variables, env.Name)
	}
	
	// Add runtime argument names and value hints
	for _, arg := range pkg.RuntimeArguments {
		if arg.Name != "" {
			variables = append(variables, arg.Name)
		}
		if arg.ValueHint != "" {
			variables = append(variables, arg.ValueHint)
		}
	}
	
	// Add package argument names and value hints
	for _, arg := range pkg.PackageArguments {
		if arg.Name != "" {
			variables = append(variables, arg.Name)
		}
		if arg.ValueHint != "" {
			variables = append(variables, arg.ValueHint)
		}
	}
	
	return variables
}

// validatePackageTransportType validates transport type for packages (allows templates)
func validatePackageTransportType(transport model.TransportTypeConfig, availableVariables []string) error {
	// Validate transport type is supported
	switch transport.Type {
	case model.TransportTypeStdio:
		// No additional validation needed for stdio
		return nil
	case model.TransportTypeStreamableHTTP:
		// URL is required for streamable-http
		if transport.URL == "" {
			return fmt.Errorf("url is required for %s transport type", model.TransportTypeStreamableHTTP)
		}
		// Validate URL format with template variable support
		if !IsValidTemplatedURL(transport.URL, availableVariables, true) {
			// Check if it's a template variable issue or basic URL issue
			templateVars := extractTemplateVariables(transport.URL)
			if len(templateVars) > 0 {
				return fmt.Errorf("%w: template variables in URL %s reference undefined variables. Available variables: %v", 
					ErrInvalidRemoteURL, transport.URL, availableVariables)
			}
			return fmt.Errorf("%w: %s", ErrInvalidRemoteURL, transport.URL)
		}
		return nil
	default:
		return fmt.Errorf("unsupported transport type: %s", transport.Type)
	}
}

// validateRemoteTransportType validates transport type for remotes (only streamable-http allowed)
func validateRemoteTransportType(transport model.TransportTypeConfig) error {
	// Validate transport type is supported - remotes only support streamable-http
	switch transport.Type {
	case model.TransportTypeStreamableHTTP:
		// URL is required for streamable-http
		if transport.URL == "" {
			return fmt.Errorf("url is required for %s transport type", model.TransportTypeStreamableHTTP)
		}
		// Validate URL format without template variable support
		if !IsValidTemplatedURL(transport.URL, nil, false) {
			// Check if it contains templates (which are not allowed)
			templateVars := extractTemplateVariables(transport.URL)
			if len(templateVars) > 0 {
				return fmt.Errorf("%w: template variables are not supported in remote URLs: %s", 
					ErrInvalidRemoteURL, transport.URL)
			}
			return fmt.Errorf("%w: %s", ErrInvalidRemoteURL, transport.URL)
		}
		return nil
	default:
		return fmt.Errorf("unsupported transport type: %s", transport.Type)
	}
}

func validateRemote(obj *model.Remote) error {
	return validateRemoteTransportType(obj.TransportType)
}

// ValidatePublishRequest validates a complete publish request including extensions
func ValidatePublishRequest(req apiv0.ServerJSON, cfg *config.Config) error {
	// Validate publisher extensions in _meta
	if err := validatePublisherExtensions(req); err != nil {
		return err
	}

	// Validate the server detail (includes all nested validation)
	if err := ValidateServerJSON(&req); err != nil {
		return err
	}

	// Validate registry ownership for all packages if validation is enabled and server is not deleted
	if cfg.EnableRegistryValidation && req.Status != model.StatusDeleted {
		ctx := context.Background()
		for i, pkg := range req.Packages {
			if err := ValidatePackage(ctx, pkg, req.Name); err != nil {
				return fmt.Errorf("registry validation failed for package %d (%s): %w", i, pkg.Identifier, err)
			}
		}
	}

	return nil
}

func validatePublisherExtensions(req apiv0.ServerJSON) error {
	const maxExtensionSize = 4 * 1024 // 4KB limit

	// Check size limit for _meta.publisher extension
	if req.Meta != nil && req.Meta.Publisher != nil {
		extensionsJSON, err := json.Marshal(req.Meta.Publisher)
		if err != nil {
			return fmt.Errorf("failed to marshal _meta.publisher extension: %w", err)
		}
		if len(extensionsJSON) > maxExtensionSize {
			return fmt.Errorf("_meta.publisher extension exceeds 4KB limit (%d bytes)", len(extensionsJSON))
		}
	}

	if req.Meta != nil {
		// Validate that only "publisher" is allowed in _meta during publish (no registry metadata should be present)
		if req.Meta.IOModelContextProtocolRegistry != nil {
			return fmt.Errorf("registry metadata '_meta.io.modelcontextprotocol.registry' is not allowed during publish")
		}
	}

	return nil
}

func parseServerName(serverJSON apiv0.ServerJSON) (string, error) {
	name := serverJSON.Name
	if name == "" {
		return "", fmt.Errorf("server name is required and must be a string")
	}

	// Validate format: dns-namespace/name
	if !strings.Contains(name, "/") {
		return "", fmt.Errorf("server name must be in format 'dns-namespace/name' (e.g., 'com.example.api/server')")
	}

	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("server name must be in format 'dns-namespace/name' with non-empty namespace and name parts")
	}

	return name, nil
}

// validateRemoteNamespaceMatch validates that remote URLs match the reverse-DNS namespace
func validateRemoteNamespaceMatch(serverJSON apiv0.ServerJSON) error {
	namespace := serverJSON.Name

	for _, remote := range serverJSON.Remotes {
		// Only validate if remote has a URL (streamable-http transport)
		if remote.TransportType.URL != "" {
			if err := validateRemoteURLMatchesNamespace(remote.TransportType.URL, namespace); err != nil {
				return fmt.Errorf("remote URL %s does not match namespace %s: %w", remote.TransportType.URL, namespace, err)
			}
		}
	}

	return nil
}

// validateRemoteURLMatchesNamespace checks if a remote URL's hostname matches the publisher domain from the namespace
func validateRemoteURLMatchesNamespace(remoteURL, namespace string) error {
	// Parse the URL to extract the hostname
	parsedURL, err := url.Parse(remoteURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a valid hostname")
	}

	// Skip validation for localhost and local development URLs
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || hostname == "127.0.0.1" {
		return nil
	}

	// Extract publisher domain from reverse-DNS namespace
	publisherDomain := extractPublisherDomainFromNamespace(namespace)
	if publisherDomain == "" {
		return fmt.Errorf("invalid namespace format: cannot extract domain from %s", namespace)
	}

	// Check if the remote URL hostname matches the publisher domain or is a subdomain
	if !isValidHostForDomain(hostname, publisherDomain) {
		return fmt.Errorf("remote URL host %s does not match publisher domain %s", hostname, publisherDomain)
	}

	return nil
}

// extractPublisherDomainFromNamespace converts reverse-DNS namespace to normal domain format
// e.g., "com.example" -> "example.com"
func extractPublisherDomainFromNamespace(namespace string) string {
	// Extract the namespace part before the first slash
	namespacePart := namespace
	if slashIdx := strings.Index(namespace, "/"); slashIdx != -1 {
		namespacePart = namespace[:slashIdx]
	}

	// Split into parts and reverse them to get normal domain format
	parts := strings.Split(namespacePart, ".")
	if len(parts) < 2 {
		return ""
	}

	// Reverse the parts to convert from reverse-DNS to normal domain
	slices.Reverse(parts)

	return strings.Join(parts, ".")
}

// isValidHostForDomain checks if a hostname is the domain or a subdomain of the publisher domain
func isValidHostForDomain(hostname, publisherDomain string) bool {
	// Exact match
	if hostname == publisherDomain {
		return true
	}

	// Subdomain match - hostname should end with "." + publisherDomain
	if strings.HasSuffix(hostname, "."+publisherDomain) {
		return true
	}

	return false
}

// validateRegistryType validates that the registry type is supported
func validateRegistryType(registryType string) error {
	if registryType == "" {
		return fmt.Errorf("%w: registry type is required", ErrUnsupportedRegistryType)
	}

	// Check if registry type is supported
	switch registryType {
	case model.RegistryTypeNPM, model.RegistryTypePyPI, model.RegistryTypeOCI, model.RegistryTypeNuGet, model.RegistryTypeMCPB:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedRegistryType, registryType)
	}
}

// validateRegistryBaseURL validates that the registry base URL matches the registry type
func validateRegistryBaseURL(registryType, registryBaseURL string) error {
	// Empty base URL is allowed - it will be inferred
	if registryBaseURL == "" {
		return nil
	}

	// Check if the base URL matches the registry type
	switch registryType {
	case model.RegistryTypeNPM:
		if registryBaseURL != model.RegistryURLNPM {
			return fmt.Errorf("%w: registry type '%s' requires base URL '%s', got '%s'", 
				ErrMismatchedRegistryTypeAndURL, registryType, model.RegistryURLNPM, registryBaseURL)
		}
	case model.RegistryTypePyPI:
		if registryBaseURL != model.RegistryURLPyPI {
			return fmt.Errorf("%w: registry type '%s' requires base URL '%s', got '%s'", 
				ErrMismatchedRegistryTypeAndURL, registryType, model.RegistryURLPyPI, registryBaseURL)
		}
	case model.RegistryTypeOCI:
		if registryBaseURL != model.RegistryURLDocker {
			return fmt.Errorf("%w: registry type '%s' requires base URL '%s', got '%s'", 
				ErrMismatchedRegistryTypeAndURL, registryType, model.RegistryURLDocker, registryBaseURL)
		}
	case model.RegistryTypeNuGet:
		if registryBaseURL != model.RegistryURLNuGet {
			return fmt.Errorf("%w: registry type '%s' requires base URL '%s', got '%s'", 
				ErrMismatchedRegistryTypeAndURL, registryType, model.RegistryURLNuGet, registryBaseURL)
		}
	case model.RegistryTypeMCPB:
		// MCPB can use GitHub or GitLab URLs
		if registryBaseURL != model.RegistryURLGitHub && registryBaseURL != model.RegistryURLGitLab {
			return fmt.Errorf("%w: registry type '%s' requires base URL '%s' or '%s', got '%s'", 
				ErrMismatchedRegistryTypeAndURL, registryType, model.RegistryURLGitHub, model.RegistryURLGitLab, registryBaseURL)
		}
	default:
		// This should not happen if validateRegistryType was called first
		return fmt.Errorf("%w: %s", ErrUnsupportedRegistryType, registryType)
	}

	// Additional validation for unsupported base URLs
	if err := validateSupportedRegistryBaseURL(registryBaseURL); err != nil {
		return err
	}

	return nil
}

// validateSupportedRegistryBaseURL validates that the base URL is in the supported list and has correct format
func validateSupportedRegistryBaseURL(baseURL string) error {
	// List of all supported base URLs
	supportedURLs := []string{
		model.RegistryURLNPM,
		model.RegistryURLPyPI,
		model.RegistryURLDocker,
		model.RegistryURLNuGet,
		model.RegistryURLGitHub,
		model.RegistryURLGitLab,
	}

	for _, supportedURL := range supportedURLs {
		if baseURL == supportedURL {
			return nil
		}
	}

	// Check for common mistakes like trailing slashes
	for _, supportedURL := range supportedURLs {
		if baseURL == supportedURL+"/" {
			return fmt.Errorf("%w: base URL should not have trailing slash, use '%s' instead of '%s'", 
				ErrUnsupportedRegistryBaseURL, supportedURL, baseURL)
		}
	}

	// Check for localhost URLs - should be rejected
	if strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") {
		return fmt.Errorf("%w: localhost URLs are not allowed for registry base URL: %s", 
			ErrUnsupportedRegistryBaseURL, baseURL)
	}

	return fmt.Errorf("%w: %s", ErrUnsupportedRegistryBaseURL, baseURL)
}

// validateMCPBIdentifier validates that MCPB package identifiers are valid release URLs
func validateMCPBIdentifier(registryType, registryBaseURL, identifier string) error {
	// Only validate MCPB packages
	if registryType != model.RegistryTypeMCPB {
		return nil
	}

	// Skip validation if no identifier is provided
	if identifier == "" {
		return nil
	}

	// Auto-detect GitHub or GitLab based on the identifier URL itself
	if strings.HasPrefix(identifier, "https://github.com/") {
		return validateGitHubReleaseURL(identifier)
	}
	
	if strings.HasPrefix(identifier, "https://gitlab.com/") {
		return validateGitLabReleaseURL(identifier)
	}

	// If the identifier doesn't start with a known URL pattern but we have a registry base URL,
	// validate based on the registry base URL
	if registryBaseURL != "" {
		if registryBaseURL == model.RegistryURLGitHub {
			return validateGitHubReleaseURL(identifier)
		}
		if registryBaseURL == model.RegistryURLGitLab {
			return validateGitLabReleaseURL(identifier)
		}
	}

	// If we can't determine the type, this might be a custom URL pattern
	// For now, we'll allow it through basic validation, but this could be enhanced
	return nil
}

// validateGitHubReleaseURL validates that the identifier is a valid GitHub release asset URL
func validateGitHubReleaseURL(identifier string) error {
	// Expected format: https://github.com/owner/repo/releases/download/tag/filename
	if !strings.HasPrefix(identifier, "https://github.com/") {
		return fmt.Errorf("GitHub MCPB packages must be release assets")
	}

	// Remove the base URL part
	path := strings.TrimPrefix(identifier, "https://github.com/")
	
	// Split into parts: owner/repo/releases/download/tag/filename
	parts := strings.Split(path, "/")
	
	// Must have at least 6 parts: owner, repo, releases, download, tag, filename
	if len(parts) < 6 {
		return fmt.Errorf("GitHub MCPB packages must be release assets")
	}

	// Check that it has the correct structure
	if parts[2] != "releases" || parts[3] != "download" {
		return fmt.Errorf("GitHub MCPB packages must be release assets")
	}

	// Check that tag is not empty
	if parts[4] == "" {
		return fmt.Errorf("GitHub MCPB packages must be release assets")
	}

	// Check that filename is not empty
	if parts[5] == "" {
		return fmt.Errorf("GitHub MCPB packages must be release assets")
	}

	return nil
}

// validateGitLabReleaseURL validates that the identifier is a valid GitLab release asset URL
func validateGitLabReleaseURL(identifier string) error {
	// GitLab has two valid formats:
	// 1. Release downloads: https://gitlab.com/owner/repo/-/releases/tag/downloads/filename
	// 2. Package files: https://gitlab.com/owner/repo/-/package_files/id/download

	if !strings.HasPrefix(identifier, "https://gitlab.com/") {
		return fmt.Errorf("GitLab MCPB packages must be release assets")
	}

	// Remove the base URL part
	path := strings.TrimPrefix(identifier, "https://gitlab.com/")
	
	// Split into parts
	parts := strings.Split(path, "/")
	
	// Must have at least 5 parts for any valid GitLab pattern
	if len(parts) < 5 {
		return fmt.Errorf("GitLab MCPB packages must be release assets")
	}

	// Find the "-/" separator (GitLab uses this pattern)
	dashIndex := -1
	for i, part := range parts {
		if part == "-" {
			dashIndex = i
			break
		}
	}

	if dashIndex == -1 {
		return fmt.Errorf("GitLab MCPB packages must be release assets")
	}

	// Check remaining parts after "-/"
	remainingParts := parts[dashIndex+1:]
	if len(remainingParts) < 2 {
		return fmt.Errorf("GitLab MCPB packages must be release assets")
	}

	// Check for valid patterns
	switch remainingParts[0] {
	case "releases":
		// Format: /-/releases/tag/downloads/filename
		if len(remainingParts) < 4 {
			return fmt.Errorf("GitLab MCPB packages must be release assets")
		}
		if remainingParts[2] != "downloads" {
			return fmt.Errorf("GitLab MCPB packages must be release assets")
		}
		// Check tag is not empty
		if remainingParts[1] == "" {
			return fmt.Errorf("GitLab MCPB packages must be release assets")
		}
		// Check filename is not empty
		if remainingParts[3] == "" {
			return fmt.Errorf("GitLab MCPB packages must be release assets")
		}
	case "package_files":
		// Format: /-/package_files/numeric_id/download
		if len(remainingParts) < 3 {
			return fmt.Errorf("GitLab MCPB packages must be release assets")
		}
		if remainingParts[2] != "download" {
			return fmt.Errorf("GitLab MCPB packages must be release assets")
		}
		// Check that package file ID is numeric
		packageID := remainingParts[1]
		if packageID == "" {
			return fmt.Errorf("GitLab MCPB packages must be release assets")
		}
		// Simple check for numeric ID (all digits)
		for _, char := range packageID {
			if char < '0' || char > '9' {
				return fmt.Errorf("GitLab MCPB packages must be release assets")
			}
		}
	default:
		return fmt.Errorf("GitLab MCPB packages must be release assets")
	}

	return nil
}
