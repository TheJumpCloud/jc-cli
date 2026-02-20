package tui

import (
	"sort"

	"github.com/klaassen-consulting/jc/internal/schema"
)

// Category groups related resources for the home screen.
type Category string

const (
	CategoryUserMgmt   Category = "User Management"
	CategoryDeviceMgmt Category = "Device Management"
	CategoryAccess     Category = "Access"
	CategorySecurity   Category = "Security"
	CategoryInsights   Category = "Insights"
	CategorySettings   Category = "Settings"
)

// CategoryOrder defines the display order of categories.
// This order controls both: (1) single-column filter mode rendering, and
// (2) within-column stacking order in grid mode (categories in the same
// column appear top-to-bottom in this order).
var CategoryOrder = []Category{
	CategoryUserMgmt,
	CategoryDeviceMgmt,
	CategoryAccess,
	CategorySecurity,
	CategoryInsights,
	CategorySettings,
}

// categoryColumns maps each category to its grid column (0-indexed).
var categoryColumns = map[Category]int{
	CategoryUserMgmt:   0,
	CategorySecurity:   0,
	CategoryDeviceMgmt: 1,
	CategorySettings:   1,
	CategoryAccess:     2,
	CategoryInsights:   2,
}

// CategoryColumn returns the grid column (0-2) for a category.
func CategoryColumn(c Category) int {
	if col, ok := categoryColumns[c]; ok {
		return col
	}
	return 0
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
	Placeholder     bool                  // True for "Coming soon" items
	SubMenu         []ResourceEntry       // Non-nil for sub-menu groupings (e.g. Cloud Directories)
}

// graphSourceTypes maps TUI resource keys to V2 graph source type identifiers.
var graphSourceTypes = map[string]string{
	"users":          "user",
	"devices":        "device",
	"user-groups":    "user_group",
	"device-groups":  "device_group",
	"apps":           "application",
	"commands":       "command",
	"policies":       "policy",
	"policy-groups":  "policy_group",
	"software":       "software_app",
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
	"command":      "/commands",
	"policy":       "/policies",
	"policy_group": "/policygroups",
	"software_app": "/softwareapps",
}

// ValidAssocTargets maps each graph source type to its allowed target types.
// Membership targets (user for user_group, system for device_group) are listed
// first and use dedicated membership endpoints instead of graph associations.
// Graph targets are validated against the live JumpCloud V2 API (see graph.go).
// Only targets with TUI registry entries (graphTypeToRegistryKey) are included.
var ValidAssocTargets = map[string][]string{
	"user":         {"user_group", "application", "system", "system_group", "radius_server", "ldap_server"},
	"device":       {"command", "policy", "system_group", "user", "user_group"},
	"user_group":   {"user", "application", "system", "system_group", "radius_server", "ldap_server"},
	"device_group": {"system", "command", "policy", "user", "user_group"},
	"application":  {"user", "user_group"},
	"command":      {"system", "system_group"},
	"policy":       {"system", "system_group", "policy_group"},
	"policy_group": {"system", "system_group", "policy"},
	"software_app": {"system", "system_group"},
}

// assocTargetLabels maps V2 graph target type identifiers to human-readable labels.
var assocTargetLabels = map[string]string{
	"user":          "Users",
	"system":        "Devices",
	"user_group":    "User Groups",
	"system_group":  "Device Groups",
	"application":   "Applications",
	"command":       "Commands",
	"policy":        "Policies",
	"radius_server": "RADIUS Servers",
	"ldap_server":   "LDAP Servers",
	"policy_group":  "Policy Groups",
	"software_app":  "Software Apps",
}

// AssocTargetLabel returns the human-readable label for a V2 graph target type.
// Falls back to the raw type string if no label is defined.
func AssocTargetLabel(target string) string {
	if label, ok := assocTargetLabels[target]; ok {
		return label
	}
	return target
}

// MemberOfTarget returns the group target type for a non-group source type, or ""
// if the source is a group or has no memberof endpoint. Non-group resources use
// /memberof to discover which groups they belong to, because the V2 graph
// associations API does not support user→user_group or device→system_group.
func MemberOfTarget(sourceType string) string {
	switch sourceType {
	case "user":
		return "user_group"
	case "device":
		return "system_group"
	default:
		return ""
	}
}

// MembershipTarget returns the member type for a group source type, or "" if
// the source is not a group. Group members use dedicated endpoints (/members,
// /membership) rather than the graph associations API.
func MembershipTarget(sourceType string) string {
	switch sourceType {
	case "user_group":
		return "user"
	case "device_group":
		return "system"
	default:
		return ""
	}
}

