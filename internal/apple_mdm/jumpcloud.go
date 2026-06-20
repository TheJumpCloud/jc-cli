package apple_mdm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/klaassen-consulting/jc/internal/api"
)

// CustomMDMTemplate is the resolved shape we need to construct a
// JumpCloud "MDM Custom Configuration Profile" policy. It's the result
// of looking up one of JumpCloud's custom_mdm_profile_<os> policy
// templates by name + introspecting its configFields for the payload
// and redispatchPolicy field IDs.
//
// We resolve dynamically rather than hardcoding because:
//   - JumpCloud could refresh template IDs (though stable in practice today)
//   - New OS families (custom_mdm_profile_visionos?) will surface without
//     code changes
//   - PR1's vendored Apple schemas track Apple's release; this resolution
//     tracks JumpCloud's catalog — separate axes that shouldn't be
//     coupled in source.
type CustomMDMTemplate struct {
	// ID is the policy template's MongoDB ObjectID. Goes into the
	// request body's template.id field.
	ID string
	// Name is JumpCloud's internal template name (e.g.
	// "custom_mdm_profile_darwin"). Useful for diagnostics — operators
	// can confirm we resolved the right family.
	Name string
	// PayloadFieldID is the configField ID for the .mobileconfig
	// payload itself (the field labeled "Mobile Configuration File"
	// in the Admin Portal).
	PayloadFieldID string
	// RedispatchFieldID is the configField ID for the
	// "Re-apply policy on every OS update" checkbox. Optional in the
	// wire format — older templates predate this field and BuildBody
	// gracefully omits it when empty.
	RedispatchFieldID string
}

// OSFamily values JumpCloud uses for its Custom MDM templates. These
// are JumpCloud's naming, NOT Apple's — Apple says "macOS" while
// JumpCloud's template names use "darwin", etc. Keep this curated so
// adding visionOS later is one line plus a new template name.
const (
	OSFamilyDarwin = "darwin"
	OSFamilyIOS    = "iphone"
	OSFamilyTVOS   = "tvos"
)

// ResolveCustomMDMTemplate finds the Custom MDM template for an OS
// family. It does two API calls: a filtered list to find the template
// ID by name, then a single-get to read the configFields. The two-step
// is required because the list response doesn't include configFields.
//
// Returns an error with the looked-up template name so an operator can
// confirm the name in the Admin Portal if a tenant is missing it.
func ResolveCustomMDMTemplate(ctx context.Context, client *api.V2Client, osFamily string) (CustomMDMTemplate, error) {
	name := "custom_mdm_profile_" + osFamily

	// JumpCloud /policytemplates supports filter on name; this avoids
	// paginating the full template catalog.
	result, err := client.ListAll(ctx, "/policytemplates", api.V2ListOptions{
		Filter: []string{"name:eq:" + name},
	})
	if err != nil {
		return CustomMDMTemplate{}, fmt.Errorf("listing policy templates for %q: %w", name, err)
	}
	if len(result.Data) == 0 {
		return CustomMDMTemplate{}, fmt.Errorf("no Custom MDM template found for OS family %q (looked for template name %q)", osFamily, name)
	}

	var summary struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(result.Data[0], &summary); err != nil {
		return CustomMDMTemplate{}, fmt.Errorf("decoding policy template summary: %w", err)
	}
	if summary.ID == "" {
		return CustomMDMTemplate{}, fmt.Errorf("policy template %q returned empty ID", name)
	}

	return resolveTemplateDetails(ctx, client, summary.ID, summary.Name)
}

func resolveTemplateDetails(ctx context.Context, client *api.V2Client, id, name string) (CustomMDMTemplate, error) {
	detail, err := client.Get(ctx, "/policytemplates/"+id)
	if err != nil {
		return CustomMDMTemplate{}, fmt.Errorf("fetching policy template %s: %w", id, err)
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
		return CustomMDMTemplate{}, fmt.Errorf("decoding policy template detail: %w", err)
	}

	out := CustomMDMTemplate{ID: d.ID, Name: d.Name}
	if out.Name == "" {
		out.Name = name
	}
	for _, f := range d.ConfigFields {
		switch f.Name {
		case "payload":
			out.PayloadFieldID = f.ID
		case "redispatchPolicy":
			out.RedispatchFieldID = f.ID
		}
	}
	if out.PayloadFieldID == "" {
		return out, fmt.Errorf("template %q has no configField named %q — JumpCloud may have renamed the field", name, "payload")
	}
	return out, nil
}

// BuildCustomMDMPolicyBody assembles the POST /policies body for a
// Custom MDM Configuration Profile policy. The shape matches what
// JumpCloud's Admin Portal produces and `jc policies get` returns —
// confirmed against a real policy during the KLA-449 empirical gate.
//
// plistXML is the unsigned .mobileconfig (raw XML); this function
// base64-encodes it for the wire. JumpCloud server-side re-signs.
//
// Pass redispatch=true to enable "Re-apply policy on every OS update"
// (JumpCloud's UI default is true). If the template has no
// RedispatchFieldID (older templates), redispatch is silently
// ignored — there's no wire slot to set it.
func BuildCustomMDMPolicyBody(policyName string, tmpl CustomMDMTemplate, plistXML []byte, redispatch bool) map[string]any {
	encoded := base64.StdEncoding.EncodeToString(plistXML)

	values := []any{
		map[string]any{
			"configFieldID":   tmpl.PayloadFieldID,
			"configFieldName": "payload",
			"value":           encoded,
		},
	}
	if tmpl.RedispatchFieldID != "" {
		values = append(values, map[string]any{
			"configFieldID":   tmpl.RedispatchFieldID,
			"configFieldName": "redispatchPolicy",
			"value":           redispatch,
		})
	}

	return map[string]any{
		"name":     policyName,
		"template": map[string]any{"id": tmpl.ID},
		"values":   values,
	}
}
