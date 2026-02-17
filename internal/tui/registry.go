package tui

import (
	"sort"

	"github.com/klaassen-consulting/jc/internal/schema"
)

// Category groups related resources for the home screen.
type Category string

const (
	CategoryIdentity     Category = "Identity"
	CategoryDevices      Category = "Devices"
	CategoryManagement   Category = "Management"
	CategoryApplications Category = "Applications"
	CategorySecurity     Category = "Security"
	CategoryIntegrations Category = "Integrations"
	CategoryAudit        Category = "Audit"
)

// CategoryOrder defines the display order of categories.
var CategoryOrder = []Category{
	CategoryIdentity,
	CategoryDevices,
	CategorySecurity,
	CategoryManagement,
	CategoryApplications,
	CategoryIntegrations,
	CategoryAudit,
}

// ClientType indicates which API client a resource uses.
type ClientType int

const (
	ClientV1 ClientType = iota
	ClientV2
	ClientInsights
)

// ResourceEntry is a TUI-enriched view of a schema resource.
type ResourceEntry struct {
	Key             string                // Schema key (e.g. "users")
	DisplayName     string                // Human-readable name (e.g. "Users")
	Category        Category              // UI grouping
	ClientType      ClientType            // Which API client to use
	ListEndpoint    string                // API endpoint for listing
	GetEndpoint     string                // API endpoint template for single get (with %s for ID)
	GraphSourceType string                // V2 graph source type (e.g. "user"), empty if no associations
	PivotField      string                // Row field whose value becomes the ID for pivot navigation
	PivotTargetKey  string                // Registry key of the target resource to pivot to
	SearchEndpoint  string                // POST search endpoint (e.g. "/search/systemusers"), empty if not supported
	SearchFields    []string              // Fields to search across (e.g. ["username","email","firstname","lastname"])
	Schema          schema.ResourceSchema // Full schema metadata
}

// graphSourceTypes maps TUI resource keys to V2 graph source type identifiers.
var graphSourceTypes = map[string]string{
	"users":         "user",
	"devices":       "device",
	"user-groups":   "user_group",
	"device-groups": "device_group",
	"apps":          "application",
}

// searchEndpoints maps resource keys to their POST search endpoint.
// Only V1 resources with dedicated /search/ endpoints are listed here.
var searchEndpoints = map[string]string{
	"users":   "/search/systemusers",
	"devices": "/search/systems",
}

// searchFields maps resource keys to the fields searched by POST search.
var searchFields = map[string][]string{
	"users":   {"username", "email", "firstname", "lastname"},
	"devices": {"displayName", "hostname", "serialNumber"},
}

// graphEndpoints maps graph source types to their V2 API endpoint prefix.
var graphEndpoints = map[string]string{
	"user":         "/users",
	"device":       "/systems",
	"user_group":   "/usergroups",
	"device_group": "/systemgroups",
	"application":  "/applications",
}

// ValidAssocTargets maps each graph source type to its allowed target types.
var ValidAssocTargets = map[string][]string{
	"user":         {"application", "system", "system_group", "radius_server", "ldap_server"},
	"device":       {"command", "policy", "user", "user_group"},
	"user_group":   {"user", "application", "system", "system_group"},
	"device_group": {"system", "command", "policy", "user", "user_group"},
	"application":  {"user", "user_group"},
}

// GraphEndpoint returns the V2 graph API endpoint prefix for a source type.
func GraphEndpoint(sourceType string) string {
	return graphEndpoints[sourceType]
}

// graphTypeToRegistryKey maps V2 graph association type identifiers back to
// TUI registry keys, enabling drill-down from association rows.
var graphTypeToRegistryKey = map[string]string{
	"user":          "users",
	"system":        "devices",
	"user_group":    "user-groups",
	"system_group":  "device-groups",
	"application":   "apps",
	"command":       "commands",
	"policy":        "policies",
	"radius_server": "radius",
	"ldap_server":   "ldap",
}

