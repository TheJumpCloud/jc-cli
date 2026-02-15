package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/spf13/viper"
)

// idPattern matches JumpCloud 24-character hex IDs (MongoDB ObjectIDs).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsID returns true if the input looks like a JumpCloud ID (24-char hex).
func IsID(s string) bool {
	return idPattern.MatchString(s)
}

// cacheEntry holds a cached name→ID mapping with a timestamp.
type cacheEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
}

// cacheFile represents the on-disk cache for a single resource type.
type cacheFile map[string]cacheEntry

// Resolver resolves human-friendly names (usernames, hostnames) to JumpCloud IDs.
// It checks if the input is already an ID, then consults a file-based cache,
// and finally falls back to API search.
type Resolver struct {
	Client *api.V1Client

	// nowFn returns the current time. Overridable for tests.
	nowFn func() time.Time
}

// NewResolver creates a resolver with the given V1 client.
func NewResolver(client *api.V1Client) *Resolver {
	return &Resolver{
		Client: client,
		nowFn:  time.Now,
	}
}

// ResourceConfig defines how to resolve names for a specific resource type.
type ResourceConfig struct {
	// CacheKey is the filename stem for the cache file (e.g., "users", "systems").
	CacheKey string
	// ListEndpoint is the V1 API endpoint for listing (e.g., "/systemusers").
	ListEndpoint string
	// NameField is the JSON field name used for matching (e.g., "username", "hostname").
	NameField string
	// IDField is the JSON field that contains the resource ID (typically "_id").
	IDField string
}

// UserConfig is the resolution config for JumpCloud users.
var UserConfig = ResourceConfig{
	CacheKey:     "users",
	ListEndpoint: "/systemusers",
	NameField:    "username",
	IDField:      "_id",
}

// DeviceConfig is the resolution config for JumpCloud devices (systems).
var DeviceConfig = ResourceConfig{
	CacheKey:     "systems",
	ListEndpoint: "/systems",
	NameField:    "hostname",
	IDField:      "_id",
}

// UserGroupConfig is the resolution config for JumpCloud user groups (V2 API).
var UserGroupConfig = ResourceConfig{
	CacheKey:     "usergroups",
	ListEndpoint: "/usergroups",
	NameField:    "name",
	IDField:      "id",
}

// DeviceGroupConfig is the resolution config for JumpCloud device/system groups (V2 API).
var DeviceGroupConfig = ResourceConfig{
	CacheKey:     "systemgroups",
	ListEndpoint: "/systemgroups",
	NameField:    "name",
	IDField:      "id",
}

// CommandConfig is the resolution config for JumpCloud commands (V1 API).
var CommandConfig = ResourceConfig{
	CacheKey:     "commands",
	ListEndpoint: "/commands",
	NameField:    "name",
	IDField:      "_id",
}

// ApplicationConfig is the resolution config for JumpCloud SSO applications (V1 API).
var ApplicationConfig = ResourceConfig{
	CacheKey:     "applications",
	ListEndpoint: "/applications",
	NameField:    "name",
	IDField:      "_id",
}

// IPListConfig is the resolution config for JumpCloud IP lists (V2 API).
var IPListConfig = ResourceConfig{
	CacheKey:     "iplists",
	ListEndpoint: "/iplists",
	NameField:    "name",
	IDField:      "id",
}

// SoftwareAppConfig is the resolution config for JumpCloud software apps (V2 API).
var SoftwareAppConfig = ResourceConfig{
	CacheKey:     "softwareapps",
	ListEndpoint: "/softwareapps",
	NameField:    "displayName",
	IDField:      "id",
}

// LDAPServerConfig is the resolution config for JumpCloud LDAP servers (V2 API).
var LDAPServerConfig = ResourceConfig{
	CacheKey:     "ldapservers",
	ListEndpoint: "/ldapservers",
	NameField:    "name",
	IDField:      "id",
}

// AuthPolicyConfig is the resolution config for JumpCloud authentication policies (V2 API).
var AuthPolicyConfig = ResourceConfig{
	CacheKey:     "auth-policies",
	ListEndpoint: "/authn/policies",
	NameField:    "name",
	IDField:      "id",
}

