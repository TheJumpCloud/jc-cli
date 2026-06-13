// Package schema provides machine-readable metadata for JumpCloud resource
// types and CLI commands. It is the single source of truth consumed by both
// the "jc schema" CLI commands and the MCP resource handlers.
package schema

import (
	"sort"

	"github.com/klaassen-consulting/jc/internal/version"
)

// FieldDef describes a single field on a JumpCloud resource.
type FieldDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // string, bool, int, datetime, array, object
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
}

// ResourceSchema describes a JumpCloud resource type.
type ResourceSchema struct {
	Resource      string     `json:"resource"`
	APIVersion    string     `json:"api_version"`
	Verbs         []string   `json:"verbs"`
	DefaultFields []string   `json:"default_fields"`
	Fields        []FieldDef `json:"fields"`
	FilterSupport bool       `json:"filter_support"`
	SortSupport   bool       `json:"sort_support"`
	SortFields    []string   `json:"sort_fields,omitempty"`
	IDField       string     `json:"id_field"`
	NameField     string     `json:"name_field"`
}

// CommandManifest describes the full CLI command tree.
type CommandManifest struct {
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Commands    []CommandEntry `json:"commands"`
	GlobalFlags []FlagEntry    `json:"global_flags"`
	Resources   []string       `json:"resources"`
}

// CommandEntry describes a CLI command group with its subcommands and flags.
//
// Long is an optional multi-sentence elaboration shown on the showcase
// site and in llms-full.txt. It's deliberately hand-curated rather than
// pulled from Cobra's Long string — the audience here is "someone
// browsing the public catalog," which wants more context than `jc <cmd>
// --help` typically gives. Leave empty if Description is sufficient.
type CommandEntry struct {
	Path        string      `json:"path"`
	Description string      `json:"description"`
	Long        string      `json:"long,omitempty"`
	Subcommands []string    `json:"subcommands,omitempty"`
	Flags       []FlagEntry `json:"flags,omitempty"`
}

// FlagEntry describes a CLI flag.
type FlagEntry struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
}