// RegistryKeyForGraphType returns the TUI registry key for a V2 graph
// association type (e.g. "system" → "devices"). Returns "" if unknown.
func RegistryKeyForGraphType(graphType string) string {
	return graphTypeToRegistryKey[graphType]
}

// resourceCategory maps schema resource names to their UI category.
var resourceCategory = map[string]Category{
	"users":       CategoryIdentity,
	"admins":      CategoryIdentity,
	"user-states": CategoryIdentity,

	"devices":         CategoryDevices,
	"device-groups":   CategoryDevices,
	"system-insights": CategoryDevices,

	"commands":       CategoryManagement,
	"policies":       CategoryManagement,
	"policy-groups":  CategoryManagement,
	"org":            CategoryManagement,
	"custom-emails":  CategoryManagement,
	"bulk":           CategoryManagement,

	"apps":              CategoryApplications,
	"app-templates":     CategoryApplications,
	"policy-templates":  CategoryApplications,

	"auth-policies": CategorySecurity,
	"iplists":       CategorySecurity,
	"radius":        CategorySecurity,

	"user-groups": CategoryIdentity,
	"gsuite":    CategoryIntegrations,
	"office365": CategoryIntegrations,
	"ldap":      CategoryIntegrations,
	"ad":        CategoryIntegrations,
	"apple-mdm": CategoryIntegrations,
	"duo":       CategoryIntegrations,
	"software":  CategoryIntegrations,

	"insights": CategoryAudit,
}

// displayNames maps schema resource names to human-readable display names.
var displayNames = map[string]string{
	"users":            "Users",
	"devices":          "Devices",
	"user-groups":      "User Groups",
	"device-groups":    "Device Groups",
	"commands":         "Commands",
	"policies":         "Policies",
	"apps":             "Applications",
	"admins":           "Administrators",
	"auth-policies":    "Auth Policies",
	"iplists":          "IP Lists",
	"insights":         "Directory Insights",
	"software":         "Software Apps",
	"ldap":             "LDAP Servers",
	"ad":               "Active Directory",
	"org":              "Organization",
	"system-insights":  "System Insights",
	"radius":           "RADIUS Servers",
	"policy-templates": "Policy Templates",
	"apple-mdm":        "Apple MDM",
	"policy-groups":    "Policy Groups",
	"user-states":      "User States",
	"gsuite":           "Google Workspace",
	"office365":        "Office 365",
	"duo":              "Duo Security",
	"custom-emails":    "Custom Emails",
	"app-templates":    "App Templates",
}

// listEndpoints maps schema resource names to their list API endpoint.
var listEndpoints = map[string]string{
	"users":            "/systemusers",
	"devices":          "/systems",
	"commands":         "/commands",
	"apps":             "/applications",
	"admins":           "/users",
	"org":              "/organizations",
	"radius":           "/radiusservers",
	"app-templates":    "/application-templates",
	"user-groups":      "/usergroups",
	"device-groups":    "/systemgroups",
	"policies":         "/policies",
	"auth-policies":    "/authn/policies",
	"iplists":          "/iplists",
	"software":         "/softwareapps",
	"ldap":             "/ldapservers",
	"ad":               "/activedirectories",
	"policy-templates": "/policytemplates",
	"apple-mdm":        "/applemdms",
	"policy-groups":    "/policygroups",
	"user-states":      "/bulk/userstates",
	"gsuite":           "/gsuites",
	"office365":        "/office365s",
	"duo":              "/duo/accounts",
	"custom-emails":    "/customemail/templates",
	"system-insights": "/systeminsights",
	"insights":        "/events",
}

// clientTypeOverrides corrects resources whose schema.APIVersion doesn't match
// the actual client used by the CLI. For example, admins uses the V1 /users
// endpoint even though the schema declares it as V2.
var clientTypeOverrides = map[string]ClientType{
	"admins": ClientV1,
}