// PolicyConfig is the resolution config for JumpCloud policies (V2 API).
var PolicyConfig = ResourceConfig{
	CacheKey:     "policies",
	ListEndpoint: "/policies",
	NameField:    "name",
	IDField:      "id",
}

// ActiveDirectoryConfig is the resolution config for JumpCloud Active Directory integrations (V2 API).
var ActiveDirectoryConfig = ResourceConfig{
	CacheKey:     "activedirectories",
	ListEndpoint: "/activedirectories",
	NameField:    "domain",
	IDField:      "id",
}

// RADIUSServerConfig is the resolution config for JumpCloud RADIUS servers (V1 API).
var RADIUSServerConfig = ResourceConfig{
	CacheKey:     "radius",
	ListEndpoint: "/radiusservers",
	NameField:    "name",
	IDField:      "_id",
}

// AppleMDMConfig is the resolution config for JumpCloud Apple MDM configurations (V2 API).
var AppleMDMConfig = ResourceConfig{
	CacheKey:     "apple-mdm",
	ListEndpoint: "/applemdms",
	NameField:    "name",
	IDField:      "id",
}

// PolicyGroupConfig is the resolution config for JumpCloud policy groups (V2 API).
var PolicyGroupConfig = ResourceConfig{
	CacheKey:     "policy-groups",
	ListEndpoint: "/policygroups",
	NameField:    "name",
	IDField:      "id",
}

// AdminConfig is the resolution config for JumpCloud administrators (V1 API).
var AdminConfig = ResourceConfig{
	CacheKey:     "admins",
	ListEndpoint: "/users",
	NameField:    "email",
	IDField:      "_id",
}

// GsuiteConfig is the resolution config for JumpCloud Google Workspace integrations (V2 API).
var GsuiteConfig = ResourceConfig{
	CacheKey:     "gsuites",
	ListEndpoint: "/gsuites",
	NameField:    "name",
	IDField:      "id",
}

// Office365Config is the resolution config for JumpCloud Office 365 integrations (V2 API).
var Office365Config = ResourceConfig{
	CacheKey:     "office365",
	ListEndpoint: "/office365s",
	NameField:    "name",
	IDField:      "id",
}

// DuoAccountConfig is the resolution config for JumpCloud Duo accounts (V2 API).
var DuoAccountConfig = ResourceConfig{
	CacheKey:     "duo",
	ListEndpoint: "/duo/accounts",
	NameField:    "name",
	IDField:      "id",
}

// Resolve takes a human-friendly identifier (name or ID) and returns the JumpCloud ID.
//
// Resolution order:
//  1. If input matches 24-char hex pattern → return as-is (it's already an ID)
//  2. If cache enabled and not bypassed → check cache for name→ID mapping
//  3. Fall back to API list with name matching
//  4. Cache the result if caching is enabled
//
// Returns an error if:
//   - No match found
//   - Multiple matches found (ambiguous)
//   - API error occurs
func (r *Resolver) Resolve(ctx context.Context, identifier string, cfg ResourceConfig) (string, error) {
	// Step 1: If it looks like an ID, use it directly.
	if IsID(identifier) {
		return identifier, nil
	}

	noCache := viper.GetBool("no-cache")
	cacheEnabled := viper.GetBool("cache.enabled")

	// Step 2: Check the cache (if enabled and not bypassed).
	if cacheEnabled && !noCache {
		if id, ok := r.lookupCache(identifier, cfg.CacheKey); ok {
			return id, nil
		}
	}

	// Step 3: Resolve via API.
	id, err := r.resolveViaAPI(ctx, identifier, cfg)
	if err != nil {
		return "", err
	}

	// Step 4: Store in cache.
	if cacheEnabled && !noCache {
		r.storeCache(identifier, id, cfg.CacheKey)
	}

	return id, nil
}

// lookupCache checks the on-disk cache for a name→ID mapping.
// Returns the ID and true if found and not expired, empty string and false otherwise.
func (r *Resolver) lookupCache(name, cacheKey string) (string, bool) {
	cf := r.readCacheFile(cacheKey)
	if cf == nil {
		return "", false
	}

	entry, ok := cf[strings.ToLower(name)]
	if !ok {
		return "", false
	}

	// Check TTL.
	ttl := time.Duration(viper.GetInt("cache.ttl")) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	if r.nowFn().Sub(entry.Timestamp) > ttl {
		return "", false // expired
	}

	return entry.ID, true
}

