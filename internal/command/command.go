// Package command holds the JumpCloud command wire-contract helpers
// shared by the CLI (internal/cmd) and the MCP server (internal/mcp), so
// the two surfaces can never drift on the parts that broke in KLA-484:
// the /runcommand body field, the Windows shell whitelist, and the
// read-modify-write update strip list. Every rule here was verified live
// against the tenant on 2026-07-23 (see the probe in the KLA-484 PR).
package command

import "fmt"

// DefaultWindowsShell is what a Windows command is created with when the
// caller doesn't specify one. A Windows command with an empty shell is
// stored but won't run, so we pick the common case rather than ship a
// broken command.
const DefaultWindowsShell = "powershell"

// RunBody builds the POST body for /runcommand. The command id MUST go
// under "_id": the endpoint 400s with "command id is required" for every
// other field name (probed live: command/commandId/id all fail, _id
// succeeds). The old code sent {"command": id}, which is why
// `jc commands run` never worked.
func RunBody(commandID string, systemIDs, systemGroupIDs []string) map[string]any {
	body := map[string]any{"_id": commandID}
	if len(systemIDs) > 0 {
		body["systems"] = systemIDs
	}
	if len(systemGroupIDs) > 0 {
		body["systemGroups"] = systemGroupIDs
	}
	return body
}

// ValidateShell rejects a Windows shell value that JumpCloud can't run.
// Empty is allowed — the caller decides whether to default it. The API
// does NOT validate this server-side (it stored "bogusshell" verbatim),
// so this client-side gate is the only thing standing between a typo and
// a silently-broken command.
func ValidateShell(shell string) error {
	switch shell {
	case "", "powershell", "cmd":
		return nil
	default:
		return fmt.Errorf(`invalid shell %q: must be "powershell" or "cmd"`, shell)
	}
}

// serverManagedKeys are dropped from a command object before it is PUT
// back as a read-modify-write update. They are server-owned; echoing
// them is at best ignored and at worst rejected. "systems" (device
// associations) is managed through the graph endpoints, not the command
// PUT, so it is stripped too — keeping it risks the PUT reinterpreting
// the associations.
var serverManagedKeys = []string{"_id", "id", "organization", "commandRunners", "systems"}

// StripServerManaged removes server-owned keys from a command object in
// place so the remainder is a clean full-object PUT payload. PUT
// /commands/{id} is a full replace: any field left out reverts to its
// server default (notably commandType → "linux"), so an update must send
// the whole object, not a sparse patch.
func StripServerManaged(obj map[string]any) {
	for _, k := range serverManagedKeys {
		delete(obj, k)
	}
}
