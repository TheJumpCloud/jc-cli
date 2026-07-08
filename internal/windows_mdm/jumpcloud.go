// Package windows_mdm bridges the jc CLI to JumpCloud's two generic
// Windows custom-policy templates: "Custom MDM (OMA-URI)" and
// "Advanced: Custom Registry Keys". Unlike the Apple side
// (internal/apple_mdm) there is no vendored schema catalog here —
// both templates accept arbitrary settings, so this package is pure
// passthrough: validate the operator/agent-supplied entries, resolve
// the template, assemble the POST /policies body. The Policy CSP
// discovery catalog is a separate follow-up (KLA-460).
package windows_mdm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/klaassen-consulting/jc/internal/api"
)

// JumpCloud's internal template names for the two Windows custom-policy
// shapes. Resolved by name (never by hardcoded ObjectID) for the same
// reasons internal/apple_mdm documents: template IDs could refresh, and
// name-resolution surfaces a clear "not found" on tenants without
// Windows MDM enabled.
//
// Confirmed against a real tenant (2026-07-08) during the KLA-459
// empirical gate.
const (
	TemplateNameOMAURI   = "custom_oma_uri_mdm_windows"
	TemplateNameRegistry = "custom_registry_keys_policy_windows"

	fieldNameOMAURI   = "uriList"
	fieldNameRegistry = "customRegTable"
)

// CustomTemplate is the resolved shape needed to construct a Windows
// custom policy. Both JumpCloud templates carry exactly one list-valued
// configField (a multilist of OMA-URI triples / a table of registry
// rows), so unlike apple_mdm.CustomMDMTemplate there is a single
// FieldID/FieldName pair and no optional extras.
type CustomTemplate struct {
	// ID is the policy template's MongoDB ObjectID. Goes into the
	// request body's template.id field.
	ID string
	// Name is JumpCloud's internal template name (e.g.
	// "custom_oma_uri_mdm_windows") — diagnostics, so operators can
	// confirm the resolved template in the Admin Portal.
	Name string
	// FieldID is the configField ID for the template's single
	// list-valued field.
	FieldID string
	// FieldName is that field's name ("uriList" / "customRegTable").
	FieldName string
}

// OMAURISetting is one entry in the Custom MDM (OMA-URI) template's
// uriList multilist. JSON tags match the wire sub-field names JumpCloud
// stores (confirmed via the template's defaultValue during the
// empirical gate).
type OMAURISetting struct {
	// URI is the OMA-URI path, e.g.
	// ./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption
	URI string `json:"uri"`
	// Format is the wire data type: int, chr (string), float, bool,
	// xml, or b64. NormalizeAndValidateSettings also accepts the
	// Admin Portal's display aliases (string/boolean/base64/integer).
	Format string `json:"format"`
	// Value is the setting value, string-encoded on the wire
	// regardless of Format.
	Value string `json:"value"`
}

// RegistryKey is one row in the Custom Registry Keys template's
// customRegTable. JSON tags match the wire column names.
type RegistryKey struct {
	// Location is the HKLM-relative key path (HKEY_LOCAL_MACHINE is
	// implied — never prefix it). JumpCloud recommends paths under
	// SOFTWARE\Policies. 255-char limit, case-insensitive.
	Location string `json:"customLocation"`
	// ValueName is the registry value name. 99-char limit.
	ValueName string `json:"customValueName"`
	// RegType is the wire value type: DWORD, expandString (EXPAND_SZ),
	// multiString (MULTI_SZ), String (SZ), or QWORD.
	// NormalizeAndValidateKeys also accepts the *_SZ display aliases.
	RegType string `json:"customRegType"`
	// Data is the value data.
	Data string `json:"customData"`
}

// ResolveOMAURITemplate finds the Custom MDM (OMA-URI) template on the
// tenant. See resolveTemplate for the two-step lookup.
func ResolveOMAURITemplate(ctx context.Context, client *api.V2Client) (CustomTemplate, error) {
	return resolveTemplate(ctx, client, TemplateNameOMAURI, fieldNameOMAURI)
}