// storeCache persists a name→ID mapping to the on-disk cache.
func (r *Resolver) storeCache(name, id, cacheKey string) {
	cf := r.readCacheFile(cacheKey)
	if cf == nil {
		cf = make(cacheFile)
	}

	cf[strings.ToLower(name)] = cacheEntry{
		ID:        id,
		Timestamp: r.nowFn(),
	}

	r.writeCacheFile(cacheKey, cf)
}

// resolveViaAPI searches the API for a resource matching the given name.
func (r *Resolver) resolveViaAPI(ctx context.Context, name string, cfg ResourceConfig) (string, error) {
	// Fetch all resources and filter client-side by name field.
	// We use a generous limit — name resolution typically matches 0 or 1 items.
	result, err := r.Client.ListAll(ctx, cfg.ListEndpoint, api.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("resolving %s %q: %w", cfg.NameField, name, err)
	}

	var matches []match
	lowerName := strings.ToLower(name)

	for _, raw := range result.Data {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}

		// Extract the name field.
		nameRaw, ok := obj[cfg.NameField]
		if !ok {
			continue
		}
		var nameVal string
		if err := json.Unmarshal(nameRaw, &nameVal); err != nil {
			continue
		}

		if strings.ToLower(nameVal) != lowerName {
			continue
		}

		// Extract the ID field.
		idRaw, ok := obj[cfg.IDField]
		if !ok {
			continue
		}
		var idVal string
		if err := json.Unmarshal(idRaw, &idVal); err != nil {
			continue
		}

		matches = append(matches, match{ID: idVal, Name: nameVal})
	}

	switch len(matches) {
	case 0:
		return "", &ResolveError{
			ResourceType: cfg.NameField,
			Identifier:   name,
			Message:      fmt.Sprintf("%s %q not found", cfg.NameField, name),
		}
	case 1:
		return matches[0].ID, nil
	default:
		lines := make([]string, len(matches))
		for i, m := range matches {
			lines[i] = fmt.Sprintf("  %s (ID: %s)", m.Name, m.ID)
		}
		return "", &ResolveError{
			ResourceType: cfg.NameField,
			Identifier:   name,
			Message: fmt.Sprintf("ambiguous %s %q matched %d resources:\n%s",
				cfg.NameField, name, len(matches), strings.Join(lines, "\n")),
		}
	}
}

// ResolveError is a structured error for resource resolution failures.
// It carries the resource type and identifier for machine-readable error reporting.
type ResolveError struct {
	ResourceType string // e.g., "username", "hostname", "name"
	Identifier   string // the name or ID that failed to resolve
	Message      string // human-readable message
}

func (e *ResolveError) Error() string {
	return e.Message
}

type match struct {
	ID   string
	Name string
}

// V2Resolver resolves human-friendly names to JumpCloud IDs using the V2 API.
type V2Resolver struct {
	Client *api.V2Client
	nowFn  func() time.Time
}

// NewV2Resolver creates a resolver with the given V2 client.
func NewV2Resolver(client *api.V2Client) *V2Resolver {
	return &V2Resolver{
		Client: client,
		nowFn:  time.Now,
	}
}

// Resolve takes a human-friendly identifier (name or ID) and returns the JumpCloud ID
// using the V2 API for lookup.
func (r *V2Resolver) Resolve(ctx context.Context, identifier string, cfg ResourceConfig) (string, error) {
	if IsID(identifier) {
		return identifier, nil
	}

	noCache := viper.GetBool("no-cache")
	cacheEnabled := viper.GetBool("cache.enabled")

	if cacheEnabled && !noCache {
		if id, ok := r.lookupCache(identifier, cfg.CacheKey); ok {
			return id, nil
		}
	}

	id, err := r.resolveViaV2API(ctx, identifier, cfg)
	if err != nil {
		return "", err
	}

	if cacheEnabled && !noCache {
		r.storeCache(identifier, id, cfg.CacheKey)
	}

	return id, nil
}

