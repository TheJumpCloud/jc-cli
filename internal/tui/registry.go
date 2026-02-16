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
	Key          string                // Schema key (e.g. "users")
	DisplayName  string                // Human-readable name (e.g. "Users")
	Category     Category              // UI grouping
	ClientType   ClientType            // Which API client to use
	ListEndpoint string                // API endpoint for listing
	GetEndpoint  string                // API endpoint template for single get (with %s for ID)
	Schema       schema.ResourceSchema // Full schema metadata
}

// resourceCategory maps schema resource names to their UI category.
var resourceCategory = map[string]Category{
	"users":       CategoryIdentity,
	"admins":      CategoryIdentity,
	"user-states": CategoryIdentity,

	"devices":         CategoryDevices,
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

	"groups":    CategoryIntegrations,
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
	"groups":           "Groups",
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
	"groups":           "/usergroups",
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
}

// clientTypeOverrides corrects resources whose schema.APIVersion doesn't match
// the actual client used by the CLI. For example, admins uses the V1 /users
// endpoint even though the schema declares it as V2.
var clientTypeOverrides = map[string]ClientType{
	"admins": ClientV1,
}

// skipInTUI lists resources that cannot be browsed generically.
// system-insights requires a table name; insights uses POST-based queries.
var skipInTUI = map[string]bool{
	"system-insights": true,
	"insights":        true,
}

// BuildRegistry creates ResourceEntry items for all schema resources.
func BuildRegistry() []ResourceEntry {
	entries := make([]ResourceEntry, 0, len(schema.Resources))
	for name, s := range schema.Resources {
		// Skip resources that can't be browsed generically.
		if skipInTUI[name] {
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

		entries = append(entries, ResourceEntry{
			Key:          name,
			DisplayName:  dn,
			Category:     cat,
			ClientType:   ct,
			ListEndpoint: ep,
			Schema:       s,
		})
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