// skipInTUI lists resources that cannot be browsed generically.
// These resources need custom screens instead of the generic list view.
var skipInTUI = map[string]bool{}

// SystemInsightsTables lists all supported System Insights osquery table names.
// Duplicated from cmd/system_insights.go to avoid importing cmd (circular dependency).
var SystemInsightsTables = []string{
	"alf", "alf_exceptions", "alf_explicit_auths", "apps", "authorized_keys",
	"azure_instance_metadata", "azure_instance_tags", "battery", "bitlocker_info",
	"browser_plugins", "certificates", "chassis_info", "chrome_extensions",
	"connectivity", "crashes", "cups_destinations", "disk_encryption", "disk_info",
	"dns_resolvers", "etc_hosts", "firefox_addons", "groups",
	"ie_extensions", "interface_addresses", "interface_details", "kernel_info",
	"launchd", "linux_packages", "logged_in_users", "logical_drives",
	"managed_policies", "mounts", "os_version", "patches", "programs",
	"python_packages", "safari_extensions", "scheduled_tasks", "secureboot",
	"services", "shadow", "shared_folders", "shared_resources",
	"sharing_preferences", "sip_config", "startup_items", "system_controls",
	"system_info", "tpm_info", "uptime", "usb_devices", "user_assist",
	"user_groups", "user_ssh_keys", "users", "wifi_networks", "wifi_status",
	"windows_security_center", "windows_security_products",
}

// BuildRegistry creates ResourceEntry items for all schema resources.
func BuildRegistry() []ResourceEntry {
	entries := make([]ResourceEntry, 0, len(schema.Resources))
	for name, s := range schema.Resources {
		// Skip resources that can't be browsed generically.
		if skipInTUI[name] {
			continue
		}

		// Split "groups" into two entries: user-groups and device-groups.
		if name == "groups" {
			for _, synKey := range []string{"user-groups", "device-groups"} {
				entries = append(entries, ResourceEntry{
					Key:             synKey,
					DisplayName:     displayNames[synKey],
					Category:        resourceCategory[synKey],
					ClientType:      ClientV2,
					ListEndpoint:    listEndpoints[synKey],
					GraphSourceType: graphSourceTypes[synKey],
					Schema:          s,
				})
			}
			continue
		}

		ct := ClientV2
		switch s.APIVersion {
		case "v1":
			ct = ClientV1
		case "insights/v1":
			ct = ClientInsights
		}

		// Apply client type overrides where schema doesn't match reality.
		if override, ok := clientTypeOverrides[name]; ok {
			ct = override
		}

		cat := resourceCategory[name]
		if cat == "" {
			cat = CategoryManagement
		}

		dn := displayNames[name]
		if dn == "" {
			dn = name
		}

		ep := listEndpoints[name]

		entry := ResourceEntry{
			Key:             name,
			DisplayName:     dn,
			Category:        cat,
			ClientType:      ct,
			ListEndpoint:    ep,
			GraphSourceType: graphSourceTypes[name],
			SearchEndpoint:  searchEndpoints[name],
			SearchFields:    searchFields[name],
			Schema:          s,
		}

		// System Insights rows have no ID of their own but contain a system_id
		// that references a device. Pivot Enter to the device detail screen.
		if name == "system-insights" {
			entry.PivotField = "system_id"
			entry.PivotTargetKey = "devices"
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return categoryIndex(entries[i].Category) < categoryIndex(entries[j].Category)
		}
		return entries[i].DisplayName < entries[j].DisplayName
	})

	return entries
}

func categoryIndex(c Category) int {
	for i, cat := range CategoryOrder {
		if cat == c {
			return i
		}
	}
	return len(CategoryOrder)
}

// RegistryByKey returns a map from schema key to ResourceEntry.
func RegistryByKey() map[string]ResourceEntry {
	entries := BuildRegistry()
	m := make(map[string]ResourceEntry, len(entries))
	for _, e := range entries {
		m[e.Key] = e
	}
	return m
}