// resolveViaV2API searches the V2 API for a resource matching the given name.
func (r *V2Resolver) resolveViaV2API(ctx context.Context, name string, cfg ResourceConfig) (string, error) {
	result, err := r.Client.ListAll(ctx, cfg.ListEndpoint, api.V2ListOptions{})
	if err != nil {
		return "", fmt.Errorf("resolving %s %q: %w", cfg.NameField, name, err)
	}

	var matches []match
	lowerName := strings.ToLower(name)

	for _, raw := range result.Data {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}

		nameRaw, ok := obj[cfg.NameField]
		if !ok {
			continue
		}
		var nameVal string
		if err := json.Unmarshal(nameRaw, &nameVal); err != nil {
			continue
		}

		if strings.ToLower(nameVal) != lowerName {
			continue
		}

		idRaw, ok := obj[cfg.IDField]
		if !ok {
			continue
		}
		var idVal string
		if err := json.Unmarshal(idRaw, &idVal); err != nil {
			continue
		}

		matches = append(matches, match{ID: idVal, Name: nameVal})
	}

	switch len(matches) {
	case 0:
		return "", &ResolveError{
			ResourceType: cfg.NameField,
			Identifier:   name,
			Message:      fmt.Sprintf("%s %q not found", cfg.NameField, name),
		}
	case 1:
		return matches[0].ID, nil
	default:
		lines := make([]string, len(matches))
		for i, m := range matches {
			lines[i] = fmt.Sprintf("  %s (ID: %s)", m.Name, m.ID)
		}
		return "", &ResolveError{
			ResourceType: cfg.NameField,
			Identifier:   name,
			Message: fmt.Sprintf("ambiguous %s %q matched %d resources:\n%s",
				cfg.NameField, name, len(matches), strings.Join(lines, "\n")),
		}
	}
}

// lookupCache checks the on-disk cache (reuses the same cache infrastructure as V1).
func (r *V2Resolver) lookupCache(name, cacheKey string) (string, bool) {
	cf := r.readCacheFile(cacheKey)
	if cf == nil {
		return "", false
	}

	entry, ok := cf[strings.ToLower(name)]
	if !ok {
		return "", false
	}

	ttl := time.Duration(viper.GetInt("cache.ttl")) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	if r.nowFn().Sub(entry.Timestamp) > ttl {
		return "", false
	}

	return entry.ID, true
}

// storeCache persists a name→ID mapping to the on-disk cache.
func (r *V2Resolver) storeCache(name, id, cacheKey string) {
	cf := r.readCacheFile(cacheKey)
	if cf == nil {
		cf = make(cacheFile)
	}

	cf[strings.ToLower(name)] = cacheEntry{
		ID:        id,
		Timestamp: r.nowFn(),
	}

	r.writeCacheFile(cacheKey, cf)
}

// readCacheFile reads and parses a cache file for the given resource type.
func (r *V2Resolver) readCacheFile(cacheKey string) cacheFile {
	path := filepath.Join(cacheDir(), cacheKey+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil
	}
	return cf
}

// writeCacheFile writes the cache file for the given resource type.
func (r *V2Resolver) writeCacheFile(cacheKey string, cf cacheFile) {
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}

	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return
	}

	path := filepath.Join(dir, cacheKey+".json")
	_ = os.WriteFile(path, data, 0600)
}

// cacheDir returns the cache directory path.
// Priority: config cache.directory → XDG_CACHE_HOME/jc → ~/.cache/jc.
func cacheDir() string {
	if dir := viper.GetString("cache.directory"); dir != "" {
		return dir
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "jc")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "jc")
}

// readCacheFile reads and parses a cache file for the given resource type.
func (r *Resolver) readCacheFile(cacheKey string) cacheFile {
	path := filepath.Join(cacheDir(), cacheKey+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil
	}
	return cf
}

// writeCacheFile writes the cache file for the given resource type.
func (r *Resolver) writeCacheFile(cacheKey string, cf cacheFile) {
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return // silently fail on cache write errors
	}

	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return
	}

	path := filepath.Join(dir, cacheKey+".json")
	_ = os.WriteFile(path, data, 0600)
}
