package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/simulator"
	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client factory vars for test injection.
var (
	newV1ClientFunc       = func() (*api.V1Client, error) { return api.NewV1Client() }
	newV2ClientFunc       = func() (*api.V2Client, error) { return api.NewV2Client() }
	newInsightsClientFunc = func() (*api.InsightsClient, error) { return api.NewInsightsClient() }
)

// --- Input types for typed tools ---

type listInput struct {
	Limit  int      `json:"limit,omitempty" jsonschema:"Maximum number of results to return (0 = all)"`
	Sort   string   `json:"sort,omitempty" jsonschema:"Field to sort by. Prefix with - for descending (e.g. -created)"`
	Filter []string `json:"filter,omitempty" jsonschema:"Filter expressions (e.g. field=value)"`
}

type getInput struct {
	Identifier string `json:"identifier" jsonschema:"Name or ID of the resource"`
}

type userCreateInput struct {
	Username   string `json:"username" jsonschema:"Username"`
	Email      string `json:"email" jsonschema:"Email address"`
	Firstname  string `json:"firstname,omitempty" jsonschema:"First name"`
	Lastname   string `json:"lastname,omitempty" jsonschema:"Last name"`
	Department string `json:"department,omitempty" jsonschema:"Department"`
}

type userUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Username or ID of the user to update"`
	Email      string `json:"email,omitempty" jsonschema:"New email address"`
	Firstname  string `json:"firstname,omitempty" jsonschema:"New first name"`
	Lastname   string `json:"lastname,omitempty" jsonschema:"New last name"`
	Department string `json:"department,omitempty" jsonschema:"New department"`
	JobTitle   string `json:"jobTitle,omitempty" jsonschema:"New job title"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute the update. Without this the tool returns a plan."`
}

