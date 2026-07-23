package command

import "testing"

func TestRunBody_UsesUnderscoreID(t *testing.T) {
	// The whole point: the id must be under "_id", never "command".
	body := RunBody("abc123", []string{"sys1"}, nil)
	if _, ok := body["_id"]; !ok {
		t.Fatal(`RunBody must put the command id under "_id"`)
	}
	if body["_id"] != "abc123" {
		t.Errorf(`_id = %v, want abc123`, body["_id"])
	}
	if _, ok := body["command"]; ok {
		t.Error(`RunBody must NOT set "command" (the endpoint 400s on it)`)
	}
	if got, _ := body["systems"].([]string); len(got) != 1 || got[0] != "sys1" {
		t.Errorf("systems = %v, want [sys1]", body["systems"])
	}
	if _, ok := body["systemGroups"]; ok {
		t.Error("systemGroups must be omitted when empty")
	}
}

func TestRunBody_GroupTarget(t *testing.T) {
	body := RunBody("abc123", nil, []string{"grp1"})
	if _, ok := body["systems"]; ok {
		t.Error("systems must be omitted when empty")
	}
	if got, _ := body["systemGroups"].([]string); len(got) != 1 || got[0] != "grp1" {
		t.Errorf("systemGroups = %v, want [grp1]", body["systemGroups"])
	}
}

func TestValidateShell(t *testing.T) {
	for _, ok := range []string{"", "powershell", "cmd"} {
		if err := ValidateShell(ok); err != nil {
			t.Errorf("ValidateShell(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"pwsh", "bash", "PowerShell", "command", "bogusshell"} {
		if err := ValidateShell(bad); err == nil {
			t.Errorf("ValidateShell(%q) = nil, want error", bad)
		}
	}
}

func TestStripServerManaged(t *testing.T) {
	obj := map[string]any{
		"_id": "x", "id": "x", "organization": "o", "commandRunners": []any{}, "systems": []any{"s"},
		"name": "keep", "commandType": "windows", "shell": "powershell", "command": "echo",
		"launchType": "manual", "timeout": "0", "trigger": "",
	}
	StripServerManaged(obj)
	for _, gone := range []string{"_id", "id", "organization", "commandRunners", "systems"} {
		if _, ok := obj[gone]; ok {
			t.Errorf("%q should have been stripped", gone)
		}
	}
	// Everything else — the fields a full-object PUT must preserve — stays.
	for _, keep := range []string{"name", "commandType", "shell", "command", "launchType", "timeout", "trigger"} {
		if _, ok := obj[keep]; !ok {
			t.Errorf("%q must be preserved for the read-modify-write PUT", keep)
		}
	}
}