// MembershipEndpoint returns the V2 API endpoint prefix for listing group members.
func MembershipEndpoint(sourceType string) string {
	switch sourceType {
	case "user_group":
		return "/usergroups"
	case "device_group":
		return "/systemgroups"
	default:
		return ""
	}
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
	"policy_group":  "policy-groups",
	"software_app":  "software",
}

// RegistryKeyForGraphType returns the TUI registry key for a V2 graph
// association type (e.g. "system" → "devices"). Returns "" if unknown.
func RegistryKeyForGraphType(graphType string) string {
	return graphTypeToRegistryKey[graphType]
}

// resourceCategory maps schema resource names to their UI category.
var resourceCategory = map[string]Category{
	// User Management
	"users":       CategoryUserMgmt,
	"user-groups": CategoryUserMgmt,
	"ad":          CategoryUserMgmt,

	// Device Management
	"devices":          CategoryDeviceMgmt,
	"device-groups":    CategoryDeviceMgmt,
	"commands":         CategoryDeviceMgmt,
	"policies":         CategoryDeviceMgmt,
	"policy-groups":    CategoryDeviceMgmt,
	"software":         CategoryDeviceMgmt,
	"apple-mdm":        CategoryDeviceMgmt,
	"system-insights":  CategoryDeviceMgmt,
	"policy-templates": CategoryDeviceMgmt,

	// Access
	"apps":          CategoryAccess,
	"app-templates": CategoryAccess,
	"ldap":          CategoryAccess,
	"radius":        CategoryAccess,

	// Security
	"auth-policies": CategorySecurity,
	"iplists":       CategorySecurity,

	// Insights
	"insights": CategoryInsights,

	// Settings
	"admins":        CategorySettings,
	"org":           CategorySettings,
	"custom-emails": CategorySettings,
	"user-states":   CategorySettings,
	"bulk":          CategorySettings,
	"duo": CategorySettings,
	// Note: gsuite and office365 are excluded — they are folded into the
	// "cloud-directories" sub-menu entry by BuildRegistry().
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
	"office365":        "M365",
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

// placeholderEntries defines "Coming soon" items shown grayed out in the menu.
var placeholderEntries = []ResourceEntry{
	{Key: "hr-directories", DisplayName: "HR Directories", Category: CategoryUserMgmt, Placeholder: true},
	{Key: "identity-providers", DisplayName: "Identity Providers", Category: CategoryUserMgmt, Placeholder: true},
	{Key: "asset-management", DisplayName: "Asset Management", Category: CategoryDeviceMgmt, Placeholder: true},
	{Key: "patch-management", DisplayName: "Patch Management", Category: CategoryDeviceMgmt, Placeholder: true},
	{Key: "access-requests", DisplayName: "Access Requests", Category: CategoryAccess, Placeholder: true},
	{Key: "ai-saas-management", DisplayName: "AI & SaaS Management", Category: CategoryAccess, Placeholder: true},
	{Key: "vault", DisplayName: "Vault", Category: CategoryAccess, Placeholder: true},
	{Key: "mfa-configurations", DisplayName: "MFA Configurations", Category: CategorySecurity, Placeholder: true},
	{Key: "device-trust", DisplayName: "Device Trust", Category: CategorySecurity, Placeholder: true},
	{Key: "password-policies", DisplayName: "Password Policies", Category: CategorySecurity, Placeholder: true},
}

// cloudDirResources lists schema resource names that are folded into the
// "Cloud Directories" sub-menu instead of appearing as top-level entries.
var cloudDirResources = map[string]bool{
	"gsuite":    true,
	"office365": true,
}

// BuildRegistry creates ResourceEntry items for all schema resources.
func BuildRegistry() []ResourceEntry {
	entries := make([]ResourceEntry, 0, len(schema.Resources))
	var cloudDirChildren []ResourceEntry

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
			cat = CategorySettings
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

		// Cloud directory resources are folded into a sub-menu.
		if cloudDirResources[name] {
			cloudDirChildren = append(cloudDirChildren, entry)
			continue
		}

		entries = append(entries, entry)
	}

	// Cloud Directories sub-menu groups gsuite and office365.
	if len(cloudDirChildren) > 0 {
		// Sort children deterministically: Google Workspace before M365.
		sort.Slice(cloudDirChildren, func(i, j int) bool {
			return cloudDirChildren[i].Key < cloudDirChildren[j].Key
		})
		entries = append(entries, ResourceEntry{
			Key:         "cloud-directories",
			DisplayName: "Cloud Directories",
			Category:    CategoryUserMgmt,
			SubMenu:     cloudDirChildren,
		})
	}

	// Add placeholder entries.
	entries = append(entries, placeholderEntries...)

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