type destructiveInput struct {
	Identifier string `json:"identifier" jsonschema:"Name or ID of the resource"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type membershipInput struct {
	Group      string `json:"group" jsonschema:"Group name or ID"`
	Member     string `json:"member" jsonschema:"User or device name or ID to add/remove"`
	MemberType string `json:"member_type" jsonschema:"Type of member: user or device"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type insightsQueryInput struct {
	Service   string `json:"service" jsonschema:"Event service to query (sso/radius/ldap/user_portal/admin/mdm/directory/software/systems/password_manager/all)"`
	Last      string `json:"last,omitempty" jsonschema:"Time range shortcut (24h/7d/30d/1m)"`
	Start     string `json:"start,omitempty" jsonschema:"Start time (RFC 3339 or YYYY-MM-DD)"`
	End       string `json:"end,omitempty" jsonschema:"End time (RFC 3339 or YYYY-MM-DD)"`
	EventType string `json:"event_type,omitempty" jsonschema:"Filter by event type (e.g. sso_auth_failed)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum events to return (0 = all)"`
}

type insightsCountInput struct {
	Service   string `json:"service" jsonschema:"Event service to query"`
	Last      string `json:"last,omitempty" jsonschema:"Time range shortcut (24h/7d/30d/1m)"`
	Start     string `json:"start,omitempty" jsonschema:"Start time (RFC 3339 or YYYY-MM-DD)"`
	End       string `json:"end,omitempty" jsonschema:"End time (RFC 3339 or YYYY-MM-DD)"`
	EventType string `json:"event_type,omitempty" jsonschema:"Filter by event type"`
}

type commandRunInput struct {
	Command string `json:"command" jsonschema:"Command name or ID to run"`
	Target  string `json:"target" jsonschema:"Device hostname/ID or device group name/ID to run on"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type recipeRunInput struct {
	Name   string            `json:"name" jsonschema:"Recipe name to run"`
	Params map[string]string `json:"params,omitempty" jsonschema:"Recipe parameters as key-value pairs"`
}

type authPolicyCreateInput struct {
	Name       string `json:"name" jsonschema:"Policy name"`
	Type       string `json:"type,omitempty" jsonschema:"Policy type (e.g. user_portal, admin)"`
	Conditions string `json:"conditions,omitempty" jsonschema:"Conditions tree as raw JSON string"`
	MFA        bool   `json:"mfa,omitempty" jsonschema:"Require MFA for this policy"`
	Disabled   bool   `json:"disabled,omitempty" jsonschema:"Create in disabled state"`
}

type authPolicyUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Policy name or ID to update"`
	Name       string `json:"name,omitempty" jsonschema:"New policy name"`
	Conditions string `json:"conditions,omitempty" jsonschema:"New conditions tree as raw JSON"`
	Disabled   *bool  `json:"disabled,omitempty" jsonschema:"Set to true to disable or false to enable"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type simulateInput struct {
	Policy   string `json:"policy" jsonschema:"Policy name or ID to simulate"`
	User     string `json:"user" jsonschema:"User name or ID"`
	IP       string `json:"ip,omitempty" jsonschema:"Source IP address"`
	Device   string `json:"device,omitempty" jsonschema:"Device name or ID"`
	Location string `json:"location,omitempty" jsonschema:"Country code (e.g. US, DE)"`
}

type blastRadiusInput struct {
	Policy string `json:"policy" jsonschema:"Policy name or ID"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of affected users to return (default 100)"`
}

type ipListCreateInput struct {
	Name        string   `json:"name" jsonschema:"IP list name"`
	Description string   `json:"description,omitempty" jsonschema:"IP list description"`
	IPs         []string `json:"ips" jsonschema:"IP entries (single IPs, CIDR ranges, IP ranges)"`
}

type ipListUpdateInput struct {
	Identifier  string   `json:"identifier" jsonschema:"IP list name or ID to update"`
	Name        string   `json:"name,omitempty" jsonschema:"New IP list name"`
	Description string   `json:"description,omitempty" jsonschema:"New description"`
	IPs         []string `json:"ips,omitempty" jsonschema:"New IP entries (replaces existing)"`
	Execute     bool     `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type softwareCreateInput struct {
	Name     string `json:"name" jsonschema:"Display name for the software app"`
	Settings string `json:"settings,omitempty" jsonschema:"Package settings as raw JSON array"`
}

type softwareUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Software app name or ID to update"`
	Name       string `json:"name,omitempty" jsonschema:"New display name"`
	Settings   string `json:"settings,omitempty" jsonschema:"New package settings as raw JSON array"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type assetCreateInput struct {
	Name         string `json:"name" jsonschema:"Asset name"`
	SerialNumber string `json:"serialNumber,omitempty" jsonschema:"Hardware serial number"`
	AssetTag     string `json:"assetTag,omitempty" jsonschema:"Organization asset tag"`
	Status       string `json:"status,omitempty" jsonschema:"Asset status"`
	Type         string `json:"type,omitempty" jsonschema:"Asset type"`
	SystemID     string `json:"systemId,omitempty" jsonschema:"Linked JumpCloud system ID"`
}

type assetUpdateInput struct {
	Identifier   string `json:"identifier" jsonschema:"Asset name or ID to update"`
	Name         string `json:"name,omitempty" jsonschema:"New asset name"`
	SerialNumber string `json:"serialNumber,omitempty" jsonschema:"New serial number"`
	AssetTag     string `json:"assetTag,omitempty" jsonschema:"New asset tag"`
	Status       string `json:"status,omitempty" jsonschema:"New status"`
	Type         string `json:"type,omitempty" jsonschema:"New asset type"`
	SystemID     string `json:"systemId,omitempty" jsonschema:"New system ID"`
	Execute      bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type ldapCreateInput struct {
	Name                         string `json:"name" jsonschema:"LDAP server name"`
	UserLockoutAction            string `json:"user_lockout_action,omitempty" jsonschema:"Action on user lockout"`
	UserPasswordExpirationAction string `json:"user_password_expiration_action,omitempty" jsonschema:"Action on password expiration"`
}

type ldapUpdateInput struct {
	Identifier                   string `json:"identifier" jsonschema:"LDAP server name or ID to update"`
	Name                         string `json:"name,omitempty" jsonschema:"New server name"`
	UserLockoutAction            string `json:"user_lockout_action,omitempty" jsonschema:"New lockout action"`
	UserPasswordExpirationAction string `json:"user_password_expiration_action,omitempty" jsonschema:"New password expiration action"`
	Execute                      bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type adCreateInput struct {
	Domain  string `json:"domain" jsonschema:"Active Directory domain name"`
	UseCase string `json:"use_case,omitempty" jsonschema:"Integration use case"`
}

type adUpdateInput struct {
	Identifier    string `json:"identifier" jsonschema:"AD domain or ID to update"`
	UseCase       string `json:"use_case,omitempty" jsonschema:"New use case"`
	GroupsEnabled *bool  `json:"groups_enabled,omitempty" jsonschema:"Enable or disable group sync"`
	Execute       bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type adminCreateInput struct {
	Email     string `json:"email" jsonschema:"Administrator email address"`
	Role      string `json:"role,omitempty" jsonschema:"Admin role (e.g. Administrator, Manager, Read Only)"`
	EnableMFA bool   `json:"enable_mfa,omitempty" jsonschema:"Enable multi-factor authentication"`
}

type adminUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Admin email or ID to update"`
	Role       string `json:"role,omitempty" jsonschema:"New admin role"`
	EnableMFA  *bool  `json:"enable_mfa,omitempty" jsonschema:"Set to true to enable MFA or false to disable"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type planInput struct {
	Command string `json:"command" jsonschema:"A jc command string to preview (e.g. users delete jdoe)"`
}

type explainInput struct {
	Command string `json:"command" jsonschema:"A jc command string to explain (e.g. users delete jdoe)"`
}

type searchInput struct {
	Term  string `json:"term" jsonschema:"Search term to match across multiple fields"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of results to return (0 = all)"`
	Sort  string `json:"sort,omitempty" jsonschema:"Field to sort by. Prefix with - for descending"`
}

type deviceUpdateInput struct {
	Identifier                     string `json:"identifier" jsonschema:"Device hostname or ID to update"`
	DisplayName                    string `json:"displayName,omitempty" jsonschema:"New display name"`
	AllowSshPasswordAuthentication *bool  `json:"allowSshPasswordAuthentication,omitempty" jsonschema:"Allow SSH password auth"`
	AllowSshRootLogin              *bool  `json:"allowSshRootLogin,omitempty" jsonschema:"Allow SSH root login"`
	AllowMultiFactorAuthentication *bool  `json:"allowMultiFactorAuthentication,omitempty" jsonschema:"Allow multi-factor auth"`
	AllowPublicKeyAuthentication   *bool  `json:"allowPublicKeyAuthentication,omitempty" jsonschema:"Allow public key auth"`
	Execute                        bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type commandCreateInput struct {
	Name        string `json:"name" jsonschema:"Command name"`
	Command     string `json:"command" jsonschema:"Command body to execute"`
	CommandType string `json:"command_type" jsonschema:"Command type: linux, mac, windows"`
}

type commandUpdateInput struct {
	Identifier  string `json:"identifier" jsonschema:"Command name or ID to update"`
	Name        string `json:"name,omitempty" jsonschema:"New command name"`
	Command     string `json:"command,omitempty" jsonschema:"New command body"`
	CommandType string `json:"command_type,omitempty" jsonschema:"New command type: linux, mac, windows"`
	Execute     bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type resultsInput struct {
	Identifier string `json:"identifier" jsonschema:"Name or ID of the resource to get results for"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum number of results to return (0 = all)"`
	Sort       string `json:"sort,omitempty" jsonschema:"Field to sort by. Prefix with - for descending"`
}

type policyCreateInput struct {
	Name       string `json:"name" jsonschema:"Policy name"`
	TemplateID string `json:"template_id" jsonschema:"Policy template ID"`
	Values     string `json:"values,omitempty" jsonschema:"Policy values as raw JSON object"`
}

type policyUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Policy name or ID to update"`
	Name       string `json:"name,omitempty" jsonschema:"New policy name"`
	Values     string `json:"values,omitempty" jsonschema:"New policy values as raw JSON object"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type groupCreateInput struct {
	Name        string `json:"name" jsonschema:"Group name"`
	Description string `json:"description,omitempty" jsonschema:"Group description"`
}

type groupUpdateInput struct {
	Identifier  string `json:"identifier" jsonschema:"Group name or ID to update"`
	Name        string `json:"name,omitempty" jsonschema:"New group name"`
	Description string `json:"description,omitempty" jsonschema:"New group description"`
	Execute     bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type appCreateInput struct {
	Name    string `json:"name" jsonschema:"Application name"`
	SsoType string `json:"sso_type,omitempty" jsonschema:"SSO type (e.g. saml, oidc, bookmark)"`
	Config  string `json:"config,omitempty" jsonschema:"Application configuration as raw JSON"`
}

type appUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Application name or ID to update"`
	Name       string `json:"name,omitempty" jsonschema:"New application name"`
	Config     string `json:"config,omitempty" jsonschema:"New configuration as raw JSON"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type systemInsightsListInput struct {
	Table    string   `json:"table" jsonschema:"System insights table name (e.g. os_version, disk_encryption, apps)"`
	SystemID string   `json:"system_id,omitempty" jsonschema:"Device hostname or ID to filter results"`
	Limit    int      `json:"limit,omitempty" jsonschema:"Maximum number of results to return (0 = all)"`
	Sort     string   `json:"sort,omitempty" jsonschema:"Field to sort by"`
	Filter   []string `json:"filter,omitempty" jsonschema:"Filter expressions (e.g. field=value)"`
}

type radiusCreateInput struct {
	Name           string `json:"name" jsonschema:"RADIUS server name"`
	SharedSecret   string `json:"shared_secret" jsonschema:"RADIUS shared secret"`
	AuthPort       int    `json:"auth_port,omitempty" jsonschema:"Authentication port (default 1812)"`
	AccountingPort int    `json:"accounting_port,omitempty" jsonschema:"Accounting port (default 1813)"`
}

type radiusUpdateInput struct {
	Identifier     string `json:"identifier" jsonschema:"RADIUS server name or ID to update"`
	Name           string `json:"name,omitempty" jsonschema:"New server name"`
	SharedSecret   string `json:"shared_secret,omitempty" jsonschema:"New shared secret"`
	AuthPort       int    `json:"auth_port,omitempty" jsonschema:"New authentication port"`
	AccountingPort int    `json:"accounting_port,omitempty" jsonschema:"New accounting port"`
	Execute        bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type appleMDMCreateInput struct {
	Name    string `json:"name" jsonschema:"MDM configuration name"`
	OrgName string `json:"org_name,omitempty" jsonschema:"Organization name for the MDM certificate"`
}

type appleMDMUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Apple MDM name or ID to update"`
	Name       string `json:"name,omitempty" jsonschema:"New MDM configuration name"`
	OrgName    string `json:"org_name,omitempty" jsonschema:"New organization name"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type policyGroupCreateInput struct {
	Name        string `json:"name" jsonschema:"Policy group name"`
	Description string `json:"description,omitempty" jsonschema:"Policy group description"`
}

type policyGroupUpdateInput struct {
	Identifier  string `json:"identifier" jsonschema:"Policy group name or ID to update"`
	Name        string `json:"name,omitempty" jsonschema:"New policy group name"`
	Description string `json:"description,omitempty" jsonschema:"New description"`
	Execute     bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type userStateCreateInput struct {
	User      string `json:"user" jsonschema:"User name or ID"`
	State     string `json:"state" jsonschema:"Target state: suspended or activated"`
	StartDate string `json:"start_date" jsonschema:"Date for state change (YYYY-MM-DD or RFC 3339)"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"Optional end date to revert the state change"`
}

type orgUpdateInput struct {
	ID           string `json:"id" jsonschema:"Organization ID"`
	Name         string `json:"name,omitempty" jsonschema:"New organization display name"`
	SettingsJSON string `json:"settings_json,omitempty" jsonschema:"Raw JSON for organization settings"`
	Execute      bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type sshKeyAddInput struct {
	User      string `json:"user" jsonschema:"Username or ID of the user"`
	Name      string `json:"name" jsonschema:"Label for the SSH key"`
	PublicKey string `json:"public_key" jsonschema:"SSH public key string"`
}

type sshKeyDeleteInput struct {
	User    string `json:"user" jsonschema:"Username or ID of the user"`
	KeyID   string `json:"key_id" jsonschema:"SSH key ID to delete"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type duoCreateInput struct {
	Name string `json:"name" jsonschema:"Duo account name"`
}

type duoAppCreateInput struct {
	Account string `json:"account" jsonschema:"Duo account name or ID"`
	Name    string `json:"name" jsonschema:"Duo application name"`
	APIHost string `json:"api_host" jsonschema:"Duo API host"`
}

type duoAppGetInput struct {
	Account string `json:"account" jsonschema:"Duo account name or ID"`
	AppID   string `json:"app_id" jsonschema:"Duo application ID"`
}

type duoAppDeleteInput struct {
	Account string `json:"account" jsonschema:"Duo account name or ID"`
	AppID   string `json:"app_id" jsonschema:"Duo application ID"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type softwareStatusesInput struct {
	Identifier string `json:"identifier" jsonschema:"Software app name or ID"`
}

type softwareReclaimInput struct {
	Identifier string `json:"identifier" jsonschema:"Software app name or ID"`
	DeviceID   string `json:"device_id" jsonschema:"Device hostname or ID"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type graphTraverseInput struct {
	From string `json:"from" jsonschema:"Source resource as type:name-or-id (e.g. user:jdoe, user_group:Engineering). Types: user, device, user_group, device_group, application"`
	To   string `json:"to" jsonschema:"Target resource type (e.g. application, system, user_group, active_directory, ldap_server)"`
}

type graphBindInput struct {
	From    string `json:"from" jsonschema:"Source resource as type:name-or-id (e.g. user_group:Engineering)"`
	To      string `json:"to" jsonschema:"Target resource as type:name-or-id (e.g. application:Slack)"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type commandTriggerInput struct {
	TriggerName string `json:"trigger_name" jsonschema:"Name of the command trigger to fire"`
	Data        string `json:"data,omitempty" jsonschema:"Optional JSON payload to send with the trigger"`
}

type customEmailTypeInput struct {
	EmailType string `json:"email_type" jsonschema:"Custom email type (e.g. activate_user_custom, password_expiration)"`
}

type customEmailCreateInput struct {
	EmailType string `json:"email_type" jsonschema:"Custom email type (e.g. activate_user_custom, password_expiration)"`
	Subject   string `json:"subject" jsonschema:"Email subject line"`
	Title     string `json:"title,omitempty" jsonschema:"Email title"`
	Body      string `json:"body,omitempty" jsonschema:"Email body text"`
	Header    string `json:"header,omitempty" jsonschema:"Email header text"`
	Button    string `json:"button,omitempty" jsonschema:"Email button text"`
	Execute   bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type customEmailUpdateInput struct {
	EmailType string `json:"email_type" jsonschema:"Custom email type to update"`
	Subject   string `json:"subject,omitempty" jsonschema:"New email subject line"`
	Title     string `json:"title,omitempty" jsonschema:"New email title"`
	Body      string `json:"body,omitempty" jsonschema:"New email body text"`
	Header    string `json:"header,omitempty" jsonschema:"New email header text"`
	Button    string `json:"button,omitempty" jsonschema:"New email button text"`
	Execute   bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type customEmailDeleteInput struct {
	EmailType string `json:"email_type" jsonschema:"Custom email type to delete"`
	Execute   bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type sambaDomainGetInput struct {
	LDAPServer string `json:"ldap_server" jsonschema:"LDAP server name or ID"`
	DomainID   string `json:"domain_id" jsonschema:"Samba domain ID"`
}

type sambaDomainCreateInput struct {
	LDAPServer string `json:"ldap_server" jsonschema:"LDAP server name or ID"`
	Name       string `json:"name" jsonschema:"Samba domain workgroup name"`
	SID        string `json:"sid" jsonschema:"Samba domain security identifier"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type sambaDomainUpdateInput struct {
	LDAPServer string `json:"ldap_server" jsonschema:"LDAP server name or ID"`
	DomainID   string `json:"domain_id" jsonschema:"Samba domain ID"`
	Name       string `json:"name,omitempty" jsonschema:"New workgroup name"`
	SID        string `json:"sid,omitempty" jsonschema:"New security identifier"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type sambaDomainDeleteInput struct {
	LDAPServer string `json:"ldap_server" jsonschema:"LDAP server name or ID"`
	DomainID   string `json:"domain_id" jsonschema:"Samba domain ID"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type insightsDistinctInput struct {
	Service   string `json:"service" jsonschema:"Event service to query (sso/radius/ldap/user_portal/admin/mdm/directory/software/systems/password_manager/all)"`
	Field     string `json:"field" jsonschema:"Field to get distinct values for (e.g. event_type, initiated_by)"`
	Last      string `json:"last,omitempty" jsonschema:"Time range shortcut (24h/7d/30d/1m)"`
	Start     string `json:"start,omitempty" jsonschema:"Start time (RFC 3339 or YYYY-MM-DD)"`
	End       string `json:"end,omitempty" jsonschema:"End time (RFC 3339 or YYYY-MM-DD)"`
	EventType string `json:"event_type,omitempty" jsonschema:"Filter by event type"`
}

// registerTools adds MCP tools to the server.
func (s *Server) registerTools() {
	// ping: A simple health-check tool.
	s.addTool("jc_ping", "Check if the JC MCP server is running and authenticated",
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return textResult(fmt.Sprintf("jc MCP server v%s is running", version.Number)), nil, nil
		},
	)

	// --- Users tools ---
	s.registerUserTools()

	// --- Devices tools ---
	s.registerDeviceTools()

	// --- Groups tools ---
	s.registerGroupTools()

	// --- Insights tools ---
	s.registerInsightsTools()

	// --- Commands tools ---
	s.registerCommandTools()

	// --- Policies tools ---
	s.registerPolicyTools()

	// --- Auth Policies tools ---
	s.registerAuthPolicyTools()

	// --- IP Lists tools ---
	s.registerIPListTools()

	// --- Software tools ---
	s.registerSoftwareTools()

	// --- Assets tools ---
	s.registerAssetsTools()

	// --- LDAP tools ---
	s.registerLDAPTools()

	// --- Active Directory tools ---
	s.registerADTools()

	// --- Organization tools ---
	s.registerOrgTools()

	// --- Admin tools ---
	s.registerAdminTools()

	// --- Apps tools ---
	s.registerAppsTools()

	// --- Graph tools ---
	s.registerGraphTools()

	// --- System Insights tools ---
	s.registerSystemInsightsTools()

	// --- RADIUS tools ---
	s.registerRADIUSTools()

	// --- Policy Templates tools ---
	s.registerPolicyTemplateTools()

	// --- Apple MDM tools ---
	s.registerAppleMDMTools()

	// --- Policy Groups tools ---
	s.registerPolicyGroupTools()

	// --- User States tools ---
	s.registerUserStateTools()

	// --- G Suite tools ---
	s.registerGsuiteTools()

	// --- Office 365 tools ---
	s.registerOffice365Tools()

	// --- Duo tools ---
	s.registerDuoTools()

	// --- Custom Emails tools ---
	s.registerCustomEmailTools()

	// --- App Templates tools ---
	s.registerAppTemplateTools()

	// --- Recipe tools ---
	s.registerRecipeTools()

	// --- Plan and explain tools ---
	s.registerMetaTools()
}

func (s *Server) registerUserTools() {
	addTypedTool(s, "users_list", "List all JumpCloud users. Returns user objects with fields like username, email, firstname, lastname, activated, suspended.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/systemusers", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing users: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "users_get", "Get a single JumpCloud user by username or ID. Returns the full user object.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/systemusers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting user: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "users_create", "Create a new JumpCloud user. Requires username and email.",
		func(ctx context.Context, req *mcp.CallToolRequest, args userCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]string{
				"username": args.Username,
				"email":    args.Email,
			}
			if args.Firstname != "" {
				body["firstname"] = args.Firstname
			}
			if args.Lastname != "" {
				body["lastname"] = args.Lastname
			}
			if args.Department != "" {
				body["department"] = args.Department
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/systemusers", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating user: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "users_update", "Update a JumpCloud user's fields. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args userUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]string{}
			if args.Email != "" {
				body["email"] = args.Email
			}
			if args.Firstname != "" {
				body["firstname"] = args.Firstname
			}
			if args.Lastname != "" {
				body["lastname"] = args.Lastname
			}
			if args.Department != "" {
				body["department"] = args.Department
			}
			if args.JobTitle != "" {
				body["jobTitle"] = args.JobTitle
			}
			if len(body) == 0 {
				return errorResult("no fields to update — provide at least one field (email, firstname, lastname, department, jobTitle)"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("update", "user", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/systemusers/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating user: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "users_delete", "Delete a JumpCloud user. Set execute=true to delete; otherwise returns a plan. This is destructive and irreversible.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "user", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/systemusers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting user: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User %s deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_lock", "Lock a JumpCloud user account. Set execute=true to lock; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("lock", "user", args.Identifier, id, nil)
			}
			_, err = client.Update(ctx, "/systemusers/"+id, map[string]bool{"account_locked": true})
			if err != nil {
				return errorResult(fmt.Sprintf("locking user: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User %s locked successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_unlock", "Unlock a JumpCloud user account. Set execute=true to unlock; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("unlock", "user", args.Identifier, id, nil)
			}
			_, err = client.Update(ctx, "/systemusers/"+id, map[string]bool{"account_locked": false})
			if err != nil {
				return errorResult(fmt.Sprintf("unlocking user: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User %s unlocked successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_reset_mfa", "Reset MFA/TOTP enrollment for a JumpCloud user. The user will need to re-enroll. Set execute=true to reset; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("reset_mfa", "user", args.Identifier, id, nil)
			}
			_, err = client.Post(ctx, "/systemusers/"+id+"/resetmfa", map[string]bool{"exclusion": true})
			if err != nil {
				return errorResult(fmt.Sprintf("resetting MFA: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("MFA reset for user %s. User will need to re-enroll.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_reset_password", "Trigger a password reset email for a JumpCloud user. Set execute=true to reset; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("reset_password", "user", args.Identifier, id, nil)
			}
			_, err = client.Post(ctx, "/systemusers/"+id+"/expire", nil)
			if err != nil {
				return errorResult(fmt.Sprintf("resetting password: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Password reset email sent to user %s.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "users_search", "Search for JumpCloud users by keyword across username, email, firstname, lastname.",
		func(ctx context.Context, req *mcp.CallToolRequest, args searchInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			body := map[string]any{"searchTerm": map[string]string{"op": "or", "searchTerm": args.Term}}
			result, err := client.Search(ctx, "/search/systemusers", body, api.SearchOptions{Limit: args.Limit, Sort: args.Sort})
			if err != nil {
				return errorResult(fmt.Sprintf("searching users: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "users_ssh_keys_list", "List SSH keys for a JumpCloud user.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/systemusers/"+id+"/sshkeys", api.ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing SSH keys: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "users_ssh_keys_add", "Add an SSH key to a JumpCloud user.",
		func(ctx context.Context, req *mcp.CallToolRequest, args sshKeyAddInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{
				"name":       args.Name,
				"public_key": args.PublicKey,
			}
			data, err := client.Create(ctx, "/systemusers/"+id+"/sshkeys", body)
			if err != nil {
				return errorResult(fmt.Sprintf("adding SSH key: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "users_ssh_keys_delete", "Delete an SSH key from a JumpCloud user. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args sshKeyDeleteInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "SSH key", args.KeyID, args.KeyID, map[string]string{"user": args.User})
			}
			_, err = client.Delete(ctx, "/systemusers/"+id+"/sshkeys/"+args.KeyID)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting SSH key: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("SSH key %q deleted successfully", args.KeyID)), nil, nil
		},
	)
}

func (s *Server) registerDeviceTools() {
	addTypedTool(s, "devices_list", "List all JumpCloud devices (systems). Returns device objects with fields like displayName, hostname, os, osVersion, lastContact, agentVersion.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/systems", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing devices: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "devices_get", "Get a single JumpCloud device by hostname or ID. Returns the full device object.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.DeviceConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/systems/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting device: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "devices_lock", "Send MDM lock command to a device. Set execute=true to lock; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceMDMTool(ctx, args, "lock")
		},
	)

	addTypedTool(s, "devices_restart", "Send MDM restart command to a device. Set execute=true to restart; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceMDMTool(ctx, args, "restart")
		},
	)

	addTypedTool(s, "devices_erase", "Send MDM erase (wipe) command to a device. THIS IS EXTREMELY DESTRUCTIVE — it will wipe all data. Set execute=true to erase; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceMDMTool(ctx, args, "erase")
		},
	)

	addTypedTool(s, "devices_update", "Update settings on an existing JumpCloud device. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args deviceUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{}
			if args.DisplayName != "" {
				body["displayName"] = args.DisplayName
			}
			if args.AllowSshPasswordAuthentication != nil {
				body["allowSshPasswordAuthentication"] = *args.AllowSshPasswordAuthentication
			}
			if args.AllowSshRootLogin != nil {
				body["allowSshRootLogin"] = *args.AllowSshRootLogin
			}
			if args.AllowMultiFactorAuthentication != nil {
				body["allowMultiFactorAuthentication"] = *args.AllowMultiFactorAuthentication
			}
			if args.AllowPublicKeyAuthentication != nil {
				body["allowPublicKeyAuthentication"] = *args.AllowPublicKeyAuthentication
			}
			if len(body) == 0 {
				return errorResult("no fields to update — provide at least one field"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.DeviceConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("update", "device", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/systems/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating device: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "devices_delete", "Delete a JumpCloud device. Set execute=true to delete; otherwise returns a plan. This is destructive and irreversible.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.DeviceConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "device", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/systems/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting device: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Device %s deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "devices_search", "Search for JumpCloud devices by keyword across hostname, displayName, os, serialNumber.",
		func(ctx context.Context, req *mcp.CallToolRequest, args searchInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			body := map[string]any{"searchTerm": map[string]string{"op": "or", "searchTerm": args.Term}}
			result, err := client.Search(ctx, "/search/systems", body, api.SearchOptions{Limit: args.Limit, Sort: args.Sort})
			if err != nil {
				return errorResult(fmt.Sprintf("searching devices: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "devices_fde_key", "Retrieve the Full Disk Encryption recovery key for a device.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			v1Client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V1 client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, v1Client, args.Identifier, resolve.DeviceConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			v2Client, v2Err := newV2ClientFunc()
			if v2Err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", v2Err)), nil, nil
			}
			data, err := v2Client.Get(ctx, "/systems/"+id+"/fdekey")
			if err != nil {
				return errorResult(fmt.Sprintf("getting FDE key: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)
}

func (s *Server) runDeviceMDMTool(ctx context.Context, args destructiveInput, action string) (*mcp.CallToolResult, any, error) {
	if s.readOnly {
		return errorResult("server is in read-only mode"), nil, nil
	}
	client, err := newV1ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
	}
	id, err := resolveV1(ctx, client, args.Identifier, resolve.DeviceConfig)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if !args.Execute {
		return planResult(action, "device", args.Identifier, id, nil)
	}
	_, err = client.Post(ctx, "/systems/"+id+"/command/builtin/"+action, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("%s device: %v", action, err)), nil, nil
	}
	return textResult(fmt.Sprintf("Device %s %s command sent successfully.", args.Identifier, action)), nil, nil
}

func (s *Server) registerGroupTools() {
	addTypedTool(s, "groups_list", "List all JumpCloud user groups and device (system) groups. Returns group objects with id, name, type.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			userGroups, err := client.ListAll(ctx, "/usergroups", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing user groups: %v", err)), nil, nil
			}
			deviceGroups, err := client.ListAll(ctx, "/systemgroups", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing device groups: %v", err)), nil, nil
			}
			all := append(userGroups.Data, deviceGroups.Data...)
			return rawListResult(all, len(all))
		},
	)

	addTypedTool(s, "groups_add_member", "Add a user or device to a group. Set execute=true to add; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args membershipInput) (*mcp.CallToolResult, any, error) {
			return s.runMembershipTool(ctx, args, "add")
		},
	)

	addTypedTool(s, "groups_remove_member", "Remove a user or device from a group. Set execute=true to remove; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args membershipInput) (*mcp.CallToolResult, any, error) {
			return s.runMembershipTool(ctx, args, "remove")
		},
	)

	// --- User group CRUD ---

	addTypedTool(s, "groups_user_list", "List all JumpCloud user groups. Returns group objects with id, name, description.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/usergroups", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing user groups: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "groups_user_get", "Get a single JumpCloud user group by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.UserGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/usergroups/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting user group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "groups_user_create", "Create a new JumpCloud user group.",
		func(ctx context.Context, req *mcp.CallToolRequest, args groupCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/usergroups", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating user group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "groups_user_update", "Update a JumpCloud user group. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args groupUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.UserGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			if !args.Execute {
				return planResult("update", "user group", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/usergroups/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating user group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "groups_user_delete", "Delete a JumpCloud user group. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.UserGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "user group", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/usergroups/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting user group: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User group %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	// --- Device group CRUD ---

	addTypedTool(s, "groups_device_list", "List all JumpCloud device (system) groups. Returns group objects with id, name, description.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/systemgroups", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing device groups: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "groups_device_get", "Get a single JumpCloud device (system) group by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.DeviceGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/systemgroups/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting device group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "groups_device_create", "Create a new JumpCloud device (system) group.",
		func(ctx context.Context, req *mcp.CallToolRequest, args groupCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/systemgroups", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating device group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "groups_device_update", "Update a JumpCloud device (system) group. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args groupUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.DeviceGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			if !args.Execute {
				return planResult("update", "device group", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/systemgroups/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating device group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "groups_device_delete", "Delete a JumpCloud device (system) group. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.DeviceGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "device group", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/systemgroups/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting device group: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Device group %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) runMembershipTool(ctx context.Context, args membershipInput, op string) (*mcp.CallToolResult, any, error) {
	if s.readOnly {
		return errorResult("server is in read-only mode"), nil, nil
	}
	memberType := strings.ToLower(args.MemberType)
	if memberType != "user" && memberType != "device" {
		return errorResult("member_type must be 'user' or 'device'"), nil, nil
	}

	v2Client, err := newV2ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
	}

	// Resolve group.
	var groupID string
	var groupEndpoint, memberEndpoint string
	var v2Resolver = resolve.NewV2Resolver(v2Client)
	if memberType == "user" {
		groupID, err = v2Resolver.Resolve(ctx, args.Group, resolve.UserGroupConfig)
		if err != nil {
			return errorResult(fmt.Sprintf("resolving group: %v", err)), nil, nil
		}
		groupEndpoint = "/usergroups"
		memberEndpoint = "/usergroups/" + groupID + "/members"
	} else {
		groupID, err = v2Resolver.Resolve(ctx, args.Group, resolve.DeviceGroupConfig)
		if err != nil {
			return errorResult(fmt.Sprintf("resolving group: %v", err)), nil, nil
		}
		groupEndpoint = "/systemgroups"
		memberEndpoint = "/systemgroups/" + groupID + "/membership"
	}
	_ = groupEndpoint

	// Resolve member.
	v1Client, err := newV1ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating V1 client: %v", err)), nil, nil
	}
	var memberID string
	if memberType == "user" {
		memberID, err = resolveV1(ctx, v1Client, args.Member, resolve.UserConfig)
	} else {
		memberID, err = resolveV1(ctx, v1Client, args.Member, resolve.DeviceConfig)
	}
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	if !args.Execute {
		effects := map[string]string{
			"operation":   op,
			"group":       args.Group,
			"group_id":    groupID,
			"member":      args.Member,
			"member_id":   memberID,
			"member_type": memberType,
		}
		return planResult(op+"_member", "group_membership", args.Group, groupID, effects)
	}

	apiType := memberType
	if memberType == "device" {
		apiType = "system"
	}
	body := map[string]string{
		"op":   op,
		"type": apiType,
		"id":   memberID,
	}
	_, err = v2Client.Create(ctx, memberEndpoint, body)
	if err != nil {
		return errorResult(fmt.Sprintf("%s member: %v", op, err)), nil, nil
	}
	verb := "added to"
	if op == "remove" {
		verb = "removed from"
	}
	return textResult(fmt.Sprintf("Member %s %s group %s.", args.Member, verb, args.Group)), nil, nil
}

func (s *Server) registerInsightsTools() {
	addTypedTool(s, "insights_query", "Query JumpCloud Directory Insights events. Returns audit/activity events matching the criteria.",
		func(ctx context.Context, req *mcp.CallToolRequest, args insightsQueryInput) (*mcp.CallToolResult, any, error) {
			client, err := newInsightsClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating Insights client: %v", err)), nil, nil
			}
			if err := api.ValidateService(args.Service); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			startTime, endTime, err := resolveTimeRange(args.Last, args.Start, args.End)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			query := api.InsightsQuery{
				Service:   args.Service,
				StartTime: startTime,
				EndTime:   endTime,
			}
			if args.EventType != "" {
				query.SearchTermFilter = map[string]any{"event_type": args.EventType}
			}
			result, err := client.QueryEvents(ctx, query, api.InsightsQueryOptions{
				Limit: args.Limit,
			})
			if err != nil {
				return errorResult(fmt.Sprintf("querying events: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "insights_count", "Count JumpCloud Directory Insights events matching criteria. Returns a single count number.",
		func(ctx context.Context, req *mcp.CallToolRequest, args insightsCountInput) (*mcp.CallToolResult, any, error) {
			client, err := newInsightsClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating Insights client: %v", err)), nil, nil
			}
			if err := api.ValidateService(args.Service); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			startTime, endTime, err := resolveTimeRange(args.Last, args.Start, args.End)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			query := api.InsightsQuery{
				Service:   args.Service,
				StartTime: startTime,
				EndTime:   endTime,
			}
			if args.EventType != "" {
				query.SearchTermFilter = map[string]any{"event_type": args.EventType}
			}
			count, err := client.CountEvents(ctx, query)
			if err != nil {
				return errorResult(fmt.Sprintf("counting events: %v", err)), nil, nil
			}
			res, err := jsonResult(map[string]int{"count": count})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "insights_distinct", "Get distinct values for a field from JumpCloud Directory Insights events.",
		func(ctx context.Context, req *mcp.CallToolRequest, args insightsDistinctInput) (*mcp.CallToolResult, any, error) {
			client, err := newInsightsClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating Insights client: %v", err)), nil, nil
			}
			if err := api.ValidateService(args.Service); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			startTime, endTime, err := resolveTimeRange(args.Last, args.Start, args.End)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			query := api.InsightsQuery{
				Service:   args.Service,
				StartTime: startTime,
				EndTime:   endTime,
			}
			if args.EventType != "" {
				query.SearchTermFilter = map[string]any{"event_type": args.EventType}
			}
			data, err := client.DistinctEvents(ctx, query, args.Field)
			if err != nil {
				return errorResult(fmt.Sprintf("querying distinct values: %v", err)), nil, nil
			}
			return rawListResult(data, len(data))
		},
	)
}

func (s *Server) registerCommandTools() {
	addTypedTool(s, "commands_list", "List all JumpCloud commands. Returns command objects with name, commandType, command, schedule fields.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/commands", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing commands: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "commands_run", "Trigger a JumpCloud command on a device or device group. Set execute=true to run; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args commandRunInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			// Resolve command.
			cmdResolver := resolve.NewResolver(client)
			cmdID, err := cmdResolver.Resolve(ctx, args.Command, resolve.CommandConfig)
			if err != nil {
				return errorResult(fmt.Sprintf("resolving command: %v", err)), nil, nil
			}
			// Determine if target is a device or device group.
			targetID := args.Target
			isDeviceID := resolve.IsID(args.Target)
			if !isDeviceID {
				// Try resolving as device first.
				deviceResolver := resolve.NewResolver(client)
				id, deviceErr := deviceResolver.Resolve(ctx, args.Target, resolve.DeviceConfig)
				if deviceErr == nil {
					targetID = id
					isDeviceID = true
				}
			}

			if !args.Execute {
				effects := map[string]string{
					"command":    args.Command,
					"command_id": cmdID,
					"target":     args.Target,
					"target_id":  targetID,
				}
				return planResult("run", "command", args.Command, cmdID, effects)
			}

			body := map[string]any{
				"command": cmdID,
			}
			if isDeviceID {
				body["systems"] = []string{targetID}
			} else {
				body["systemGroups"] = []string{targetID}
			}
			_, err = client.Post(ctx, "/runcommand", body)
			if err != nil {
				return errorResult(fmt.Sprintf("running command: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Command %s triggered on %s.", args.Command, args.Target)), nil, nil
		},
	)

	addTypedTool(s, "commands_get", "Get a single JumpCloud command by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.CommandConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/commands/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting command: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "commands_create", "Create a new JumpCloud command.",
		func(ctx context.Context, req *mcp.CallToolRequest, args commandCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]string{
				"name":        args.Name,
				"command":     args.Command,
				"commandType": args.CommandType,
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/commands", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating command: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "commands_update", "Update an existing JumpCloud command. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args commandUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]string{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Command != "" {
				body["command"] = args.Command
			}
			if args.CommandType != "" {
				body["commandType"] = args.CommandType
			}
			if len(body) == 0 {
				return errorResult("no fields to update — provide at least one field (name, command, command_type)"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.CommandConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("update", "command", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/commands/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating command: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "commands_delete", "Permanently delete a JumpCloud command. Set execute=true to delete; otherwise returns a plan. IRREVERSIBLE.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.CommandConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "command", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/commands/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting command: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Command %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "commands_results", "List execution results for a JumpCloud command showing exit codes, stdout, stderr.",
		func(ctx context.Context, req *mcp.CallToolRequest, args resultsInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.CommandConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			opts := api.ListOptions{
				Limit:  args.Limit,
				Sort:   args.Sort,
				Filter: []string{"command:$eq:" + id},
			}
			result, err := client.ListAll(ctx, "/commandresults", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing command results: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "commands_trigger", "Fire a command trigger by name. Triggers run pre-configured commands without needing a command ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args commandTriggerInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			var body any
			if args.Data != "" {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(args.Data), &parsed); err != nil {
					return errorResult(fmt.Sprintf("invalid data JSON: %v", err)), nil, nil
				}
				body = parsed
			}
			result, err := client.Post(ctx, "/command/trigger/"+args.TriggerName, body)
			if err != nil {
				return errorResult(fmt.Sprintf("triggering command: %v", err)), nil, nil
			}
			return textResult(string(result)), nil, nil
		},
	)
}

func (s *Server) registerPolicyTools() {
	addTypedTool(s, "policies_list", "List all JumpCloud policies. Returns policy objects with id, name, template, os fields.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/policies", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing policies: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "policies_get", "Get a single JumpCloud policy by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.PolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/policies/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "policies_create", "Create a new JumpCloud policy from a template.",
		func(ctx context.Context, req *mcp.CallToolRequest, args policyCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name":     args.Name,
				"template": map[string]string{"id": args.TemplateID},
			}
			if args.Values != "" {
				var values any
				if err := json.Unmarshal([]byte(args.Values), &values); err != nil {
					return errorResult(fmt.Sprintf("invalid values JSON: %v", err)), nil, nil
				}
				body["values"] = values
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/policies", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "policies_update", "Update an existing JumpCloud policy. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args policyUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.PolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Values != "" {
				var values any
				if err := json.Unmarshal([]byte(args.Values), &values); err != nil {
					return errorResult(fmt.Sprintf("invalid values JSON: %v", err)), nil, nil
				}
				body["values"] = values
			}
			if !args.Execute {
				return planResult("update", "policy", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/policies/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "policies_delete", "Permanently delete a JumpCloud policy. Set execute=true to delete; otherwise returns a plan. IRREVERSIBLE.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.PolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "policy", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/policies/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting policy: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Policy %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "policies_results", "List policy application results per device for a JumpCloud policy.",
		func(ctx context.Context, req *mcp.CallToolRequest, args resultsInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.PolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			opts := api.V2ListOptions{
				Limit: args.Limit,
				Sort:  args.Sort,
			}
			result, err := client.ListAll(ctx, "/policies/"+id+"/policystatuses", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing policy results: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)
}

func (s *Server) registerAuthPolicyTools() {
	addTypedTool(s, "auth_policies_list", "List all JumpCloud authentication policies for conditional access. Returns policy objects with id, name, disabled, type, conditions, effect fields.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/authn/policies", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing auth policies: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "auth_policies_get", "Get a single JumpCloud authentication policy by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/authn/policies/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_create", "Create a new JumpCloud authentication policy. Conditions are specified as a raw JSON string.",
		func(ctx context.Context, req *mcp.CallToolRequest, args authPolicyCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name":     args.Name,
				"disabled": args.Disabled,
			}
			if args.Type != "" {
				body["type"] = args.Type
			}
			if args.Conditions != "" {
				var cond any
				if err := json.Unmarshal([]byte(args.Conditions), &cond); err != nil {
					return errorResult(fmt.Sprintf("invalid conditions JSON: %v", err)), nil, nil
				}
				body["conditions"] = cond
			}
			if args.MFA {
				body["mfa"] = map[string]any{"required": true}
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/authn/policies", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_update", "Update a JumpCloud authentication policy. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args authPolicyUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Conditions != "" {
				var cond any
				if err := json.Unmarshal([]byte(args.Conditions), &cond); err != nil {
					return errorResult(fmt.Sprintf("invalid conditions JSON: %v", err)), nil, nil
				}
				body["conditions"] = cond
			}
			if args.Disabled != nil {
				body["disabled"] = *args.Disabled
			}
			if !args.Execute {
				return planResult("update", "auth policy", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/authn/policies/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_delete", "Delete a JumpCloud authentication policy. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "auth policy", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/authn/policies/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting auth policy: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Auth policy %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_simulate", "Simulate whether a user would be allowed or denied by an authentication policy. Evaluates policy conditions locally against the provided context.",
		func(ctx context.Context, req *mcp.CallToolRequest, args simulateInput) (*mcp.CallToolResult, any, error) {
			v2Client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
			}
			v1Client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V1 client: %v", err)), nil, nil
			}

			// Resolve policy.
			r := resolve.NewV2Resolver(v2Client)
			policyID, err := r.Resolve(ctx, args.Policy, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			policyRaw, err := v2Client.Get(ctx, "/authn/policies/"+policyID)
			if err != nil {
				return errorResult(fmt.Sprintf("getting policy: %v", err)), nil, nil
			}

			var policy simulator.Policy
			if err := json.Unmarshal(policyRaw, &policy); err != nil {
				return errorResult(fmt.Sprintf("parsing policy: %v", err)), nil, nil
			}

			// Resolve user.
			userID, err := resolveV1(ctx, v1Client, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			// Fetch user's group memberships.
			memberResult, _ := v2Client.ListAll(ctx, "/users/"+userID+"/memberof", api.V2ListOptions{})
			var userGroups []string
			if memberResult != nil {
				for _, raw := range memberResult.Data {
					var assoc struct {
						ID   string `json:"id"`
						Type string `json:"type"`
					}
					if err := json.Unmarshal(raw, &assoc); err == nil && assoc.Type == "user_group" {
						userGroups = append(userGroups, assoc.ID)
					}
				}
			}

			simCtx := simulator.SimulationContext{
				UserID:     userID,
				UserGroups: userGroups,
				IP:         args.IP,
				Location:   args.Location,
			}

			// IP resolver.
			ipResolver := func(listID string) ([]string, error) {
				raw, err := v2Client.Get(ctx, "/iplists/"+listID)
				if err != nil {
					return nil, err
				}
				var ipList struct {
					IPs []string `json:"ips"`
				}
				if err := json.Unmarshal(raw, &ipList); err != nil {
					return nil, err
				}
				return ipList.IPs, nil
			}

			result := simulator.EvaluatePolicy(policy, simCtx, ipResolver)
			res, err := jsonResult(result)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "auth_policies_blast_radius", "Analyze which users are affected by an authentication policy. Returns the list of users in the policy's target groups.",
		func(ctx context.Context, req *mcp.CallToolRequest, args blastRadiusInput) (*mcp.CallToolResult, any, error) {
			v2Client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(v2Client)
			policyID, err := r.Resolve(ctx, args.Policy, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			policyRaw, err := v2Client.Get(ctx, "/authn/policies/"+policyID)
			if err != nil {
				return errorResult(fmt.Sprintf("getting policy: %v", err)), nil, nil
			}

			var policy struct {
				Name    string `json:"name"`
				Targets struct {
					UserGroups []string `json:"userGroups"`
					AllUsers   bool     `json:"allUsers"`
				} `json:"targets"`
			}
			if err := json.Unmarshal(policyRaw, &policy); err != nil {
				return errorResult(fmt.Sprintf("parsing policy: %v", err)), nil, nil
			}

			limit := args.Limit
			if limit == 0 {
				limit = 100
			}

			if policy.Targets.AllUsers {
				res, err := jsonResult(map[string]any{
					"all_users": true,
					"message":   fmt.Sprintf("Policy %q targets ALL users.", policy.Name),
				})
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			if len(policy.Targets.UserGroups) == 0 {
				res, err := jsonResult(map[string]any{
					"groups":  0,
					"members": 0,
					"message": fmt.Sprintf("Policy %q has no target user groups.", policy.Name),
				})
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			seen := make(map[string]bool)
			var members []json.RawMessage
			for _, groupID := range policy.Targets.UserGroups {
				if len(members) >= limit {
					break
				}
				result, err := v2Client.ListAll(ctx, "/usergroups/"+groupID+"/members", api.V2ListOptions{})
				if err != nil {
					continue
				}
				for _, raw := range result.Data {
					var m struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(raw, &m); err == nil && !seen[m.ID] {
						seen[m.ID] = true
						members = append(members, raw)
						if len(members) >= limit {
							break
						}
					}
				}
			}

			return rawListResult(members, len(members))
		},
	)

	addTypedTool(s, "auth_policies_enable", "Enable a disabled JumpCloud authentication policy. Set execute=true to enable; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("enable", "auth policy", args.Identifier, id, nil)
			}
			data, err := client.Update(ctx, "/authn/policies/"+id, map[string]any{"disabled": false})
			if err != nil {
				return errorResult(fmt.Sprintf("enabling auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "auth_policies_disable", "Disable a JumpCloud authentication policy. Set execute=true to disable; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AuthPolicyConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("disable", "auth policy", args.Identifier, id, nil)
			}
			data, err := client.Update(ctx, "/authn/policies/"+id, map[string]any{"disabled": true})
			if err != nil {
				return errorResult(fmt.Sprintf("disabling auth policy: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)
}

func (s *Server) registerIPListTools() {
	addTypedTool(s, "iplists_list", "List all JumpCloud IP lists. Returns objects with id, name, description.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/iplists", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing IP lists: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "iplists_get", "Get a single JumpCloud IP list by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IPListConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/iplists/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting IP list: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "iplists_create", "Create a new JumpCloud IP list. IPs can be single addresses, CIDR ranges, or IP ranges.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ipListCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
				"ips":  args.IPs,
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/iplists", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating IP list: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "iplists_update", "Update a JumpCloud IP list. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ipListUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IPListConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			if len(args.IPs) > 0 {
				body["ips"] = args.IPs
			}
			if !args.Execute {
				return planResult("update", "IP list", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/iplists/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating IP list: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "iplists_delete", "Delete a JumpCloud IP list. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IPListConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "IP list", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/iplists/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting IP list: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("IP list %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerSoftwareTools() {
	addTypedTool(s, "software_list", "List all JumpCloud software apps. Returns objects with id, displayName, settings, createdAt, updatedAt.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/softwareapps", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing software apps: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "software_get", "Get a single JumpCloud software app by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/softwareapps/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting software app: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "software_create", "Create a new JumpCloud software app. Settings is a raw JSON array of package configurations.",
		func(ctx context.Context, req *mcp.CallToolRequest, args softwareCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"displayName": args.Name,
			}
			if args.Settings != "" {
				var settings any
				if err := json.Unmarshal([]byte(args.Settings), &settings); err != nil {
					return errorResult(fmt.Sprintf("invalid settings JSON: %v", err)), nil, nil
				}
				body["settings"] = settings
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/softwareapps", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating software app: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "software_update", "Update a JumpCloud software app. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args softwareUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["displayName"] = args.Name
			}
			if args.Settings != "" {
				var settings any
				if err := json.Unmarshal([]byte(args.Settings), &settings); err != nil {
					return errorResult(fmt.Sprintf("invalid settings JSON: %v", err)), nil, nil
				}
				body["settings"] = settings
			}
			if !args.Execute {
				return planResult("update", "software app", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/softwareapps/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating software app: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "software_delete", "Delete a JumpCloud software app. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "software app", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/softwareapps/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting software app: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Software app %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "software_statuses", "List deployment statuses for a JumpCloud software app. Shows per-device installation status.",
		func(ctx context.Context, req *mcp.CallToolRequest, args softwareStatusesInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/softwareapps/"+id+"/statuses", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing software statuses: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "software_associations", "List device associations for a JumpCloud software app.",
		func(ctx context.Context, req *mcp.CallToolRequest, args softwareStatusesInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/softwareapps/"+id+"/associations?targets=system", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing software associations: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "software_reclaim_license", "Reclaim a software license from a device. Set execute=true to reclaim; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args softwareReclaimInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			v2, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(v2)
			appID, err := r.Resolve(ctx, args.Identifier, resolve.SoftwareAppConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("reclaim-license", "software app", args.Identifier, appID, map[string]any{"device": args.DeviceID})
			}
			body := map[string]any{"device_id": args.DeviceID}
			_, err = v2.Create(ctx, "/softwareapps/"+appID+"/reclaim-licenses", body)
			if err != nil {
				return errorResult(fmt.Sprintf("reclaiming license: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("License reclaimed from device %q for software app %q.", args.DeviceID, args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerAssetsTools() {
	addTypedTool(s, "assets_list", "List all JumpCloud assets. Returns objects with id, name, serialNumber, assetTag, status, type, systemId.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/assets", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing assets: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "assets_get", "Get a single JumpCloud asset by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AssetConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/assets/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting asset: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "assets_create", "Create a new JumpCloud asset.",
		func(ctx context.Context, req *mcp.CallToolRequest, args assetCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
			}
			if args.SerialNumber != "" {
				body["serialNumber"] = args.SerialNumber
			}
			if args.AssetTag != "" {
				body["assetTag"] = args.AssetTag
			}
			if args.Status != "" {
				body["status"] = args.Status
			}
			if args.Type != "" {
				body["type"] = args.Type
			}
			if args.SystemID != "" {
				body["systemId"] = args.SystemID
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/assets", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating asset: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "assets_update", "Update a JumpCloud asset. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args assetUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AssetConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.SerialNumber != "" {
				body["serialNumber"] = args.SerialNumber
			}
			if args.AssetTag != "" {
				body["assetTag"] = args.AssetTag
			}
			if args.Status != "" {
				body["status"] = args.Status
			}
			if args.Type != "" {
				body["type"] = args.Type
			}
			if args.SystemID != "" {
				body["systemId"] = args.SystemID
			}
			if !args.Execute {
				return planResult("update", "asset", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/assets/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating asset: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "assets_delete", "Delete a JumpCloud asset. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AssetConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "asset", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/assets/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting asset: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Asset %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerLDAPTools() {
	addTypedTool(s, "ldap_list", "List all JumpCloud LDAP servers. Returns objects with id, name, userLockoutAction, userPasswordExpirationAction.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/ldapservers", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing LDAP servers: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "ldap_get", "Get a single JumpCloud LDAP server by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/ldapservers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting LDAP server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_create", "Create a new JumpCloud LDAP server.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ldapCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
			}
			if args.UserLockoutAction != "" {
				body["userLockoutAction"] = args.UserLockoutAction
			}
			if args.UserPasswordExpirationAction != "" {
				body["userPasswordExpirationAction"] = args.UserPasswordExpirationAction
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/ldapservers", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating LDAP server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_update", "Update a JumpCloud LDAP server. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args ldapUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.UserLockoutAction != "" {
				body["userLockoutAction"] = args.UserLockoutAction
			}
			if args.UserPasswordExpirationAction != "" {
				body["userPasswordExpirationAction"] = args.UserPasswordExpirationAction
			}
			if !args.Execute {
				return planResult("update", "LDAP server", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/ldapservers/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating LDAP server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_delete", "Delete a JumpCloud LDAP server. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "LDAP server", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/ldapservers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting LDAP server: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("LDAP server %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	// --- Samba Domains (sub-resource of LDAP servers) ---

	addTypedTool(s, "ldap_samba_domains_list", "List samba domains for a JumpCloud LDAP server.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			ldapID, err := r.Resolve(ctx, args.Identifier, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/ldapservers/"+ldapID+"/sambadomains", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing samba domains: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "ldap_samba_domain_get", "Get a specific samba domain for a JumpCloud LDAP server.",
		func(ctx context.Context, req *mcp.CallToolRequest, args sambaDomainGetInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			ldapID, err := r.Resolve(ctx, args.LDAPServer, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/ldapservers/"+ldapID+"/sambadomains/"+args.DomainID)
			if err != nil {
				return errorResult(fmt.Sprintf("getting samba domain: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_samba_domain_create", "Create a samba domain for a JumpCloud LDAP server. Set execute=true to create; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args sambaDomainCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			ldapID, err := r.Resolve(ctx, args.LDAPServer, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
				"sid":  args.SID,
			}
			if !args.Execute {
				return planResult("create", "samba domain", args.Name, "", body)
			}
			data, err := client.Create(ctx, "/ldapservers/"+ldapID+"/sambadomains", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating samba domain: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_samba_domain_update", "Update a samba domain for a JumpCloud LDAP server. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args sambaDomainUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			ldapID, err := r.Resolve(ctx, args.LDAPServer, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.SID != "" {
				body["sid"] = args.SID
			}
			if !args.Execute {
				return planResult("update", "samba domain", args.DomainID, args.DomainID, body)
			}
			data, err := client.Update(ctx, "/ldapservers/"+ldapID+"/sambadomains/"+args.DomainID, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating samba domain: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ldap_samba_domain_delete", "Delete a samba domain from a JumpCloud LDAP server. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args sambaDomainDeleteInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			ldapID, err := r.Resolve(ctx, args.LDAPServer, resolve.LDAPServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "samba domain", args.DomainID, args.DomainID, nil)
			}
			_, err = client.Delete(ctx, "/ldapservers/"+ldapID+"/sambadomains/"+args.DomainID)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting samba domain: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Samba domain %q deleted successfully.", args.DomainID)), nil, nil
		},
	)
}

func (s *Server) registerADTools() {
	addTypedTool(s, "ad_list", "List all JumpCloud Active Directory integrations. Returns objects with id, domain, useCase, groupsEnabled, delegationState.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/activedirectories", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing Active Directory integrations: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "ad_get", "Get a single JumpCloud Active Directory integration by domain or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/activedirectories/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting Active Directory: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_create", "Create a new JumpCloud Active Directory integration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"domain": args.Domain,
			}
			if args.UseCase != "" {
				body["useCase"] = args.UseCase
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/activedirectories", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating Active Directory: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_update", "Update a JumpCloud Active Directory integration. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.UseCase != "" {
				body["useCase"] = args.UseCase
			}
			if args.GroupsEnabled != nil {
				body["groupsEnabled"] = *args.GroupsEnabled
			}
			if !args.Execute {
				return planResult("update", "Active Directory", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/activedirectories/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating Active Directory: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_delete", "Delete a JumpCloud Active Directory integration. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "Active Directory", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/activedirectories/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting Active Directory: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Active Directory %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerOrgTools() {
	addTypedTool(s, "org_list", "List JumpCloud organizations. Returns objects with _id, displayName, created.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			result, err := client.ListAll(ctx, "/organizations", api.ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing organizations: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "org_get", "Get a JumpCloud organization by ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/organizations/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("getting organization: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "org_settings", "View the full settings for a JumpCloud organization.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/organizations/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("getting organization settings: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "org_update", "Update a JumpCloud organization. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args orgUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["displayName"] = args.Name
			}
			if args.SettingsJSON != "" {
				var settings map[string]any
				if err := json.Unmarshal([]byte(args.SettingsJSON), &settings); err != nil {
					return errorResult(fmt.Sprintf("invalid settings_json: %v", err)), nil, nil
				}
				body["settings"] = settings
			}
			if !args.Execute {
				return planResult("update", "organization", args.ID, args.ID, body)
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Update(ctx, "/organizations/"+args.ID, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating organization: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)
}

func (s *Server) registerAdminTools() {
	addTypedTool(s, "admins_list", "List all JumpCloud administrators. Returns objects with id, email, role, enableMultiFactor.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/users", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing admins: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "admins_get", "Get a single JumpCloud administrator by email or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.AdminConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/users/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting admin: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "admins_create", "Create a new JumpCloud administrator.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adminCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"email": args.Email,
			}
			if args.Role != "" {
				body["roleName"] = args.Role
			}
			if args.EnableMFA {
				body["enableMultiFactor"] = true
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/users", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating admin: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "admins_update", "Update a JumpCloud administrator. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adminUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.AdminConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Role != "" {
				body["roleName"] = args.Role
			}
			if args.EnableMFA != nil {
				body["enableMultiFactor"] = *args.EnableMFA
			}
			if !args.Execute {
				return planResult("update", "admin", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/users/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating admin: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "admins_delete", "Delete a JumpCloud administrator. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.AdminConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "admin", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/users/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting admin: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Admin %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerAppsTools() {
	addTypedTool(s, "apps_list", "List all JumpCloud SSO applications. Returns objects with _id, name, displayLabel, ssoType, status.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/applications", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing applications: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "apps_get", "Get a single JumpCloud SSO application by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.ApplicationConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/applications/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting application: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "apps_create", "Create a new JumpCloud SSO application.",
		func(ctx context.Context, req *mcp.CallToolRequest, args appCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
			}
			if args.SsoType != "" {
				body["ssoType"] = args.SsoType
			}
			if args.Config != "" {
				var cfg any
				if err := json.Unmarshal([]byte(args.Config), &cfg); err != nil {
					return errorResult(fmt.Sprintf("invalid config JSON: %v", err)), nil, nil
				}
				body["config"] = cfg
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/applications", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating application: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "apps_update", "Update an existing JumpCloud SSO application. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args appUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.ApplicationConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Config != "" {
				var cfg any
				if err := json.Unmarshal([]byte(args.Config), &cfg); err != nil {
					return errorResult(fmt.Sprintf("invalid config JSON: %v", err)), nil, nil
				}
				body["config"] = cfg
			}
			if !args.Execute {
				return planResult("update", "application", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/applications/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating application: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "apps_delete", "Permanently delete a JumpCloud SSO application. Set execute=true to delete; otherwise returns a plan. IRREVERSIBLE.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.ApplicationConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "application", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/applications/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting application: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Application %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}

// graphTargetAliases maps user-friendly target type aliases to V2 API parameter values.
var graphTargetAliases = map[string]string{
	"device":       "system",
	"device_group": "system_group",
}

func (s *Server) registerGraphTools() {
	addTypedTool(s, "graph_traverse", "Traverse JumpCloud graph associations between resources. Source types: user, device, user_group, device_group, application.",
		func(ctx context.Context, req *mcp.CallToolRequest, args graphTraverseInput) (*mcp.CallToolResult, any, error) {
			sourceType, identifier, err := parseGraphFrom(args.From)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			// Map target aliases.
			apiTarget := args.To
			if mapped, ok := graphTargetAliases[apiTarget]; ok {
				apiTarget = mapped
			}

			prefix, sourceID, err := resolveGraphSource(ctx, sourceType, identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("resolving source: %v", err)), nil, nil
			}

			endpoint := prefix + "/" + sourceID + "/associations?targets=" + apiTarget
			v2Client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
			}
			result, err := v2Client.ListAll(ctx, endpoint, api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("traversing graph: %v", err)), nil, nil
			}
			data := flattenAssociations(result.Data)
			return rawListResult(data, len(data))
		},
	)

	addTypedTool(s, "graph_bind", "Create an association between two JumpCloud resources. Set execute=true to bind; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args graphBindInput) (*mcp.CallToolResult, any, error) {
			return s.runGraphManageTool(ctx, args.From, args.To, "add", args.Execute)
		},
	)

	addTypedTool(s, "graph_unbind", "Remove an association between two JumpCloud resources. Set execute=true to unbind; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args graphBindInput) (*mcp.CallToolResult, any, error) {
			return s.runGraphManageTool(ctx, args.From, args.To, "remove", args.Execute)
		},
	)
}

func (s *Server) runGraphManageTool(ctx context.Context, from, to, op string, execute bool) (*mcp.CallToolResult, any, error) {
	if s.readOnly {
		return errorResult("server is in read-only mode"), nil, nil
	}

	sourceType, sourceIdent, err := parseGraphFrom(from)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	targetType, targetIdent, err := parseGraphTarget(to)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	// Map target aliases.
	apiTarget := targetType
	if mapped, ok := graphTargetAliases[targetType]; ok {
		apiTarget = mapped
	}

	prefix, sourceID, err := resolveGraphSource(ctx, sourceType, sourceIdent)
	if err != nil {
		return errorResult(fmt.Sprintf("resolving source: %v", err)), nil, nil
	}

	targetID, err := resolveGraphTarget(ctx, targetType, targetIdent)
	if err != nil {
		return errorResult(fmt.Sprintf("resolving target: %v", err)), nil, nil
	}

	if !execute {
		action := "bind"
		if op == "remove" {
			action = "unbind"
		}
		effects := map[string]string{
			"operation": op,
			"source":    sourceType + "/" + sourceID,
			"target":    apiTarget + "/" + targetID,
		}
		return planResult(action, "graph association", from+" -> "+to, sourceID, effects)
	}

	body := map[string]string{
		"op":   op,
		"type": apiTarget,
		"id":   targetID,
	}
	v2Client, err := newV2ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
	}
	_, err = v2Client.Create(ctx, prefix+"/"+sourceID+"/associations", body)
	if err != nil {
		return errorResult(fmt.Sprintf("%s graph association: %v", op, err)), nil, nil
	}
	verb := "bound"
	if op == "remove" {
		verb = "unbound"
	}
	return textResult(fmt.Sprintf("Successfully %s %s -> %s.", verb, from, to)), nil, nil
}

// parseGraphFrom splits a "type:identifier" string for graph source.
func parseGraphFrom(from string) (string, string, error) {
	parts := strings.SplitN(from, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid from format %q — expected type:name-or-id (e.g. user:jdoe)", from)
	}
	return parts[0], parts[1], nil
}

// parseGraphTarget splits a "type:identifier" string for graph target in bind/unbind.
func parseGraphTarget(to string) (string, string, error) {
	parts := strings.SplitN(to, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid to format %q — expected type:name-or-id (e.g. application:Slack)", to)
	}
	return parts[0], parts[1], nil
}

// resolveGraphSource resolves a graph source type and identifier to an API endpoint prefix and ID.
func resolveGraphSource(ctx context.Context, sourceType, identifier string) (string, string, error) {
	switch sourceType {
	case "user":
		client, err := newV1ClientFunc()
		if err != nil {
			return "", "", err
		}
		id, err := resolveV1(ctx, client, identifier, resolve.UserConfig)
		return "/users", id, err
	case "device":
		client, err := newV1ClientFunc()
		if err != nil {
			return "", "", err
		}
		id, err := resolveV1(ctx, client, identifier, resolve.DeviceConfig)
		return "/systems", id, err
	case "user_group":
		client, err := newV2ClientFunc()
		if err != nil {
			return "", "", err
		}
		r := resolve.NewV2Resolver(client)
		id, err := r.Resolve(ctx, identifier, resolve.UserGroupConfig)
		return "/usergroups", id, err
	case "device_group":
		client, err := newV2ClientFunc()
		if err != nil {
			return "", "", err
		}
		r := resolve.NewV2Resolver(client)
		id, err := r.Resolve(ctx, identifier, resolve.DeviceGroupConfig)
		return "/systemgroups", id, err
	case "application":
		client, err := newV1ClientFunc()
		if err != nil {
			return "", "", err
		}
		id, err := resolveV1(ctx, client, identifier, resolve.ApplicationConfig)
		return "/applications", id, err
	}
	return "", "", fmt.Errorf("unsupported source type %q — valid types: user, device, user_group, device_group, application", sourceType)
}

// resolveGraphTarget resolves a graph target type and identifier to an ID.
// For known source types with name resolution, uses the appropriate resolver.
// For other types, requires a 24-char hex ID.
func resolveGraphTarget(ctx context.Context, targetType, identifier string) (string, error) {
	// Try resolving as a known source type first.
	_, id, err := resolveGraphSource(ctx, targetType, identifier)
	if err == nil {
		return id, nil
	}
	// For types without name resolution, the identifier must be a raw ID.
	if resolve.IsID(identifier) {
		return identifier, nil
	}
	return "", fmt.Errorf("target type %q does not support name resolution — provide a 24-character hex ID", targetType)
}

// flattenAssociations transforms graph association objects from nested form
// {"to":{"type":"...","id":"..."}} to flat form {"type":"...","id":"..."}.
func flattenAssociations(data []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(data))
	for _, raw := range data {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			result = append(result, raw)
			continue
		}
		toRaw, ok := m["to"]
		if !ok {
			result = append(result, raw)
			continue
		}
		var toObj map[string]json.RawMessage
		if err := json.Unmarshal(toRaw, &toObj); err != nil {
			result = append(result, raw)
			continue
		}
		flat := make(map[string]json.RawMessage)
		for k, v := range m {
			if k != "to" {
				flat[k] = v
			}
		}
		for k, v := range toObj {
			flat[k] = v
		}
		out, err := json.Marshal(flat)
		if err != nil {
			result = append(result, raw)
			continue
		}
		result = append(result, out)
	}
	return result
}

func (s *Server) registerRecipeTools() {
	addTypedTool(s, "recipe_run", "Run a named jc recipe with parameters. Recipes are multi-step automated workflows.",
		func(ctx context.Context, req *mcp.CallToolRequest, args recipeRunInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			recipes, err := recipe.LoadAll()
			if err != nil {
				return errorResult(fmt.Sprintf("loading recipes: %v", err)), nil, nil
			}
			r := recipe.FindByName(recipes, args.Name)
			if r == nil {
				names := make([]string, 0, len(recipes))
				for _, rec := range recipes {
					names = append(names, rec.Name)
				}
				return errorResult(fmt.Sprintf("recipe %q not found. Available recipes: %s", args.Name, strings.Join(names, ", "))), nil, nil
			}
			params := args.Params
			if params == nil {
				params = map[string]string{}
			}
			// Preview the recipe steps.
			plans, err := r.Plan(params)
			if err != nil {
				return errorResult(fmt.Sprintf("planning recipe: %v", err)), nil, nil
			}
			res, err := jsonResult(plans)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}

func (s *Server) registerMetaTools() {
	addTypedTool(s, "plan", "Preview what a jc command would do without executing it. Returns a structured plan showing the action, target, and effects.",
		func(ctx context.Context, req *mcp.CallToolRequest, args planInput) (*mcp.CallToolResult, any, error) {
			parts := strings.Fields(args.Command)
			if len(parts) == 0 {
				return errorResult("empty command string"), nil, nil
			}
			result := map[string]any{
				"command":     args.Command,
				"description": describeCommand(parts),
				"note":        "Use the specific tool with execute=true to perform this action.",
			}
			res, err := jsonResult(result)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "explain", "Explain what a jc command would do in plain English. Does not execute anything.",
		func(ctx context.Context, req *mcp.CallToolRequest, args explainInput) (*mcp.CallToolResult, any, error) {
			parts := strings.Fields(args.Command)
			if len(parts) == 0 {
				return errorResult("empty command string"), nil, nil
			}
			explanation := describeCommand(parts)
			return textResult(explanation), nil, nil
		},
	)
}

// --- Helper functions ---

func resolveV1(ctx context.Context, client *api.V1Client, identifier string, cfg resolve.ResourceConfig) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, cfg)
}

func buildV1ListOptions(args listInput) (api.ListOptions, error) {
	opts := api.ListOptions{
		Limit: args.Limit,
		Sort:  args.Sort,
	}
	if len(args.Filter) > 0 {
		exprs, err := filter.ParseAll(args.Filter)
		if err != nil {
			return opts, fmt.Errorf("invalid filter: %w", err)
		}
		opts.Filter = filter.ToV1Queries(exprs)
	}
	return opts, nil
}

func buildV2ListOptions(args listInput) (api.V2ListOptions, error) {
	opts := api.V2ListOptions{
		Limit: args.Limit,
		Sort:  args.Sort,
	}
	if len(args.Filter) > 0 {
		exprs, err := filter.ParseAll(args.Filter)
		if err != nil {
			return opts, fmt.Errorf("invalid filter: %w", err)
		}
		opts.Filter = filter.ToV2Queries(exprs)
	}
	return opts, nil
}

func resolveTimeRange(last, start, end string) (string, string, error) {
	if last == "" && start == "" {
		return "", "", fmt.Errorf("either last or start is required for time range")
	}
	if last != "" && start != "" {
		return "", "", fmt.Errorf("last and start are mutually exclusive")
	}
	if last != "" {
		t, err := api.ParseTimeRange(last)
		if err != nil {
			return "", "", err
		}
		return t.UTC().Format("2006-01-02T15:04:05Z"), api.InsightsNowFunc().UTC().Format("2006-01-02T15:04:05Z"), nil
	}
	startTime, err := api.ParseTimeRange(start)
	if err != nil {
		return "", "", fmt.Errorf("invalid start time: %w", err)
	}
	endStr := api.InsightsNowFunc().UTC().Format("2006-01-02T15:04:05Z")
	if end != "" {
		endTime, err := api.ParseTimeRange(end)
		if err != nil {
			return "", "", fmt.Errorf("invalid end time: %w", err)
		}
		endStr = endTime.UTC().Format("2006-01-02T15:04:05Z")
	}
	return startTime.UTC().Format("2006-01-02T15:04:05Z"), endStr, nil
}

// rawListResult creates a JSON result from raw API response items.
func rawListResult(data []json.RawMessage, total int) (*mcp.CallToolResult, any, error) {
	result := map[string]any{
		"data":  data,
		"total": total,
	}
	if data == nil {
		result["data"] = []json.RawMessage{}
	}
	res, err := jsonResult(result)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

// planResult creates a plan preview result for destructive operations.
func planResult(action, resource, identifier, resolvedID string, effects any) (*mcp.CallToolResult, any, error) {
	plan := map[string]any{
		"plan":        true,
		"action":      action,
		"resource":    resource,
		"target":      identifier,
		"resolved_id": resolvedID,
		"message":     fmt.Sprintf("This would %s %s %q (ID: %s). Pass execute=true to proceed.", action, resource, identifier, resolvedID),
	}
	if effects != nil {
		plan["effects"] = effects
	}
	res, err := jsonResult(plan)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

// describeCommand generates a plain-English description of a jc command.
func describeCommand(parts []string) string {
	if len(parts) == 0 {
		return "Empty command."
	}

	descriptions := map[string]map[string]string{
		"users": {
			"list":           "List all JumpCloud system users with optional filtering and sorting.",
			"get":            "Retrieve a single JumpCloud user by username or ID.",
			"create":         "Create a new JumpCloud user account.",
			"update":         "Update fields on an existing JumpCloud user.",
			"delete":         "Permanently delete a JumpCloud user. This removes the user from all groups and unbinds all devices. IRREVERSIBLE.",
			"search":         "Search for users by keyword across username, email, firstname, and lastname.",
			"lock":           "Lock a user account, preventing login.",
			"unlock":         "Unlock a previously locked user account.",
			"reset-mfa":      "Reset MFA/TOTP enrollment. The user will need to re-enroll their authenticator.",
			"reset-password": "Trigger a password reset email to the user.",
		},
		"devices": {
			"list":    "List all JumpCloud-managed devices (systems) with optional filtering.",
			"get":     "Retrieve a single device by hostname or ID.",
			"update":  "Update settings on an existing JumpCloud device.",
			"delete":  "Remove a device record from JumpCloud.",
			"search":  "Search for devices by keyword across hostname, displayName, os, serialNumber.",
			"lock":    "Send an MDM lock command to the device.",
			"restart": "Send an MDM restart command to the device.",
			"erase":   "Send an MDM erase (wipe) command to the device. EXTREMELY DESTRUCTIVE — wipes all data.",
		},
		"groups": {
			"list":          "List all user groups and device groups.",
			"add-member":    "Add a user or device to a group.",
			"remove-member": "Remove a user or device from a group.",
			"user":          "Manage JumpCloud user groups (list, get, create, update, delete).",
			"device":        "Manage JumpCloud device groups (list, get, create, update, delete).",
		},
		"insights": {
			"query":    "Query Directory Insights events (audit log) for a given service and time range.",
			"count":    "Count events matching criteria without returning full records.",
			"distinct": "Get distinct values for a field from Directory Insights events.",
		},
		"commands": {
			"list":    "List all JumpCloud commands.",
			"get":     "Retrieve a single JumpCloud command by name or ID.",
			"create":  "Create a new JumpCloud command.",
			"update":  "Update an existing JumpCloud command.",
			"delete":  "Permanently delete a JumpCloud command. IRREVERSIBLE.",
			"results": "List execution results for a command showing exit codes, stdout, stderr.",
			"run":     "Trigger a command to run on specified devices or device groups.",
		},
		"policies": {
			"list":    "List all JumpCloud policies with name, type, and OS target.",
			"get":     "Retrieve a single JumpCloud policy by name or ID.",
			"create":  "Create a new JumpCloud policy from a template.",
			"update":  "Update an existing JumpCloud policy.",
			"delete":  "Permanently delete a JumpCloud policy. IRREVERSIBLE.",
			"results": "List policy application results per device.",
		},
		"auth-policies": {
			"list":         "List all JumpCloud authentication policies for conditional access.",
			"get":          "Get an authentication policy by name or ID.",
			"create":       "Create a new authentication policy with conditions and targets.",
			"update":       "Update an existing authentication policy.",
			"delete":       "Delete an authentication policy. IRREVERSIBLE.",
			"enable":       "Enable a disabled authentication policy.",
			"disable":      "Disable an authentication policy.",
			"simulate":     "Simulate whether a user would be allowed/denied by a policy.",
			"blast-radius": "Show which users are affected by a policy.",
		},
		"apps": {
			"list":   "List all JumpCloud SSO applications.",
			"get":    "Retrieve a single SSO application by name or ID.",
			"create": "Create a new SSO application.",
			"update": "Update an existing SSO application.",
			"delete": "Permanently delete an SSO application. IRREVERSIBLE.",
		},
		"graph": {
			"traverse": "Traverse JumpCloud graph associations between resources.",
			"bind":     "Create an association between two JumpCloud resources.",
			"unbind":   "Remove an association between two JumpCloud resources.",
		},
		"iplists": {
			"list":   "List all JumpCloud IP lists used by authentication policies.",
			"get":    "Get an IP list by name or ID.",
			"create": "Create a new IP list with IP entries.",
			"update": "Update an existing IP list.",
			"delete": "Delete an IP list. IRREVERSIBLE.",
		},
		"software": {
			"list":   "List all JumpCloud software apps.",
			"get":    "Get a software app by name or ID.",
			"create": "Create a new software app with package settings.",
			"update": "Update an existing software app.",
			"delete": "Delete a software app. IRREVERSIBLE.",
		},
		"ldap": {
			"list":   "List all JumpCloud LDAP servers.",
			"get":    "Get an LDAP server by name or ID.",
			"create": "Create a new LDAP server.",
			"update": "Update an existing LDAP server.",
			"delete": "Delete an LDAP server. IRREVERSIBLE.",
		},
		"ad": {
			"list":   "List all JumpCloud Active Directory integrations.",
			"get":    "Get an Active Directory integration by domain or ID.",
			"create": "Create a new Active Directory integration.",
			"update": "Update an existing Active Directory integration.",
			"delete": "Delete an Active Directory integration. IRREVERSIBLE.",
		},
		"org": {
			"list": "List JumpCloud organizations.",
			"get":  "Get organization details by ID.",
		},
		"admins": {
			"list":   "List all JumpCloud administrators.",
			"get":    "Get an administrator by email or ID.",
			"create": "Create a new administrator account.",
			"update": "Update an administrator's role or settings.",
			"delete": "Delete an administrator account. IRREVERSIBLE.",
		},
		"recipe": {
			"run": "Execute a multi-step automated recipe with parameters.",
		},
	}

	resource := parts[0]
	if resourceDescs, ok := descriptions[resource]; ok {
		if len(parts) > 1 {
			verb := parts[1]
			if desc, ok := resourceDescs[verb]; ok {
				return desc
			}
		}
		return fmt.Sprintf("Manage JumpCloud %s.", resource)
	}
	return fmt.Sprintf("Run jc command: %s", strings.Join(parts, " "))
}

// --- Tool infrastructure (addTool, addTypedTool, result helpers) ---

// addTool wraps mcp.AddTool with rate limiting and audit logging.
// Tools that are filtered out by the allow/block list are not registered.
func (s *Server) addTool(name, description string, handler func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error)) {
	if !s.toolFilter.isAllowed(name) {
		return
	}

	tool := &mcp.Tool{
		Name:        name,
		Description: description,
	}

	wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		// Rate limit check.
		if err := s.limiter.allow(); err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
			return errorResult(err.Error()), nil, nil
		}

		// Execute the tool.
		result, out, err := handler(ctx, req, args)

		// Audit log.
		if err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
		} else if result != nil && result.IsError {
			errMsg := "tool error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					errMsg = tc.Text
				}
			}
			s.auditLog.log(name, req.Params.Arguments, false, errMsg)
		} else {
			s.auditLog.log(name, req.Params.Arguments, true, "")
		}

		return result, out, err
	}

	mcp.AddTool(s.mcpServer, tool, wrappedHandler)
	s.toolNames = append(s.toolNames, name)
}

func (s *Server) registerSystemInsightsTools() {
	addTypedTool(s, "system_insights_list_table", "Query a system insights table (e.g. os_version, disk_encryption, apps). Returns osquery data from enrolled devices.",
		func(ctx context.Context, req *mcp.CallToolRequest, args systemInsightsListInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts := api.V2ListOptions{Limit: args.Limit, Sort: args.Sort}
			if len(args.Filter) > 0 {
				exprs, parseErr := filter.ParseAll(args.Filter)
				if parseErr != nil {
					return errorResult(fmt.Sprintf("invalid filter: %v", parseErr)), nil, nil
				}
				opts.Filter = filter.ToV2Queries(exprs)
			}
			if args.SystemID != "" {
				v1Client, v1Err := newV1ClientFunc()
				if v1Err != nil {
					return errorResult(fmt.Sprintf("creating V1 client: %v", v1Err)), nil, nil
				}
				sysID, resolveErr := resolveV1(ctx, v1Client, args.SystemID, resolve.DeviceConfig)
				if resolveErr != nil {
					return errorResult(resolveErr.Error()), nil, nil
				}
				opts.Filter = append(opts.Filter, "system_id:eq:"+sysID)
			}
			result, err := client.ListAll(ctx, "/systeminsights/"+args.Table, opts)
			if err != nil {
				return errorResult(fmt.Sprintf("querying system insights: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	s.addTool("system_insights_tables", "List all available system insights table names.",
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			tables := []string{
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
			return textResult(strings.Join(tables, "\n")), nil, nil
		},
	)
}

func (s *Server) registerRADIUSTools() {
	addTypedTool(s, "radius_list", "List all RADIUS servers. Returns objects with _id, name, networkSourceIp, authPort, accountingPort.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/radiusservers", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing RADIUS servers: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "radius_get", "Get a single RADIUS server by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.RADIUSServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/radiusservers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting RADIUS server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "radius_create", "Create a new RADIUS server.",
		func(ctx context.Context, req *mcp.CallToolRequest, args radiusCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name":         args.Name,
				"sharedSecret": args.SharedSecret,
			}
			if args.AuthPort > 0 {
				body["authPort"] = args.AuthPort
			}
			if args.AccountingPort > 0 {
				body["accountingPort"] = args.AccountingPort
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/radiusservers", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating RADIUS server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "radius_update", "Update a RADIUS server. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args radiusUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.RADIUSServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.SharedSecret != "" {
				body["sharedSecret"] = args.SharedSecret
			}
			if args.AuthPort > 0 {
				body["authPort"] = args.AuthPort
			}
			if args.AccountingPort > 0 {
				body["accountingPort"] = args.AccountingPort
			}
			if !args.Execute {
				return planResult("update", "RADIUS server", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/radiusservers/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating RADIUS server: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "radius_delete", "Delete a RADIUS server. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveV1(ctx, client, args.Identifier, resolve.RADIUSServerConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "RADIUS server", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/radiusservers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting RADIUS server: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("RADIUS server %q deleted successfully", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerPolicyTemplateTools() {
	addTypedTool(s, "policy_templates_list", "List all policy templates. Returns objects with id, name, description, osMetaFamily.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/policytemplates", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing policy templates: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "policy_templates_get", "Get a single policy template by ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/policytemplates/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("getting policy template: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)
}

func (s *Server) registerAppleMDMTools() {
	addTypedTool(s, "apple_mdm_list", "List all Apple MDM configurations.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			result, err := client.ListAll(ctx, "/applemdms", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing Apple MDM configs: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "apple_mdm_get", "Get an Apple MDM configuration by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AppleMDMConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/applemdms/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting Apple MDM config: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "apple_mdm_create", "Create a new Apple MDM configuration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{"name": args.Name}
			if args.OrgName != "" {
				body["orgName"] = args.OrgName
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/applemdms", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating Apple MDM config: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "apple_mdm_update", "Update an Apple MDM configuration. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AppleMDMConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.OrgName != "" {
				body["orgName"] = args.OrgName
			}
			if !args.Execute {
				return planResult("update", "Apple MDM configuration", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/applemdms/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating Apple MDM config: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "apple_mdm_delete", "Delete an Apple MDM configuration. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AppleMDMConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "Apple MDM configuration", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/applemdms/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting Apple MDM config: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Apple MDM configuration %q deleted successfully", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "apple_mdm_enrollment_profiles", "List enrollment profiles for an Apple MDM configuration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AppleMDMConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/applemdms/"+id+"/enrollmentprofiles", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing enrollment profiles: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "apple_mdm_devices", "List managed devices for an Apple MDM configuration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.AppleMDMConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/applemdms/"+id+"/devices", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing MDM devices: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)
}

func (s *Server) registerPolicyGroupTools() {
	addTypedTool(s, "policy_groups_list", "List all policy groups. Returns objects with id, name, description.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/policygroups", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing policy groups: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "policy_groups_get", "Get a single policy group by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.PolicyGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/policygroups/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting policy group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "policy_groups_create", "Create a new policy group.",
		func(ctx context.Context, req *mcp.CallToolRequest, args policyGroupCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{"name": args.Name}
			if args.Description != "" {
				body["description"] = args.Description
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/policygroups", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating policy group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "policy_groups_update", "Update a policy group. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args policyGroupUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.PolicyGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{}
			if args.Name != "" {
				body["name"] = args.Name
			}
			if args.Description != "" {
				body["description"] = args.Description
			}
			if !args.Execute {
				return planResult("update", "policy group", args.Identifier, id, body)
			}
			data, err := client.Update(ctx, "/policygroups/"+id, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating policy group: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "policy_groups_delete", "Delete a policy group. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.PolicyGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "policy group", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/policygroups/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting policy group: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Policy group %q deleted successfully", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerUserStateTools() {
	addTypedTool(s, "user_states_list", "List all scheduled user state changes.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			result, err := client.ListAll(ctx, "/bulk/userstates", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing user states: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "user_states_get", "Get a scheduled user state change by ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/bulk/userstates/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("getting user state: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "user_states_create", "Schedule a user state change (suspend or reactivate on a given date).",
		func(ctx context.Context, req *mcp.CallToolRequest, args userStateCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			v1Client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V1 client: %v", err)), nil, nil
			}
			userID, err := resolveV1(ctx, v1Client, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{
				"user_id":    userID,
				"state":      args.State,
				"start_date": args.StartDate,
			}
			if args.EndDate != "" {
				body["end_date"] = args.EndDate
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/bulk/userstates", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating user state: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "user_states_delete", "Delete a scheduled user state change. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "user state change", args.Identifier, args.Identifier, nil)
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			_, err = client.Delete(ctx, "/bulk/userstates/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting user state: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("User state change %q deleted successfully", args.Identifier)), nil, nil
		},
	)
}

func (s *Server) registerGsuiteTools() {
	addTypedTool(s, "gsuite_list", "List all JumpCloud Google Workspace (G Suite) integrations.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/gsuites", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing G Suite integrations: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "gsuite_get", "Get a single JumpCloud G Suite integration by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.GsuiteConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/gsuites/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting G Suite integration: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "gsuite_translation_rules", "List translation rules for a G Suite integration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.GsuiteConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/gsuites/"+id+"/translationrules", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing translation rules: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "gsuite_import_users", "List importable users from a G Suite integration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.GsuiteConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/gsuites/"+id+"/import/users", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing importable users: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)
}

func (s *Server) registerOffice365Tools() {
	addTypedTool(s, "office365_list", "List all JumpCloud Office 365 integrations.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/office365s", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing Office 365 integrations: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "office365_get", "Get a single JumpCloud Office 365 integration by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.Office365Config)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/office365s/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting Office 365 integration: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "office365_translation_rules", "List translation rules for an Office 365 integration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.Office365Config)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/office365s/"+id+"/translationrules", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing translation rules: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "office365_import_users", "List importable users from an Office 365 integration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.Office365Config)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/office365s/"+id+"/import/users", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing importable users: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)
}

func (s *Server) registerDuoTools() {
	addTypedTool(s, "duo_list", "List all JumpCloud Duo accounts.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			result, err := client.ListAll(ctx, "/duo/accounts", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing Duo accounts: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "duo_get", "Get a single JumpCloud Duo account by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.DuoAccountConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/duo/accounts/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting Duo account: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "duo_create", "Create a new JumpCloud Duo account.",
		func(ctx context.Context, req *mcp.CallToolRequest, args duoCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			body := map[string]any{"name": args.Name}
			data, err := client.Create(ctx, "/duo/accounts", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating Duo account: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "duo_delete", "Delete a JumpCloud Duo account. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.DuoAccountConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "Duo account", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/duo/accounts/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting Duo account: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Duo account %q deleted successfully.", args.Identifier)), nil, nil
		},
	)

	addTypedTool(s, "duo_apps", "List Duo applications for a Duo account.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.DuoAccountConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, fmt.Sprintf("/duo/accounts/%s/applications", id), api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing Duo applications: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "duo_app_get", "Get a specific Duo application.",
		func(ctx context.Context, req *mcp.CallToolRequest, args duoAppGetInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			accountID, err := r.Resolve(ctx, args.Account, resolve.DuoAccountConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, fmt.Sprintf("/duo/accounts/%s/applications/%s", accountID, args.AppID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting Duo application: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "duo_app_create", "Create a Duo application for a Duo account.",
		func(ctx context.Context, req *mcp.CallToolRequest, args duoAppCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			accountID, err := r.Resolve(ctx, args.Account, resolve.DuoAccountConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{
				"name":    args.Name,
				"apiHost": args.APIHost,
			}
			data, err := client.Create(ctx, fmt.Sprintf("/duo/accounts/%s/applications", accountID), body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating Duo application: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "duo_app_delete", "Delete a Duo application. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args duoAppDeleteInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			accountID, err := r.Resolve(ctx, args.Account, resolve.DuoAccountConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "Duo application", args.AppID, args.AppID, nil)
			}
			_, err = client.Delete(ctx, fmt.Sprintf("/duo/accounts/%s/applications/%s", accountID, args.AppID))
			if err != nil {
				return errorResult(fmt.Sprintf("deleting Duo application: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Duo application %q deleted successfully.", args.AppID)), nil, nil
		},
	)
}

// addTypedTool wraps mcp.AddTool with typed input args, rate limiting, and audit logging.
// Tools that are filtered out by the allow/block list are not registered.
func addTypedTool[In any](s *Server, name, description string, handler func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error)) {
	if !s.toolFilter.isAllowed(name) {
		return
	}

	tool := &mcp.Tool{
		Name:        name,
		Description: description,
	}

	wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		// Rate limit check.
		if err := s.limiter.allow(); err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
			return errorResult(err.Error()), nil, nil
		}

		// Execute the tool.
		result, out, err := handler(ctx, req, args)

		// Audit log.
		if err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
		} else if result != nil && result.IsError {
			errMsg := "tool error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					errMsg = tc.Text
				}
			}
			s.auditLog.log(name, req.Params.Arguments, false, errMsg)
		} else {
			s.auditLog.log(name, req.Params.Arguments, true, "")
		}

		return result, out, err
	}

	mcp.AddTool(s.mcpServer, tool, wrappedHandler)
	s.toolNames = append(s.toolNames, name)
}

// textResult creates a simple text result.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func (s *Server) registerCustomEmailTools() {
	addTypedTool(s, "custom_emails_templates", "List available custom email template definitions from JumpCloud.",
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			result, err := client.ListAll(ctx, "/customemail/templates", api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing custom email templates: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "custom_emails_get", "Get custom email configuration for a specific email type.",
		func(ctx context.Context, req *mcp.CallToolRequest, args customEmailTypeInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/customemails/"+args.EmailType)
			if err != nil {
				return errorResult(fmt.Sprintf("getting custom email config: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "custom_emails_create", "Create a custom email configuration. Set execute=true to create; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args customEmailCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"subject": args.Subject,
			}
			if args.Title != "" {
				body["title"] = args.Title
			}
			if args.Body != "" {
				body["body"] = args.Body
			}
			if args.Header != "" {
				body["header"] = args.Header
			}
			if args.Button != "" {
				body["button"] = args.Button
			}
			if !args.Execute {
				return planResult("create", "custom email", args.EmailType, "", body)
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/customemails/"+args.EmailType, body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating custom email: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "custom_emails_update", "Update a custom email configuration. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args customEmailUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{}
			if args.Subject != "" {
				body["subject"] = args.Subject
			}
			if args.Title != "" {
				body["title"] = args.Title
			}
			if args.Body != "" {
				body["body"] = args.Body
			}
			if args.Header != "" {
				body["header"] = args.Header
			}
			if args.Button != "" {
				body["button"] = args.Button
			}
			if !args.Execute {
				return planResult("update", "custom email", args.EmailType, args.EmailType, body)
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Update(ctx, "/customemails/"+args.EmailType, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating custom email: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "custom_emails_delete", "Delete a custom email configuration. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args customEmailDeleteInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "custom email", args.EmailType, args.EmailType, nil)
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			_, err = client.Delete(ctx, "/customemails/"+args.EmailType)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting custom email: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Custom email %q deleted successfully.", args.EmailType)), nil, nil
		},
	)
}

func (s *Server) registerAppTemplateTools() {
	addTypedTool(s, "app_templates_list", "List available JumpCloud application templates. Returns templates with _id, name, displayName, displayLabel, active.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV1ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			result, err := client.ListAll(ctx, "/application-templates", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing application templates: %v", err)), nil, nil
			}
			return rawListResult(result.Data, result.TotalCount)
		},
	)

	addTypedTool(s, "app_templates_get", "Get a single JumpCloud application template by ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/application-templates/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("getting application template: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)
}

// jsonResult creates a JSON result from a value.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}, nil
}

// errorResult creates an error result.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}
}
