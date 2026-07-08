package windows_mdm

import (
	"fmt"
	"strconv"
	"strings"
)

// Wire values for OMAURISetting.Format, in the order JumpCloud's
// Admin Portal presents them. The map values are the display aliases
// the Portal shows (and that operators will naturally reach for) —
// NormalizeAndValidateSettings accepts either form and normalizes to
// the wire value.
var omaURIFormats = map[string][]string{
	"int":   {"integer"},
	"chr":   {"string"},
	"float": {},
	"bool":  {"boolean"},
	"xml":   {},
	"b64":   {"base64"},
}

// Wire values for RegistryKey.RegType with their Admin Portal display
// aliases. Matching is case-insensitive on both forms.
var registryRegTypes = map[string][]string{
	"DWORD":        {"REG_DWORD"},
	"expandString": {"EXPAND_SZ", "REG_EXPAND_SZ"},
	"multiString":  {"MULTI_SZ", "REG_MULTI_SZ"},
	"String":       {"SZ", "REG_SZ"},
	"QWORD":        {"REG_QWORD"},
}

// OMAURIFormats returns the valid wire values for OMAURISetting.Format,
// for help text and MCP tool descriptions.
func OMAURIFormats() []string {
	return []string{"int", "chr", "float", "bool", "xml", "b64"}
}

// RegistryRegTypes returns the valid wire values for
// RegistryKey.RegType, for help text and MCP tool descriptions.
func RegistryRegTypes() []string {
	return []string{"DWORD", "expandString", "multiString", "String", "QWORD"}
}

// normalizeEnum resolves raw against a wire-value→aliases map,
// case-insensitively. Returns the canonical wire value.
func normalizeEnum(raw string, valid map[string][]string) (string, bool) {
	for wire, aliases := range valid {
		if strings.EqualFold(raw, wire) {
			return wire, true
		}
		for _, a := range aliases {
			if strings.EqualFold(raw, a) {
				return wire, true
			}
		}
	}
	return "", false
}

// NormalizeAndValidateSettings validates every OMA-URI setting and
// returns normalized copies (formats canonicalized to wire values).
// All problems across all settings are aggregated into one error so
// the operator fixes everything in one edit cycle — the same
// aggregate-errors convention the apple-mdm compose path follows.
func NormalizeAndValidateSettings(settings []OMAURISetting) ([]OMAURISetting, error) {
	if len(settings) == 0 {
		return nil, fmt.Errorf("at least one OMA-URI setting is required")
	}
	out := make([]OMAURISetting, len(settings))
	var problems []string
	for i, s := range settings {
		label := fmt.Sprintf("setting %d", i+1)
		if s.URI == "" {
			problems = append(problems, label+": uri is required")
		} else if !strings.HasPrefix(s.URI, "./") {
			// The Policy CSP addresses nodes as ./Device/Vendor/MSFT/...,
			// ./User/Vendor/MSFT/..., or the device-implied ./Vendor/MSFT/...
			// form. Anything without the leading ./ is a typo the device
			// would reject much later with a far worse error.
			problems = append(problems, fmt.Sprintf(
				"%s: uri %q must start with ./ (e.g. ./Device/Vendor/MSFT/Policy/Config/...)", label, s.URI))
		}
		format, ok := normalizeEnum(s.Format, omaURIFormats)
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: format %q is not valid; use one of %s", label, s.Format, strings.Join(OMAURIFormats(), ", ")))
		}
		if s.Value == "" {
			problems = append(problems, label+": value is required")
		} else if ok {
			// Cheap type-level sanity for the numeric/bool formats —
			// catching "format=int value=true" here beats a policy that
			// deploys and silently fails on-device.
			switch format {
			case "int":
				if _, err := strconv.ParseInt(s.Value, 10, 64); err != nil {
					problems = append(problems, fmt.Sprintf("%s: value %q is not an integer (format=int)", label, s.Value))
				}
			case "float":
				if _, err := strconv.ParseFloat(s.Value, 64); err != nil {
					problems = append(problems, fmt.Sprintf("%s: value %q is not a float (format=float)", label, s.Value))
				}
			case "bool":
				if !strings.EqualFold(s.Value, "true") && !strings.EqualFold(s.Value, "false") {
					problems = append(problems, fmt.Sprintf("%s: value %q is not a boolean; use true or false (format=bool)", label, s.Value))
				}
			}
		}
		out[i] = OMAURISetting{URI: s.URI, Format: format, Value: s.Value}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid OMA-URI settings:\n  %s", strings.Join(problems, "\n  "))
	}
	return out, nil
}

// NormalizeAndValidateKeys validates every registry row and returns
// normalized copies (reg types canonicalized to wire values). Errors
// aggregate across all rows.
func NormalizeAndValidateKeys(keys []RegistryKey) ([]RegistryKey, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("at least one registry key is required")
	}
	out := make([]RegistryKey, len(keys))
	var problems []string
	for i, k := range keys {
		label := fmt.Sprintf("key %d", i+1)
		switch {
		case k.Location == "":
			problems = append(problems, label+": location is required")
		case hasHiveRoot(k.Location):
			// JumpCloud's template applies everything under
			// HKEY_LOCAL_MACHINE; a hive prefix would end up as a
			// literal subkey named "HKEY_LOCAL_MACHINE" under HKLM.
			problems = append(problems, fmt.Sprintf(
				"%s: location %q must not include a registry hive prefix — HKEY_LOCAL_MACHINE is implied (use e.g. SOFTWARE\\Policies\\...)", label, k.Location))
		case len(k.Location) > 255:
			problems = append(problems, fmt.Sprintf("%s: location exceeds the 255-character limit (%d chars)", label, len(k.Location)))
		}
		switch {
		case k.ValueName == "":
			problems = append(problems, label+": name is required")
		case len(k.ValueName) > 99:
			problems = append(problems, fmt.Sprintf("%s: name exceeds the 99-character limit (%d chars)", label, len(k.ValueName)))
		}
		regType, ok := normalizeEnum(k.RegType, registryRegTypes)
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: type %q is not valid; use one of %s", label, k.RegType, strings.Join(RegistryRegTypes(), ", ")))
		}
		if k.Data == "" {
			problems = append(problems, label+": data is required")
		} else if ok && (regType == "DWORD" || regType == "QWORD") {
			if _, err := strconv.ParseUint(k.Data, 10, 64); err != nil {
				problems = append(problems, fmt.Sprintf("%s: data %q is not an unsigned integer (type=%s)", label, k.Data, regType))
			}
		}
		out[i] = RegistryKey{Location: k.Location, ValueName: k.ValueName, RegType: regType, Data: k.Data}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid registry keys:\n  %s", strings.Join(problems, "\n  "))
	}
	return out, nil
}

// hasHiveRoot reports whether a registry location starts with an
// explicit hive name (HKLM, HKEY_LOCAL_MACHINE, HKCU, ...). Matched
// against the first path segment only, so a legitimate subkey merely
// containing "HKEY" deeper in the path is untouched.
func hasHiveRoot(location string) bool {
	first := location
	if i := strings.IndexAny(location, `\/`); i >= 0 {
		first = location[:i]
	}
	switch strings.ToUpper(first) {
	case "HKLM", "HKCU", "HKCR", "HKU", "HKCC",
		"HKEY_LOCAL_MACHINE", "HKEY_CURRENT_USER", "HKEY_CLASSES_ROOT",
		"HKEY_USERS", "HKEY_CURRENT_CONFIG":
		return true
	}
	return false
}
