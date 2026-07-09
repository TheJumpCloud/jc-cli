package cmd

// commandClass is the central source of truth for every leaf command's
// mutation class. Each leaf in the Cobra tree MUST have an entry here;
// TestEveryLeafIsClassified fails CI when an unclassified leaf is added.
//
// Use the constants from annotations.go (ClassReadOnly / ClassMutating /
// ClassDestructive / ClassInternal). See those docs for the semantic
// boundaries between the four classes.
//
// Ordering: alphabetical by command path. Keep it that way — the lint
// test prints unclassified paths in sorted order, so insertion is
// trivially location-able.
var commandClass = map[string]string{
	// access-requests — admin-approval workflow for sensitive grants.
	"jc access-requests create": ClassMutating,
	"jc access-requests get":    ClassReadOnly,
	"jc access-requests list":   ClassReadOnly,
	"jc access-requests revoke": ClassDestructive,
	"jc access-requests update": ClassMutating,

	// ad — Active Directory integration.
	"jc ad create": ClassMutating,
	"jc ad delete": ClassDestructive,
	"jc ad get":    ClassReadOnly,
	"jc ad list":   ClassReadOnly,
	"jc ad update": ClassMutating,

	// admins — JumpCloud admin user mgmt.
	"jc admins create": ClassMutating,
	"jc admins delete": ClassDestructive,
	"jc admins get":    ClassReadOnly,
	"jc admins list":   ClassReadOnly,
	"jc admins update": ClassMutating,

	// app-templates — read-only catalog.
	"jc app-templates get":  ClassReadOnly,
	"jc app-templates list": ClassReadOnly,

	// apple-mdm — MDM tenant + payloads catalog (KLA-449..452 arc).
	"jc apple-mdm create":                  ClassMutating,
	"jc apple-mdm delete":                  ClassDestructive,
	"jc apple-mdm devices":                 ClassReadOnly,
	"jc apple-mdm enrollment-profiles":     ClassReadOnly,
	"jc apple-mdm get":                     ClassReadOnly,
	"jc apple-mdm list":                    ClassReadOnly,
	// payloads compose: --create-policy turns this into a JC POST
	// (same wire shape as create-policy), so worst-case capability is
	// mutating, not internal. Cursor Bugbot PR #62 catch.
	"jc apple-mdm payloads compose":        ClassMutating,
	"jc apple-mdm payloads create-policy":  ClassMutating, // POSTs a JC policy
	"jc apple-mdm payloads list":           ClassReadOnly, // vendored catalog
	"jc apple-mdm payloads show":           ClassReadOnly,
	// payloads template is offline-only — emits a .mobileconfig file
	// and never touches the JC API.
	"jc apple-mdm payloads template":       ClassInternal,
	"jc apple-mdm update":                  ClassMutating,

	// apps — JC software app records.
	"jc apps create": ClassMutating,
	"jc apps delete": ClassDestructive,
	"jc apps get":    ClassReadOnly,
	"jc apps list":   ClassReadOnly,
	"jc apps update": ClassMutating,

	// ask — LLM-only flow (doesn't touch the JC API directly).
	"jc ask": ClassInternal,

	// assets — asset/accessories/locations tracking.
	"jc assets accessories create": ClassMutating,
	"jc assets accessories delete": ClassDestructive,
	"jc assets accessories get":    ClassReadOnly,
	"jc assets accessories list":   ClassReadOnly,
	"jc assets accessories update": ClassMutating,
	"jc assets devices create":     ClassMutating,
	"jc assets devices delete":     ClassDestructive,
	"jc assets devices get":        ClassReadOnly,
	"jc assets devices list":       ClassReadOnly,
	"jc assets devices update":     ClassMutating,
	"jc assets locations create":   ClassMutating,
	"jc assets locations delete":   ClassDestructive,
	"jc assets locations get":      ClassReadOnly,
	"jc assets locations list":     ClassReadOnly,
	"jc assets locations update":   ClassMutating,

	// audit — local signed-audit verification.
	"jc audit verify": ClassInternal,

	// auth — local credential mgmt; doesn't write to JC.
	"jc auth login":  ClassInternal,
	"jc auth logout": ClassInternal,
	"jc auth status": ClassInternal,
	"jc auth switch": ClassInternal,

	// auth-policies — auth policy lifecycle + simulation.
	"jc auth-policies blast-radius": ClassReadOnly,
	"jc auth-policies create":       ClassMutating,
	"jc auth-policies delete":       ClassDestructive,
	"jc auth-policies disable":      ClassMutating,
	"jc auth-policies enable":       ClassMutating,
	"jc auth-policies get":          ClassReadOnly,
	"jc auth-policies list":         ClassReadOnly,
	"jc auth-policies simulate":     ClassReadOnly,
	"jc auth-policies update":       ClassMutating,

	// bulk — CSV-driven user batch ops; CAN include delete rows.
	"jc bulk users": ClassDestructive,

	// commands — saved-command lifecycle; `run`/`trigger` are remote code execution.
	"jc commands create":  ClassMutating,
	"jc commands delete":  ClassDestructive,
	"jc commands get":     ClassReadOnly,
	"jc commands list":    ClassReadOnly,
	"jc commands results": ClassReadOnly,
	"jc commands run":     ClassDestructive, // runs arbitrary code on devices
	"jc commands trigger": ClassDestructive, // ditto
	"jc commands update":  ClassMutating,

	// config — local config view/edit only.
	"jc config set":  ClassInternal,
	"jc config view": ClassInternal,

	// custom-emails — branding templates.
	"jc custom-emails create":    ClassMutating,
	"jc custom-emails delete":    ClassDestructive,
	"jc custom-emails get":       ClassReadOnly,
	"jc custom-emails templates": ClassReadOnly,
	"jc custom-emails update":    ClassMutating,

	// devices — managed-device lifecycle. lock/erase/restart all impact end-users.
	"jc devices delete":  ClassDestructive,
	"jc devices erase":   ClassDestructive,
	"jc devices fde-key": ClassReadOnly, // reads stored FDE recovery key
	"jc devices get":     ClassReadOnly,
	"jc devices list":    ClassReadOnly,
	"jc devices lock":    ClassDestructive,
	"jc devices restart": ClassDestructive, // interrupts user work
	"jc devices search":  ClassReadOnly,
	"jc devices update":  ClassMutating,

	// doctor — health probe; local + read-only.
	"jc doctor": ClassInternal,

	// duo — MFA integration + Duo apps.
	"jc duo app-create": ClassMutating,
	"jc duo app-delete": ClassDestructive,
	"jc duo app-get":    ClassReadOnly,
	"jc duo apps":       ClassReadOnly,
	"jc duo create":     ClassMutating,
	"jc duo delete":     ClassDestructive,
	"jc duo get":        ClassReadOnly,
	"jc duo list":       ClassReadOnly,

	// explain — describes a command; doesn't run it.
	"jc explain": ClassInternal,

	// graph — bind/unbind associations across resources.
	"jc graph bind":     ClassMutating,
	"jc graph traverse": ClassReadOnly,
	"jc graph unbind":   ClassDestructive,

	// groups — user/device group mgmt + per-type subcommands.
	"jc groups add-member":    ClassMutating,
	"jc groups device create": ClassMutating,
	"jc groups device delete": ClassDestructive,
	"jc groups device get":    ClassReadOnly,
	"jc groups device list":   ClassReadOnly,
	"jc groups device update": ClassMutating,
	"jc groups remove-member": ClassDestructive,
	"jc groups user create":   ClassMutating,
	"jc groups user delete":   ClassDestructive,
	"jc groups user get":      ClassReadOnly,
	"jc groups user list":     ClassReadOnly,
	"jc groups user update":   ClassMutating,

	// gsuite — G Suite directory integration.
	"jc gsuite get":               ClassReadOnly,
	"jc gsuite import-users":      ClassMutating, // imports users into JC
	"jc gsuite list":              ClassReadOnly,
	"jc gsuite translation-rules": ClassReadOnly,

	// identity-providers — SAML/OIDC IdP lifecycle.
	"jc identity-providers create": ClassMutating,
	"jc identity-providers delete": ClassDestructive,
	"jc identity-providers get":    ClassReadOnly,
	"jc identity-providers list":   ClassReadOnly,
	"jc identity-providers update": ClassMutating,

	// insights — Directory Insights queries. Read-only.
	"jc insights count":    ClassReadOnly,
	"jc insights distinct": ClassReadOnly,
	"jc insights query":    ClassReadOnly,
	"jc insights run":      ClassReadOnly, // runs a saved query
	"jc insights save":     ClassMutating, // persists a query def to JC
	"jc insights saved":    ClassReadOnly,

	// iplists — allow/deny lists for auth policies.
	"jc iplists create": ClassMutating,
	"jc iplists delete": ClassDestructive,
	"jc iplists get":    ClassReadOnly,
	"jc iplists list":   ClassReadOnly,
	"jc iplists update": ClassMutating,

	// ldap — LDAP directory integration + samba domain mgmt.
	"jc ldap create":               ClassMutating,
	"jc ldap delete":               ClassDestructive,
	"jc ldap get":                  ClassReadOnly,
	"jc ldap list":                 ClassReadOnly,
	"jc ldap samba-domain-create":  ClassMutating,
	"jc ldap samba-domain-delete":  ClassDestructive,
	"jc ldap samba-domain-get":     ClassReadOnly,
	"jc ldap samba-domain-update":  ClassMutating,
	"jc ldap samba-domains":        ClassReadOnly,
	"jc ldap update":               ClassMutating,

	// mcp — MCP server launcher + tool list.
	"jc mcp serve": ClassInternal, // runs the server; doesn't itself call JC
	"jc mcp tools": ClassInternal, // lists registered tools

	// office365 — O365 directory integration.
	"jc office365 get":               ClassReadOnly,
	"jc office365 import-users":      ClassMutating,
	"jc office365 list":              ClassReadOnly,
	"jc office365 translation-rules": ClassReadOnly,

	// org — organization metadata + settings.
	"jc org get":      ClassReadOnly,
	"jc org list":     ClassReadOnly,
	"jc org settings": ClassMutating, // writes org settings
	"jc org update":   ClassMutating,

	// policies — JC policy lifecycle.
	"jc policies create":  ClassMutating,
	"jc policies delete":  ClassDestructive,
	"jc policies get":     ClassReadOnly,
	"jc policies list":    ClassReadOnly,
	"jc policies results": ClassReadOnly,
	"jc policies update":  ClassMutating,

	// policy-groups — bundling policies for assignment.
	"jc policy-groups create": ClassMutating,
	"jc policy-groups delete": ClassDestructive,
	"jc policy-groups get":    ClassReadOnly,
	"jc policy-groups list":   ClassReadOnly,
	"jc policy-groups update": ClassMutating,

	// policy-templates — read-only catalog.
	"jc policy-templates get":  ClassReadOnly,
	"jc policy-templates list": ClassReadOnly,

	// radius — RADIUS server lifecycle.
	"jc radius create": ClassMutating,
	"jc radius delete": ClassDestructive,
	"jc radius get":    ClassReadOnly,
	"jc radius list":   ClassReadOnly,
	"jc radius update": ClassMutating,

	// recipe — recipe authoring (local) + execution (variable impact).
	"jc recipe create":   ClassInternal, // local YAML scaffold
	"jc recipe export":   ClassInternal, // writes a yaml file
	"jc recipe import":   ClassInternal, // pulls a yaml file
	"jc recipe list":     ClassInternal, // catalog of recipes
	"jc recipe run":      ClassDestructive, // executes a recipe; can include deletes
	"jc recipe show":     ClassInternal,
	"jc recipe validate": ClassInternal,

	// saas-management — SaaS app discovery + license mgmt.
	"jc saas-management account-delete": ClassDestructive,
	"jc saas-management account-get":    ClassReadOnly,
	"jc saas-management accounts":       ClassReadOnly,
	"jc saas-management catalog-get":    ClassReadOnly,
	"jc saas-management create":         ClassMutating,
	"jc saas-management delete":         ClassDestructive,
	"jc saas-management get":            ClassReadOnly,
	"jc saas-management licenses":       ClassReadOnly,
	"jc saas-management list":           ClassReadOnly,
	"jc saas-management update":         ClassMutating,
	"jc saas-management usage":          ClassReadOnly,

	// schema — machine-readable CLI schema; local introspection.
	"jc schema commands":  ClassInternal,
	"jc schema resources": ClassInternal,

	// setup — interactive bootstrap; local.
	"jc setup": ClassInternal,

	// software — software/license records.
	"jc software associations":     ClassReadOnly,
	"jc software create":           ClassMutating,
	"jc software delete":           ClassDestructive,
	"jc software get":              ClassReadOnly,
	"jc software list":             ClassReadOnly,
	"jc software reclaim-license":  ClassMutating,
	"jc software statuses":         ClassReadOnly,
	"jc software update":           ClassMutating,

	// system-insights — read-only osquery view.
	"jc system-insights list":   ClassReadOnly,
	"jc system-insights tables": ClassReadOnly,

	// tui — interactive launcher; runs whatever the user picks.
	"jc tui": ClassInternal,

	// user-states — scheduled suspend/reactivate.
	"jc user-states create": ClassMutating,
	"jc user-states delete": ClassDestructive,
	"jc user-states get":    ClassReadOnly,
	"jc user-states list":   ClassReadOnly,

	// users — user lifecycle + SSH keys.
	"jc users create":         ClassMutating,
	"jc users delete":         ClassDestructive,
	"jc users get":            ClassReadOnly,
	"jc users list":           ClassReadOnly,
	"jc users lock":           ClassDestructive,
	"jc users reset-mfa":      ClassMutating,
	"jc users reset-password": ClassMutating,
	"jc users search":         ClassReadOnly,
	"jc users ssh-key-add":    ClassMutating,
	"jc users ssh-key-delete": ClassDestructive,
	"jc users ssh-keys":       ClassReadOnly,
	"jc users unlock":         ClassMutating,
	"jc users update":         ClassMutating,

	// version — prints build info.
	"jc version": ClassInternal,

	// windows-mdm — create-policy leaves POST /policies (reversible
	// via policy delete). csp list/show are pure reads, classed
	// read-only to match the apple-mdm payloads list/show precedent
	// (CodeRabbit PR #65 review); template/update stay internal
	// because they write local files (settings-file / cache).
	"jc windows-mdm csp list":               ClassReadOnly,
	"jc windows-mdm csp show":               ClassReadOnly,
	"jc windows-mdm csp template":           ClassInternal,
	"jc windows-mdm csp update":             ClassInternal,
	"jc windows-mdm oma-uri create-policy":  ClassMutating,
	"jc windows-mdm registry create-policy": ClassMutating,
}