// ResolveRegistryTemplate finds the Custom Registry Keys template on
// the tenant.
func ResolveRegistryTemplate(ctx context.Context, client *api.V2Client) (CustomTemplate, error) {
	return resolveTemplate(ctx, client, TemplateNameRegistry, fieldNameRegistry)
}

// resolveTemplate mirrors apple_mdm.ResolveCustomMDMTemplate: a
// filtered list to find the template ID by name, then a detail GET to
// read configFields (the list response omits them). The field is
// matched by name, never by hardcoded ID.
func resolveTemplate(ctx context.Context, client *api.V2Client, tmplName, fieldName string) (CustomTemplate, error) {
	result, err := client.ListAll(ctx, "/policytemplates", api.V2ListOptions{
		Filter: []string{"name:eq:" + tmplName},
	})
	if err != nil {
		return CustomTemplate{}, fmt.Errorf("listing policy templates for %q: %w", tmplName, err)
	}
	if len(result.Data) == 0 {
		return CustomTemplate{}, fmt.Errorf(
			"no policy template named %q found — is Windows MDM enabled for this org? (check Policies → New Policy → Windows in the Admin Portal)",
			tmplName)
	}

	var summary struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(result.Data[0], &summary); err != nil {
		return CustomTemplate{}, fmt.Errorf("decoding policy template summary: %w", err)
	}
	if summary.ID == "" {
		return CustomTemplate{}, fmt.Errorf("policy template %q returned empty ID", tmplName)
	}

	detail, err := client.Get(ctx, "/policytemplates/"+summary.ID)
	if err != nil {
		return CustomTemplate{}, fmt.Errorf("fetching policy template %s: %w", summary.ID, err)
	}

	var d struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		ConfigFields []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"configFields"`
	}
	if err := json.Unmarshal(detail, &d); err != nil {
		return CustomTemplate{}, fmt.Errorf("decoding policy template detail: %w", err)
	}

	out := CustomTemplate{ID: d.ID, Name: d.Name, FieldName: fieldName}
	if out.Name == "" {
		out.Name = summary.Name
	}
	for _, f := range d.ConfigFields {
		if f.Name == fieldName {
			out.FieldID = f.ID
			break
		}
	}
	if out.FieldID == "" {
		return out, fmt.Errorf("template %q has no configField named %q — JumpCloud may have renamed the field", tmplName, fieldName)
	}
	return out, nil
}

// BuildOMAURIPolicyBody assembles the POST /policies body for a Custom
// MDM (OMA-URI) policy. Note the shape difference from the Apple side:
// the values[] entry's value is a JSON ARRAY of {uri,format,value}
// triples, not a scalar base64 blob.
func BuildOMAURIPolicyBody(policyName string, tmpl CustomTemplate, settings []OMAURISetting) map[string]any {
	return buildPolicyBody(policyName, tmpl, settings)
}

// BuildRegistryPolicyBody assembles the POST /policies body for a
// Custom Registry Keys policy. The values[] entry's value is a JSON
// array of table rows.
func BuildRegistryPolicyBody(policyName string, tmpl CustomTemplate, keys []RegistryKey) map[string]any {
	return buildPolicyBody(policyName, tmpl, keys)
}

// buildPolicyBody is the shared assembler — both templates have
// exactly one list-valued configField, so the body differs only in
// the field name/ID and the row type.
func buildPolicyBody(policyName string, tmpl CustomTemplate, list any) map[string]any {
	return map[string]any{
		"name":     policyName,
		"template": map[string]any{"id": tmpl.ID},
		"values": []any{
			map[string]any{
				"configFieldID":   tmpl.FieldID,
				"configFieldName": tmpl.FieldName,
				"value":           list,
			},
		},
	}
}