// Resources is the canonical map of all JumpCloud resource schemas.
var Resources = map[string]ResourceSchema{
	"users": {
		Resource:      "users",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete", "search", "lock", "unlock", "reset-mfa", "reset-password", "ssh-keys", "ssh-key-add", "ssh-key-delete"},
		DefaultFields: []string{"username", "email", "firstname", "lastname", "activated", "suspended"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique user identifier"},
			{Name: "username", Type: "string", Description: "System username (unique)", Required: true},
			{Name: "email", Type: "string", Description: "Email address", Required: true},
			{Name: "firstname", Type: "string", Description: "First name"},
			{Name: "lastname", Type: "string", Description: "Last name"},
			{Name: "displayname", Type: "string", Description: "Display name"},
			{Name: "department", Type: "string", Description: "Department"},
			{Name: "jobTitle", Type: "string", Description: "Job title"},
			{Name: "activated", Type: "bool", Description: "Whether the account is activated"},
			{Name: "suspended", Type: "bool", Description: "Whether the account is suspended"},
			{Name: "account_locked", Type: "bool", Description: "Whether the account is locked"},
			{Name: "totp_enabled", Type: "bool", Description: "Whether TOTP MFA is enabled"},
			{Name: "enable_user_portal_multifactor", Type: "bool", Description: "User portal MFA enabled"},
			{Name: "password_date", Type: "datetime", Description: "Last password change timestamp", ReadOnly: true},
			{Name: "created", Type: "datetime", Description: "Account creation timestamp", ReadOnly: true},
			{Name: "lastLogin", Type: "datetime", Description: "Last login timestamp", ReadOnly: true},
			{Name: "state", Type: "string", Description: "Account state (e.g. ACTIVATED, STAGED)", ReadOnly: true},
			{Name: "description", Type: "string", Description: "User description"},
			{Name: "company", Type: "string", Description: "Company name"},
			{Name: "location", Type: "string", Description: "Location"},
			{Name: "costCenter", Type: "string", Description: "Cost center"},
			{Name: "employeeType", Type: "string", Description: "Employee type"},
			{Name: "employeeIdentifier", Type: "string", Description: "Employee identifier"},
			{Name: "mfa", Type: "object", Description: "MFA configuration details"},
			{Name: "addresses", Type: "array", Description: "Physical addresses"},
			{Name: "phoneNumbers", Type: "array", Description: "Phone numbers"},
			{Name: "attributes", Type: "array", Description: "Custom attributes"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"username", "email", "firstname", "lastname", "created", "activated", "suspended"},
		IDField:       "_id",
		NameField:     "username",
	},
	"devices": {
		Resource:      "devices",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "update", "delete", "search", "lock", "restart", "erase", "fde-key"},
		DefaultFields: []string{"displayName", "hostname", "os", "osVersion", "lastContact", "agentVersion"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique device identifier"},
			{Name: "displayName", Type: "string", Description: "Display name"},
			{Name: "hostname", Type: "string", Description: "Device hostname"},
			{Name: "os", Type: "string", Description: "Operating system (e.g. Mac OS X, Windows, Linux)"},
			{Name: "osVersion", Type: "string", Description: "OS version string"},
			{Name: "lastContact", Type: "datetime", Description: "Last agent check-in timestamp"},
			{Name: "agentVersion", Type: "string", Description: "JumpCloud agent version"},
			{Name: "active", Type: "bool", Description: "Whether the device is active"},
			{Name: "allowMultiFactorAuthentication", Type: "bool", Description: "MFA allowed on device"},
			{Name: "allowPublicKeyAuthentication", Type: "bool", Description: "Public key auth allowed"},
			{Name: "allowSshPasswordAuthentication", Type: "bool", Description: "SSH password auth allowed"},
			{Name: "arch", Type: "string", Description: "CPU architecture"},
			{Name: "created", Type: "datetime", Description: "Registration timestamp"},
			{Name: "serialNumber", Type: "string", Description: "Device serial number"},
			{Name: "systemTimezone", Type: "int", Description: "System timezone offset"},
			{Name: "remoteIP", Type: "string", Description: "Remote IP address"},
			{Name: "networkInterfaces", Type: "array", Description: "Network interface details"},
			{Name: "tags", Type: "array", Description: "Assigned tags"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"displayName", "hostname", "os", "osVersion", "lastContact", "created", "active"},
		IDField:       "_id",
		NameField:     "hostname",
	},
	"groups": {
		Resource:      "groups",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "add-member", "remove-member"},
		DefaultFields: []string{"id", "name", "description", "type"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique group identifier"},
			{Name: "name", Type: "string", Description: "Group name", Required: true},
			{Name: "description", Type: "string", Description: "Group description"},
			{Name: "type", Type: "string", Description: "Group type (user_group or system_group)"},
			{Name: "email", Type: "string", Description: "Group email address"},
			{Name: "attributes", Type: "object", Description: "Group attributes"},
			{Name: "memberQuery", Type: "object", Description: "Dynamic membership query"},
			{Name: "memberQueryExemptions", Type: "array", Description: "Members exempt from dynamic query"},
			{Name: "memberSuggestionsNotify", Type: "bool", Description: "Notify on member suggestions"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "description", "type"},
		IDField:       "id",
		NameField:     "name",
	},
	"commands": {
		Resource:      "commands",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete", "run", "results", "trigger"},
		DefaultFields: []string{"name", "commandType", "command", "schedule", "scheduleRepeatType"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique command identifier"},
			{Name: "name", Type: "string", Description: "Command name", Required: true},
			{Name: "command", Type: "string", Description: "Command body (script)", Required: true},
			{Name: "commandType", Type: "string", Description: "Target OS: linux, mac, windows", Required: true},
			{Name: "user", Type: "string", Description: "Run-as user (e.g. root)"},
			{Name: "schedule", Type: "string", Description: "Cron schedule expression"},
			{Name: "scheduleRepeatType", Type: "string", Description: "Repeat type"},
			{Name: "timeout", Type: "string", Description: "Execution timeout"},
			{Name: "shell", Type: "string", Description: "Shell to use for execution"},
			{Name: "launchType", Type: "string", Description: "Launch type (trigger, manual, repeated)"},
			{Name: "trigger", Type: "string", Description: "Trigger name"},
			{Name: "files", Type: "array", Description: "Attached files"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "commandType", "schedule"},
		IDField:       "_id",
		NameField:     "name",
	},
	"policies": {
		Resource:      "policies",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "results"},
		DefaultFields: []string{"id", "name", "template", "os"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique policy identifier"},
			{Name: "name", Type: "string", Description: "Policy name"},
			{Name: "template", Type: "object", Description: "Policy template details"},
			{Name: "os", Type: "string", Description: "Target operating system"},
			{Name: "values", Type: "array", Description: "Policy configuration values"},
			{Name: "configuredFields", Type: "array", Description: "Configured policy fields"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "os"},
		IDField:       "id",
		NameField:     "name",
	},
	"apps": {
		Resource:      "apps",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"_id", "name", "displayLabel", "ssoType", "status"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique application identifier"},
			{Name: "name", Type: "string", Description: "Application name"},
			{Name: "displayLabel", Type: "string", Description: "Display label shown in user portal"},
			{Name: "ssoType", Type: "string", Description: "SSO type (saml, oidc, bookmark)"},
			{Name: "status", Type: "string", Description: "Application status"},
			{Name: "organization", Type: "string", Description: "Organization ID"},
			{Name: "config", Type: "object", Description: "SSO configuration details"},
			{Name: "beta", Type: "bool", Description: "Whether the app is in beta"},
			{Name: "learnMore", Type: "string", Description: "Documentation URL"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "displayLabel", "ssoType", "status"},
		IDField:       "_id",
		NameField:     "name",
	},
	"auth-policies": {
		Resource:      "auth-policies",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "enable", "disable", "simulate", "blast-radius"},
		DefaultFields: []string{"id", "name", "disabled", "type", "conditions"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique authentication policy identifier"},
			{Name: "name", Type: "string", Description: "Policy name", Required: true},
			{Name: "disabled", Type: "bool", Description: "Whether the policy is disabled"},
			{Name: "type", Type: "string", Description: "Policy type (e.g. user_portal, admin)"},
			{Name: "conditions", Type: "object", Description: "Conditions tree (nested all/any/not with leaf predicates)"},
			{Name: "effect", Type: "string", Description: "Policy effect: allow, deny, allow_with_mfa"},
			{Name: "targets", Type: "object", Description: "Target user groups and applications"},
			{Name: "mfa", Type: "object", Description: "MFA configuration (required, allowEnrollment)"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "disabled", "type"},
		IDField:       "id",
		NameField:     "name",
	},
	"iplists": {
		Resource:      "iplists",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"id", "name", "description"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique IP list identifier"},
			{Name: "name", Type: "string", Description: "IP list name", Required: true},
			{Name: "description", Type: "string", Description: "IP list description"},
			{Name: "ips", Type: "array", Description: "IP entries (single IPs, CIDR ranges, IP ranges)"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name"},
		IDField:       "id",
		NameField:     "name",
	},
	"admins": {
		Resource:      "admins",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"id", "email", "role", "enableMultiFactor"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique administrator identifier"},
			{Name: "email", Type: "string", Description: "Administrator email"},
			{Name: "role", Type: "string", Description: "Admin role (Administrator, Manager, Read Only, etc.)"},
			{Name: "enableMultiFactor", Type: "bool", Description: "Whether MFA is enabled for admin"},
			{Name: "firstname", Type: "string", Description: "First name"},
			{Name: "lastname", Type: "string", Description: "Last name"},
			{Name: "totpEnrolled", Type: "bool", Description: "Whether TOTP is enrolled"},
			{Name: "created", Type: "datetime", Description: "Account creation timestamp"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"email", "role"},
		IDField:       "id",
		NameField:     "email",
	},
	"software": {
		Resource:      "software",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "statuses", "associations", "reclaim-license"},
		DefaultFields: []string{"id", "displayName", "createdAt", "updatedAt"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique software app identifier"},
			{Name: "displayName", Type: "string", Description: "Display name of the software app", Required: true},
			{Name: "settings", Type: "array", Description: "Package configuration settings (nested objects)"},
			{Name: "createdAt", Type: "datetime", Description: "Creation timestamp"},
			{Name: "updatedAt", Type: "datetime", Description: "Last update timestamp"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"displayName", "createdAt", "updatedAt"},
		IDField:       "id",
		NameField:     "displayName",
	},
	"assets": {
		Resource:      "assets",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"id", "Name", "Serial Number", "Status", "Model", "Type"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique asset identifier", ReadOnly: true},
			{Name: "Name", Type: "string", Description: "Asset name"},
			{Name: "Serial Number", Type: "string", Description: "Hardware serial number"},
			{Name: "Status", Type: "string", Description: "Asset status (select field)"},
			{Name: "Model", Type: "string", Description: "Hardware model"},
			{Name: "Type", Type: "string", Description: "Asset type (select field)"},
			{Name: "Vendor", Type: "string", Description: "Hardware vendor"},
			{Name: "Tag", Type: "string", Description: "Organization asset tag"},
			{Name: "Owner", Type: "string", Description: "Assigned owner (user reference)"},
		},
		SortFields:    []string{"Name", "Status", "Model", "Type"},
		FilterSupport: false,
		SortSupport:   false,
		IDField:       "id",
		NameField:     "Name",
	},
	"ldap": {
		Resource:      "ldap",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "samba-domains", "samba-domain-get", "samba-domain-create", "samba-domain-update", "samba-domain-delete"},
		DefaultFields: []string{"id", "name", "userLockoutAction", "userPasswordExpirationAction"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique LDAP server identifier"},
			{Name: "name", Type: "string", Description: "LDAP server name", Required: true},
			{Name: "userLockoutAction", Type: "string", Description: "Action on user lockout"},
			{Name: "userPasswordExpirationAction", Type: "string", Description: "Action on password expiration"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name"},
		IDField:       "id",
		NameField:     "name",
	},
	"access-requests": {
		Resource:      "access-requests",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "revoke"},
		DefaultFields: []string{"accessId", "requestorId", "resourceId", "accessState", "expiry"},
		Fields: []FieldDef{
			{Name: "accessId", Type: "string", Description: "Unique access request identifier", ReadOnly: true},
			{Name: "requestorId", Type: "string", Description: "User ID who requested access"},
			{Name: "resourceId", Type: "string", Description: "Device ID for elevated access"},
			{Name: "resourceType", Type: "string", Description: "Resource type (device)"},
			{Name: "accessState", Type: "string", Description: "Request state (granted, revoked, expired)", ReadOnly: true},
			{Name: "expiry", Type: "datetime", Description: "When the elevated access expires"},
			{Name: "remarks", Type: "string", Description: "Optional remarks"},
			{Name: "additionalAttributes", Type: "object", Description: "Additional attributes (sudo settings)"},
			{Name: "operationId", Type: "string", Description: "Operation identifier", ReadOnly: true},
			{Name: "createdBy", Type: "string", Description: "Creator identifier", ReadOnly: true},
		},
		FilterSupport: true,
		SortSupport:   false,
		IDField:       "accessId",
		NameField:     "",
	},
	"ad": {
		Resource:      "ad",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"id", "domain", "useCase", "groupsEnabled", "delegationState"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique Active Directory identifier"},
			{Name: "domain", Type: "string", Description: "AD domain name", Required: true},
			{Name: "useCase", Type: "string", Description: "Integration use case"},
			{Name: "groupsEnabled", Type: "bool", Description: "Whether group sync is enabled"},
			{Name: "delegationState", Type: "string", Description: "Delegation state"},
			{Name: "permission", Type: "string", Description: "Permission level"},
			{Name: "primaryAgent", Type: "object", Description: "Primary agent details"},
			{Name: "primaryImportAgent", Type: "object", Description: "Primary import agent details"},
			{Name: "updatedAt", Type: "datetime", Description: "Last update timestamp"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"domain", "useCase"},
		IDField:       "id",
		NameField:     "domain",
	},
	"org": {
		Resource:      "org",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "settings", "update"},
		DefaultFields: []string{"_id", "displayName", "created"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique organization identifier"},
			{Name: "id", Type: "string", Description: "Organization ID (short form)"},
			{Name: "displayName", Type: "string", Description: "Organization display name"},
			{Name: "created", Type: "datetime", Description: "Organization creation timestamp"},
			{Name: "logoUrl", Type: "string", Description: "Organization logo URL"},
		},
		FilterSupport: false,
		SortSupport:   false,
		IDField:       "_id",
		NameField:     "displayName",
	},
	"system-insights": {
		Resource:      "system-insights",
		APIVersion:    "v2",
		Verbs:         []string{"list-table", "tables"},
		DefaultFields: []string{"system_id", "collection_time"},
		Fields: []FieldDef{
			{Name: "system_id", Type: "string", Description: "Device system ID"},
			{Name: "collection_time", Type: "datetime", Description: "Data collection timestamp"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"system_id", "collection_time"},
		IDField:       "",
		NameField:     "",
	},
	"radius": {
		Resource:      "radius",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"_id", "name", "networkSourceIp", "authPort", "accountingPort"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique RADIUS server identifier"},
			{Name: "name", Type: "string", Description: "RADIUS server name", Required: true},
			{Name: "networkSourceIp", Type: "string", Description: "Network source IP address"},
			{Name: "authPort", Type: "int", Description: "Authentication port (default 1812)"},
			{Name: "accountingPort", Type: "int", Description: "Accounting port (default 1813)"},
			{Name: "sharedSecret", Type: "string", Description: "RADIUS shared secret", Required: true},
			{Name: "mfa", Type: "string", Description: "MFA configuration"},
			{Name: "userLockoutAction", Type: "string", Description: "Action on user lockout"},
			{Name: "userPasswordExpirationAction", Type: "string", Description: "Action on password expiration"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name"},
		IDField:       "_id",
		NameField:     "name",
	},
	"policy-templates": {
		Resource:      "policy-templates",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get"},
		DefaultFields: []string{"id", "name", "description", "osMetaFamily"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique policy template identifier"},
			{Name: "name", Type: "string", Description: "Template name"},
			{Name: "description", Type: "string", Description: "Template description"},
			{Name: "osMetaFamily", Type: "string", Description: "Target OS family (darwin, linux, windows)"},
			{Name: "state", Type: "string", Description: "Template state"},
			{Name: "displayName", Type: "string", Description: "Display name"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name", "osMetaFamily"},
		IDField:       "id",
		NameField:     "name",
	},
	"apple-mdm": {
		Resource:      "apple-mdm",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "enrollment-profiles", "devices"},
		DefaultFields: []string{"id", "name", "orgName", "defaultIosUserEnrollmentDeviceGroupID"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique Apple MDM configuration identifier"},
			{Name: "name", Type: "string", Description: "MDM configuration name", Required: true},
			{Name: "orgName", Type: "string", Description: "Organization name for MDM certificate"},
			{Name: "defaultIosUserEnrollmentDeviceGroupID", Type: "string", Description: "Default device group for iOS user enrollment"},
			{Name: "defaultSystemGroupID", Type: "string", Description: "Default system group ID"},
			{Name: "appleSignedCert", Type: "string", Description: "Apple signed MDM certificate"},
		},
		FilterSupport: false,
		SortSupport:   false,
		IDField:       "id",
		NameField:     "name",
	},
	"policy-groups": {
		Resource:      "policy-groups",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"id", "name", "description"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique policy group identifier"},
			{Name: "name", Type: "string", Description: "Policy group name", Required: true},
			{Name: "description", Type: "string", Description: "Policy group description"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name"},
		IDField:       "id",
		NameField:     "name",
	},
	"user-states": {
		Resource:      "user-states",
		APIVersion:    "v2",
		Verbs:         []string{"list", "create", "get", "delete"},
		DefaultFields: []string{"id", "userId", "state", "startDate", "endDate"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique user state change identifier"},
			{Name: "userId", Type: "string", Description: "Target user ID"},
			{Name: "state", Type: "string", Description: "Target state: suspended or activated"},
			{Name: "startDate", Type: "datetime", Description: "Date for state change"},
			{Name: "endDate", Type: "datetime", Description: "Optional end date to revert state change"},
		},
		FilterSupport: false,
		SortSupport:   false,
		IDField:       "id",
		NameField:     "",
	},
	"gsuite": {
		Resource:      "gsuite",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "translation-rules", "import-users"},
		DefaultFields: []string{"id", "name", "defaultDomain"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique G Suite integration identifier"},
			{Name: "name", Type: "string", Description: "G Suite integration name"},
			{Name: "defaultDomain", Type: "string", Description: "Default domain for the G Suite directory"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name"},
		IDField:       "id",
		NameField:     "name",
	},
	"office365": {
		Resource:      "office365",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "translation-rules", "import-users"},
		DefaultFields: []string{"id", "name", "defaultDomain"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique Office 365 integration identifier"},
			{Name: "name", Type: "string", Description: "Office 365 integration name"},
			{Name: "defaultDomain", Type: "string", Description: "Default domain for the Office 365 directory"},
		},
		FilterSupport: true,
		SortSupport:   true,
		SortFields:    []string{"name"},
		IDField:       "id",
		NameField:     "name",
	},
	"duo": {
		Resource:      "duo",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "delete", "apps", "app-get", "app-create", "app-delete"},
		DefaultFields: []string{"id", "name"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique Duo account identifier"},
			{Name: "name", Type: "string", Description: "Duo account name"},
		},
		FilterSupport: false,
		SortSupport:   false,
		IDField:       "id",
		NameField:     "name",
	},
	"identity-providers": {
		Resource:      "identity-providers",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete"},
		DefaultFields: []string{"id", "name", "type", "clientId", "url"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique identity provider identifier", ReadOnly: true},
			{Name: "name", Type: "string", Description: "Identity provider display name", Required: true},
			{Name: "type", Type: "string", Description: "Provider type (OIDC, GOOGLE, OKTA, AZURE)", Required: true},
			{Name: "clientId", Type: "string", Description: "OIDC client ID", Required: true},
			{Name: "clientSecret", Type: "string", Description: "OIDC client secret (write-only)"},
			{Name: "url", Type: "string", Description: "OIDC issuer URL", Required: true},
		},
		FilterSupport: false,
		SortSupport:   false,
		SortFields:    []string{"name", "type"},
		IDField:       "id",
		NameField:     "name",
	},
	"saas-management": {
		Resource:      "saas-management",
		APIVersion:    "v2",
		Verbs:         []string{"list", "get", "create", "update", "delete", "accounts", "account-get", "account-delete", "usage", "licenses", "catalog-get"},
		DefaultFields: []string{"id", "catalog_app_id", "status", "discovered_at"},
		Fields: []FieldDef{
			{Name: "id", Type: "string", Description: "Unique SaaS application identifier", ReadOnly: true},
			{Name: "catalog_app_id", Type: "string", Description: "Catalog application identifier"},
			{Name: "name", Type: "string", Description: "Human-readable application name (single-get only)", ReadOnly: true},
			{Name: "status", Type: "string", Description: "Application status (APPROVED, UNAPPROVED, IGNORED)"},
			{Name: "access_restriction", Type: "string", Description: "Access restriction (DEFAULT_ACTION, NO_ACTION, BLOCK, DISMISSIBLE_WARNING)"},
			{Name: "discovered_at", Type: "datetime", Description: "Discovery timestamp", ReadOnly: true},
			{Name: "owner_user_id", Type: "string", Description: "Owner user ID"},
			{Name: "restriction_excluded_group_ids", Type: "array", Description: "Group IDs excluded from restriction"},
		},
		FilterSupport: true,
		SortSupport:   false,
		IDField:       "id",
		NameField:     "catalog_app_id",
	},
	"insights": {
		Resource:      "insights",
		APIVersion:    "insights/v1",
		Verbs:         []string{"query", "count", "distinct"},
		DefaultFields: []string{"timestamp", "event_type", "initiated_by", "client_ip", "success"},
		Fields: []FieldDef{
			{Name: "timestamp", Type: "datetime", Description: "Event timestamp"},
			{Name: "event_type", Type: "string", Description: "Type of event (e.g. sso_auth, admin_login)"},
			{Name: "initiated_by", Type: "object", Description: "Who initiated the event (type, id, email)"},
			{Name: "client_ip", Type: "string", Description: "Client IP address"},
			{Name: "success", Type: "bool", Description: "Whether the event was successful"},
			{Name: "service", Type: "string", Description: "Event service category"},
			{Name: "organization", Type: "string", Description: "Organization ID"},
			{Name: "geoip", Type: "object", Description: "Geographic IP data"},
			{Name: "useragent", Type: "object", Description: "User agent details"},
			{Name: "changes", Type: "array", Description: "Fields changed by this event"},
			{Name: "resource", Type: "object", Description: "Affected resource details"},
		},
		FilterSupport: false,
		SortSupport:   true,
		SortFields:    []string{"timestamp"},
		IDField:       "",
		NameField:     "",
	},
	"custom-emails": {
		Resource:      "custom-emails",
		APIVersion:    "v2",
		Verbs:         []string{"templates", "get", "create", "update", "delete"},
		DefaultFields: []string{"type", "subject", "title"},
		Fields: []FieldDef{
			{Name: "type", Type: "string", Description: "Email template type (e.g. activate_user_custom)"},
			{Name: "subject", Type: "string", Description: "Email subject line"},
			{Name: "title", Type: "string", Description: "Email title"},
			{Name: "body", Type: "string", Description: "Email body text"},
			{Name: "header", Type: "string", Description: "Email header text"},
			{Name: "button", Type: "string", Description: "Email button text"},
		},
		FilterSupport: false,
		SortSupport:   false,
		IDField:       "",
		NameField:     "type",
	},
	"app-templates": {
		Resource:      "app-templates",
		APIVersion:    "v1",
		Verbs:         []string{"list", "get"},
		DefaultFields: []string{"_id", "name", "displayName", "displayLabel", "active"},
		Fields: []FieldDef{
			{Name: "_id", Type: "string", Description: "Unique application template identifier"},
			{Name: "name", Type: "string", Description: "Template name"},
			{Name: "displayName", Type: "string", Description: "Display name"},
			{Name: "displayLabel", Type: "string", Description: "Display label"},
			{Name: "active", Type: "bool", Description: "Whether the template is active"},
			{Name: "organization", Type: "string", Description: "Organization ID"},
			{Name: "config", Type: "object", Description: "Template configuration"},
		},
		FilterSupport: false,
		SortSupport:   true,
		SortFields:    []string{"name", "displayName"},
		IDField:       "_id",
		NameField:     "name",
	},
}

// ResourceNames returns the sorted list of all resource type names.
func ResourceNames() []string {
	names := make([]string, 0, len(Resources))
	for name := range Resources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetResource returns the schema for a named resource, or nil if not found.
func GetResource(name string) *ResourceSchema {
	s, ok := Resources[name]
	if !ok {
		return nil
	}
	return &s
}

// AllResources returns all resource schemas sorted by name.
func AllResources() []ResourceSchema {
	names := ResourceNames()
	all := make([]ResourceSchema, 0, len(names))
	for _, name := range names {
		all = append(all, Resources[name])
	}
	return all
}

// BuildCommandManifest generates a machine-readable manifest of all CLI commands.
func BuildCommandManifest() CommandManifest {
	return CommandManifest{
		Name:        "jc",
		Version:     version.Number,
		Description: "JumpCloud CLI — manage users, devices, groups, policies, commands, insights, and more",
		Resources:   ResourceNames(),
		GlobalFlags: []FlagEntry{
			{Name: "output", Shorthand: "o", Type: "string", Default: "json", Description: "Output format: json, table, csv, human, yaml, ndjson"},
			{Name: "table", Shorthand: "t", Type: "bool", Description: "Shorthand for --output table"},
			{Name: "verbose", Shorthand: "v", Type: "bool", Description: "Enable verbose HTTP logging"},
			{Name: "debug", Type: "bool", Description: "Enable debug logging"},
			{Name: "quiet", Shorthand: "q", Type: "bool", Description: "Suppress output, exit code only"},
			{Name: "force", Shorthand: "f", Type: "bool", Description: "Skip confirmation prompts"},
			{Name: "plan", Type: "bool", Description: "Preview changes without executing"},
			{Name: "ids", Type: "bool", Description: "Output one ID per line (for piping)"},
			{Name: "fields", Type: "string", Description: "Comma-separated list of fields to include"},
			{Name: "exclude", Type: "string", Description: "Comma-separated list of fields to exclude"},
			{Name: "all", Type: "bool", Description: "Include all available fields in output"},
			{Name: "org", Type: "string", Description: "Override active profile for this command"},
			{Name: "api-key", Type: "string", Description: "Override API key for this command"},
			{Name: "no-cache", Type: "bool", Description: "Bypass name-to-ID cache"},
			{Name: "no-color", Type: "bool", Description: "Disable color output"},
			{Name: "non-interactive", Type: "bool", Description: "Disable all interactive prompts"},
		},
		Commands: []CommandEntry{
			{
				Path:        "jc auth",
				Description: "Authentication commands",
				Long:        "Manage JumpCloud credentials and switch between organizations. The CLI supports two credential types: a static API key (read from `JC_API_KEY`, the config, or the system keychain), or an OAuth service account (client ID + secret), with automatic token refresh. Credentials can be stored as plaintext, in macOS Keychain / GNOME libsecret / Windows Credential Manager via a `keychain://` reference, or supplied per-invocation with `--api-key`. Multiple named profiles let MSPs and admins flip between orgs without re-authenticating (`jc auth switch <profile>`), and `jc auth status` reveals the active profile, credential type, fingerprint, and source.",
				Subcommands: []string{"login", "logout", "status", "switch"},
			},
			{
				Path:        "jc audit",
				Description: "Run cross-resource health checks (security, compliance, hygiene, identity)",
				Long:        "A composable check registry that audits the entire org in one pass — admins without MFA, MFA adoption rate, FDE coverage, stale devices, disabled auth policies, suspicious admin lifecycle events, and more. Each finding is severity-tagged (info → critical), tagged with a `resource_ref` for downstream grouping, and ships with a `remediation_hint` that names the exact `jc` command to fix it. Use `--category security|compliance|hygiene|identity` to scope, `--severity high` to filter to actionable findings, and `--exit-code --threshold high` to gate CI pipelines. The same primitive powers the `jc-security-audit` and `jc-compliance-check` skills (which now interpret structured findings rather than scripting raw queries). Adding a new check is one Register call in `internal/audit/checks.go` — the registry, CLI surface, JSON shape, and skill prompts all update automatically.",
				Subcommands: []string{"verify"},
				Flags: []FlagEntry{
					{Name: "category", Type: "string[]", Description: "Restrict to one or more categories: security, compliance, hygiene, identity"},
					{Name: "severity", Type: "string", Description: "Show only findings at or above this severity (info, low, medium, high, critical)"},
					{Name: "threshold", Type: "string", Default: "high", Description: "Severity threshold used by --exit-code"},
					{Name: "exit-code", Type: "bool", Description: "Exit with code 1 if any finding meets or exceeds --threshold (for CI gating)"},
				},
			},
			{
				Path:        "jc doctor",
				Description: "No-auth diagnostic — env, config, auth resolution, API connectivity",
				Long:        "A pre-flight check for any environment where `jc` is about to run. Reports the active profile, credential source (flag / env / keychain / config), fingerprint of the resolved key, config file location, and runs a single read-only probe against the JumpCloud API to confirm the credentials actually authenticate. Works without auth (skips the API probe gracefully) so it's safe to run in a Dockerfile build step or fresh-clone sanity check. Useful as the first command in a runbook or on-call playbook — when something's wrong with `jc`, this is the fastest path to the cause.",
				Subcommands: []string{},
				Flags: []FlagEntry{
					{Name: "probe-timeout", Type: "duration", Default: "5s", Description: "Timeout for the live API probe"},
					{Name: "skip-probe", Type: "bool", Description: "Skip the live API probe (no network)"},
				},
			},
			{
				Path:        "jc config",
				Description: "Configuration management",
				Long:        "View and update jc CLI configuration — default output format, color/pager behavior, TUI refresh interval, plan-mode safety toggles, cache TTLs, and dozens of other preferences. Settings live in `~/.config/jc/config.yaml` and can be overridden per-command via `JC_*` environment variables or flags (env > flag > config > built-in default). Use `jc config view` for the full effective configuration including override sources, or `jc config set <key> <value>` to persist a change.",
				Subcommands: []string{"view", "set"},
			},
			{
				Path:        "jc users",
				Description: "Manage JumpCloud system users",
				Subcommands: []string{"list", "get", "create", "update", "delete", "search", "lock", "unlock", "reset-mfa", "reset-password", "ssh-keys", "ssh-key-add", "ssh-key-delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list/search)"},
					{Name: "sort", Type: "string", Description: "Sort field, prefix - for descending (list/search)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "search", Type: "string", Description: "Full-text search term (list)"},
				},
			},
			{
				Path:        "jc devices",
				Description: "Manage JumpCloud devices (systems)",
				Subcommands: []string{"list", "get", "update", "delete", "search", "lock", "restart", "erase", "fde-key"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field, prefix - for descending (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "confirm-erase", Type: "bool", Description: "Required safety flag for erase command"},
				},
			},
			{
				Path:        "jc groups",
				Description: "Manage user and device groups",
				Subcommands: []string{"user list", "user get", "user create", "user update", "user delete", "device list", "device get", "device create", "device update", "device delete", "add-member", "remove-member"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc commands",
				Description: "Manage JumpCloud commands",
				Subcommands: []string{"list", "get", "create", "update", "delete", "run", "results", "trigger"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "type", Type: "string", Description: "Command type filter: linux, mac, windows (list)"},
					{Name: "data", Type: "string", Description: "JSON payload for trigger"},
				},
			},
			{
				Path:        "jc policies",
				Description: "Manage JumpCloud policies",
				Subcommands: []string{"list", "get", "create", "update", "delete", "results"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc apps",
				Description: "Manage SSO applications",
				Subcommands: []string{"list", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "sso-type", Type: "string", Description: "SSO type (create, required)"},
					{Name: "config", Type: "string", Description: "SSO-specific config as JSON (create/update)"},
				},
			},
			{
				Path:        "jc admins",
				Description: "Manage JumpCloud administrators",
				Subcommands: []string{"list", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "email", Type: "string", Description: "Admin email address (create, required)"},
					{Name: "role", Type: "string", Description: "Admin role name (create/update)"},
					{Name: "enable-mfa", Type: "bool", Description: "Enable multi-factor authentication (create/update)"},
					{Name: "disable-mfa", Type: "bool", Description: "Disable multi-factor authentication (update)"},
				},
			},
			{
				Path:        "jc insights",
				Description: "Query Directory Insights audit events",
				Subcommands: []string{"query", "count", "distinct", "save", "run", "saved"},
				Flags: []FlagEntry{
					{Name: "service", Type: "string", Description: "Event service: sso, radius, ldap, user_portal, admin, mdm, directory, software, systems, password_manager, all"},
					{Name: "last", Type: "string", Description: "Time range: 24h, 7d, 30d, 1m"},
					{Name: "start", Type: "string", Description: "Start time (RFC 3339 or YYYY-MM-DD)"},
					{Name: "end", Type: "string", Description: "End time (RFC 3339 or YYYY-MM-DD)"},
					{Name: "event-type", Type: "string", Description: "Filter by event type"},
					{Name: "limit", Type: "int", Description: "Maximum events to return"},
					{Name: "sort", Type: "string", Description: "Sort field"},
				},
			},
			{
				Path:        "jc auth-policies",
				Description: "Manage authentication policies for conditional access",
				Subcommands: []string{"list", "get", "create", "update", "delete", "enable", "disable", "simulate", "blast-radius"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "conditions", Type: "string", Description: "Conditions tree as raw JSON (create/update)"},
					{Name: "user", Type: "string", Description: "User name or ID (simulate)"},
					{Name: "ip", Type: "string", Description: "Source IP address (simulate)"},
					{Name: "device", Type: "string", Description: "Device name or ID (simulate)"},
					{Name: "location", Type: "string", Description: "Country code (simulate)"},
				},
			},
			{
				Path:        "jc iplists",
				Description: "Manage IP lists for authentication policies",
				Subcommands: []string{"list", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "ips", Type: "string", Description: "Comma-separated IP entries (create/update)"},
				},
			},
			{
				Path:        "jc identity-providers",
				Description: "Manage JumpCloud identity providers for SSO/OIDC federation",
				Subcommands: []string{"list", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum results (list)"},
					{Name: "name", Type: "string", Description: "Provider name (create/update)"},
					{Name: "type", Type: "string", Description: "Provider type: OIDC, GOOGLE, OKTA, AZURE (create)"},
					{Name: "client-id", Type: "string", Description: "OIDC client ID (create/update)"},
					{Name: "client-secret", Type: "string", Description: "OIDC client secret (create/update)"},
					{Name: "url", Type: "string", Description: "OIDC issuer URL (create/update)"},
				},
			},
			{
				Path:        "jc saas-management",
				Description: "Manage JumpCloud SaaS Management applications",
				Subcommands: []string{"list", "get", "create", "update", "delete", "accounts", "account-get", "account-delete", "usage", "licenses", "catalog-get"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "catalog-app-id", Type: "string", Description: "Catalog application ID (create)"},
					{Name: "status", Type: "string", Description: "App status: APPROVED, UNAPPROVED, IGNORED (create/update)"},
					{Name: "access-restriction", Type: "string", Description: "Access restriction: DEFAULT_ACTION, NO_ACTION, BLOCK, DISMISSIBLE_WARNING (create/update)"},
					{Name: "account-id", Type: "string", Description: "Account ID (account-get/account-delete)"},
					{Name: "day-count", Type: "int", Description: "Number of days of usage data (usage, default 30)"},
				},
			},
			{
				Path:        "jc software",
				Description: "Manage JumpCloud software apps",
				Subcommands: []string{"list", "get", "create", "update", "delete", "statuses", "associations", "reclaim-license"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "name", Type: "string", Description: "App display name (create/update)"},
					{Name: "settings", Type: "string", Description: "Package settings as raw JSON (create/update)"},
				},
			},
			{
				Path:        "jc assets",
				Description: "Manage JumpCloud assets (devices, accessories, locations)",
				Subcommands: []string{"devices", "accessories", "locations"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "field", Type: "string[]", Description: "Set field value as 'Label=Value' (create/update)"},
				},
			},
			{
				Path:        "jc ldap",
				Description: "Manage JumpCloud LDAP servers",
				Subcommands: []string{"list", "get", "create", "update", "delete", "samba-domains", "samba-domain-get", "samba-domain-create", "samba-domain-update", "samba-domain-delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "name", Type: "string", Description: "LDAP server name (create/update) or samba domain workgroup name"},
					{Name: "user-lockout-action", Type: "string", Description: "Action on user lockout (create/update)"},
					{Name: "user-password-expiration-action", Type: "string", Description: "Action on password expiration (create/update)"},
					{Name: "domain-id", Type: "string", Description: "Samba domain ID (samba-domain-get/update/delete)"},
					{Name: "sid", Type: "string", Description: "Samba domain security identifier (samba-domain-create/update)"},
				},
			},
			{
				Path:        "jc access-requests",
				Description: "Manage JumpCloud temporary elevated device privilege requests",
				Subcommands: []string{"list", "get", "create", "update", "revoke"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "user", Type: "string", Description: "User name or ID (create)"},
					{Name: "device", Type: "string", Description: "Device name or ID (create)"},
					{Name: "expiry", Type: "string", Description: "Expiry time RFC 3339 (create/update)"},
					{Name: "sudo", Type: "bool", Description: "Enable sudo access (create)"},
					{Name: "sudo-nopasswd", Type: "bool", Description: "Enable passwordless sudo (create)"},
					{Name: "remarks", Type: "string", Description: "Optional remarks (create/update)"},
				},
			},
			{
				Path:        "jc ad",
				Description: "Manage JumpCloud Active Directory integrations",
				Subcommands: []string{"list", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "domain", Type: "string", Description: "AD domain name (create, required)"},
					{Name: "use-case", Type: "string", Description: "Integration use case (create/update)"},
					{Name: "groups-enabled", Type: "bool", Description: "Enable group sync (update)"},
				},
			},
			{
				Path:        "jc org",
				Description: "View and update JumpCloud organization information",
				Subcommands: []string{"list", "get", "settings", "update"},
				Flags: []FlagEntry{
					{Name: "name", Type: "string", Description: "Organization display name (update)"},
					{Name: "settings-json", Type: "string", Description: "Raw JSON for complex settings fields (update)"},
				},
			},
			{
				Path:        "jc system-insights",
				Description: "Query osquery system insight tables",
				Subcommands: []string{"list", "tables"},
				Flags: []FlagEntry{
					{Name: "system-id", Type: "string", Description: "Filter by device hostname or ID"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions"},
					{Name: "limit", Type: "int", Description: "Maximum number of results"},
					{Name: "sort", Type: "string", Description: "Sort field"},
				},
			},
			{
				Path:        "jc radius",
				Description: "Manage RADIUS servers",
				Subcommands: []string{"list", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "name", Type: "string", Description: "RADIUS server name (create/update)"},
					{Name: "shared-secret", Type: "string", Description: "RADIUS shared secret (create, required)"},
					{Name: "auth-port", Type: "int", Default: "1812", Description: "Authentication port (create/update)"},
					{Name: "accounting-port", Type: "int", Default: "1813", Description: "Accounting port (create/update)"},
				},
			},
			{
				Path:        "jc policy-templates",
				Description: "View policy templates",
				Subcommands: []string{"list", "get"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc apple-mdm",
				Description: "Manage Apple MDM configurations",
				Subcommands: []string{"list", "get", "create", "update", "delete", "enrollment-profiles", "devices"},
				Flags: []FlagEntry{
					{Name: "name", Type: "string", Description: "MDM configuration name (create/update)"},
					{Name: "org-name", Type: "string", Description: "Organization name (create/update)"},
				},
			},
			{
				Path:        "jc policy-groups",
				Description: "Manage policy groups",
				Subcommands: []string{"list", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
					{Name: "name", Type: "string", Description: "Policy group name (create/update)"},
					{Name: "description", Type: "string", Description: "Policy group description (create/update)"},
				},
			},
			{
				Path:        "jc user-states",
				Description: "Manage scheduled user state transitions",
				Subcommands: []string{"list", "create", "get", "delete"},
				Flags: []FlagEntry{
					{Name: "user", Type: "string", Description: "User name or ID (create, required)"},
					{Name: "state", Type: "string", Description: "Target state: suspended or activated (create, required)"},
					{Name: "start-date", Type: "string", Description: "Date for state change (create, required)"},
					{Name: "end-date", Type: "string", Description: "Optional end date to revert (create)"},
				},
			},
			{
				Path:        "jc gsuite",
				Description: "Manage JumpCloud Google Workspace (G Suite) integrations",
				Subcommands: []string{"list", "get", "translation-rules", "import-users"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc office365",
				Description: "Manage JumpCloud Office 365 integrations",
				Subcommands: []string{"list", "get", "translation-rules", "import-users"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
					{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
				},
			},
			{
				Path:        "jc duo",
				Description: "Manage JumpCloud Duo accounts and applications",
				Subcommands: []string{"list", "get", "create", "delete", "apps", "app-get", "app-create", "app-delete"},
				Flags: []FlagEntry{
					{Name: "name", Type: "string", Description: "Duo account name (create)"},
					{Name: "app-id", Type: "string", Description: "Duo application ID (app-get, app-delete)"},
					{Name: "api-host", Type: "string", Description: "Duo API host (app-create)"},
				},
			},
			{
				Path:        "jc custom-emails",
				Description: "Manage custom email templates",
				Subcommands: []string{"templates", "get", "create", "update", "delete"},
				Flags: []FlagEntry{
					{Name: "type", Type: "string", Description: "Custom email type (create, required)"},
					{Name: "subject", Type: "string", Description: "Email subject line (create/update)"},
					{Name: "title", Type: "string", Description: "Email title (create/update)"},
					{Name: "body", Type: "string", Description: "Email body text (create/update)"},
					{Name: "header", Type: "string", Description: "Email header text (create/update)"},
					{Name: "button", Type: "string", Description: "Email button text (create/update)"},
				},
			},
			{
				Path:        "jc app-templates",
				Description: "View application templates",
				Subcommands: []string{"list", "get"},
				Flags: []FlagEntry{
					{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
					{Name: "sort", Type: "string", Description: "Sort field (list)"},
				},
			},
			{
				Path:        "jc graph",
				Description: "Manage JumpCloud resource associations",
				Subcommands: []string{"traverse", "bind", "unbind"},
				Flags: []FlagEntry{
					{Name: "from", Type: "string", Description: "Source: type:identifier (e.g. user:jdoe)"},
					{Name: "to", Type: "string", Description: "Target type: user, system, user_group, system_group, application, policy, command"},
				},
			},
			{
				Path:        "jc bulk",
				Description: "Bulk operations from CSV files",
				Subcommands: []string{"users"},
				Flags: []FlagEntry{
					{Name: "file", Type: "string", Description: "Path to CSV file"},
				},
			},
			{
				Path:        "jc recipe",
				Description: "Manage and run automation recipes",
				Long:        "Recipes turn JumpCloud workflows into declarative, version-controlled YAML files instead of one-off shell scripts. Each step is a `jc` invocation with template variables (`{{ .param.user }}`, `{{ .step.created_user.id }}`), conditional execution based on prior step outputs, and structured output capture that downstream steps can reference — so a single recipe can create a user, add them to groups, assign devices, send a welcome email, and bail out cleanly if any step fails or a precondition isn't met. The `--plan` flag walks every step without mutation, surfacing exactly what would change before you commit; `--param k=v` makes the same recipe reusable across users, orgs, or MSP customers. Built-ins ship for the workflows admins write over and over (onboarding, offboarding, MFA-reset campaigns, quarterly compliance audits); custom recipes live in `~/.config/jc/recipes/` and can be authored in `$EDITOR` via the TUI (`jc tui` → recipes → `n`). Recipes are also a first-class MCP primitive — agents can invoke `recipe.run` as a single tool call instead of orchestrating five separate ones, dramatically reducing the surface area for LLM missteps on multi-step changes.",
				Subcommands: []string{"list", "show", "run", "validate", "create", "import", "export"},
				Flags: []FlagEntry{
					{Name: "param", Type: "string[]", Description: "Recipe parameters as key=value (run)"},
					{Name: "file", Type: "string", Description: "Output file path (export)"},
				},
			},
			{
				Path:        "jc mcp",
				Description: "MCP server for AI agent integration",
				Long:        "Run the jc CLI as a Model Context Protocol (MCP) server, exposing JumpCloud resources as typed tools to MCP-aware clients (Claude Code, Claude Desktop, Cursor, and any other host that speaks MCP). Includes per-minute rate limiting, an optional `--read-only` mode that disables every mutation tool, and a step-up authentication flow (TTY prompt, Touch ID, or webhook) for high-impact operations. Transport is stdio by default — point your MCP client at `jc mcp serve` and the CLI's full surface area becomes available to the agent.",
				Subcommands: []string{"serve"},
				Flags: []FlagEntry{
					{Name: "rate-limit", Type: "int", Default: "60", Description: "Maximum tool calls per minute"},
					{Name: "read-only", Type: "bool", Description: "Disable all mutation tools"},
				},
			},
			{
				Path:        "jc schema",
				Description: "Machine-readable schema and command manifest",
				Long:        "Return machine-readable JSON for every JumpCloud resource type and the full CLI command tree. Designed for LLMs, IDE plugins, code generators, and the public showcase site itself — `jc schema resources` lists every resource with its API version and supported verbs, `jc schema <resource>` returns the typed field list, and `jc schema commands` returns the full command manifest (paths, subcommands, flags, descriptions). The schema is the single source of truth — this site, the MCP server, and `jc ask` all read from it.",
				Subcommands: []string{"resources", "commands"},
			},
			{
				Path:        "jc ask",
				Description: "Translate natural language queries into jc CLI commands",
				Long:        "Translate plain-English questions into runnable `jc` commands using the bundled schema manifest plus an LLM prompt template. Useful for one-off queries like *\"show me admins who haven't logged in for 90 days\"* without remembering exact `jc insights query` flag syntax, or for *\"suspend everyone in the contractors group\"* without hand-piping `jc groups user list` into a `for` loop. The translated command is always printed for review before execution, and pairs naturally with `--plan` to preview any mutation before committing.",
			},
			{
				Path:        "jc explain",
				Description: "Explain what a command would do without executing",
				Long:        "Describe what a `jc` invocation would do — in plain English — without running it. Useful for sanity-checking LLM-generated commands before execution, understanding an unfamiliar invocation copied from a runbook, or onboarding new admins. The explanation covers the action type, target resource, affected scope (single object vs. batch), and a reversibility warning for destructive operations (`delete`, `lock`, `erase`).",
			},
		},
	}
}
