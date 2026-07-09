package windows_mdm

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// DDF v2 parsing (KLA-460). Microsoft describes every Policy CSP area
// in a per-area "DDF v2" XML file (schema documented at
// learn.microsoft.com → configuration-service-provider-ddf). The
// per-node metadata is rich: OMA-URI path, wire format (which matches
// JumpCloud's uriList format enum exactly), description, default,
// allowed values (enum/range/regex), OS-build applicability, and a
// clean ADMX-backed flag.
//
// The files are NOT vendored into this repo — Microsoft's download
// terms don't permit redistribution (unlike Apple's MIT-licensed
// schema repo). See catalog.go for the fetch-on-demand snapshot
// pipeline; this file is the pure parser.

// Setting is one configurable Policy CSP leaf — the catalog's unit.
type Setting struct {
	// Area is the Policy CSP area (Camera, BitLocker, ADMX_AppCompat, …).
	Area string `json:"area"`
	// Name is the policy node name within the area (AllowCamera, …).
	Name string `json:"name"`
	// URI is the full OMA-URI, ready for the oma-uri create-policy
	// path (e.g. ./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera).
	URI string `json:"uri"`
	// Scope is "device" or "user", derived from the DDF Path root.
	// JumpCloud's Custom MDM (OMA-URI) template is device-scoped;
	// user-scoped settings are cataloged but flagged.
	Scope string `json:"scope"`
	// Format is the wire data type (int/chr/bool/float/xml/b64/…) —
	// the same enum JumpCloud's uriList field accepts.
	Format string `json:"format"`
	// Description is Microsoft's per-node prose (from the fetched DDF;
	// never stored in this repo).
	Description string `json:"description,omitempty"`
	// DefaultValue is the node's default, when declared.
	DefaultValue string `json:"default_value,omitempty"`
	// AllowedValues captures the value constraint, when declared.
	AllowedValues *AllowedValues `json:"allowed_values,omitempty"`
	// ADMXBacked marks settings whose values are defined by an external
	// ADMX file — these need ADMX-style XML values (enabled/disabled +
	// data elements), not plain scalars.
	ADMXBacked bool `json:"admx_backed,omitempty"`
	// MinOSBuild is the first OS build the setting applies to
	// (MSFT:Applicability/OsBuildVersion), e.g. "10.0.10240".
	MinOSBuild string `json:"min_os_build,omitempty"`
	// Deprecated marks nodes Microsoft no longer recommends setting.
	Deprecated bool `json:"deprecated,omitempty"`
}

// AllowedValues is the normalized MSFT:AllowedValues constraint.
type AllowedValues struct {
	// Type is Microsoft's ValueType attribute: ENUM, Range, RegEx,
	// Flag, XSD, JSON, SDDL, ADMX, or None.
	Type string `json:"type"`
	// Enum holds the value list for ENUM/Flag types.
	Enum []EnumValue `json:"enum,omitempty"`
	// Value holds the raw constraint for scalar types — a Range like
	// "[0-730]", a RegEx pattern, an XSD blob.
	Value string `json:"value,omitempty"`
}

// EnumValue is one allowed enumeration entry.
type EnumValue struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// ── raw DDF v2 XML shapes ──────────────────────────────────────────
// encoding/xml matches by local name when the tag omits a namespace,
// so the MSFT-namespaced elements (Applicability, AllowedValues, …)
// parse with plain tags.

type ddfMgmtTree struct {
	XMLName xml.Name  `xml:"MgmtTree"`
	Nodes   []ddfNode `xml:"Node"`
}

type ddfNode struct {
	NodeName     string        `xml:"NodeName"`
	Path         string        `xml:"Path"`
	DFProperties ddfProperties `xml:"DFProperties"`
	Nodes        []ddfNode     `xml:"Node"`
}

type ddfProperties struct {
	Description   string            `xml:"Description"`
	DefaultValue  string            `xml:"DefaultValue"`
	DFFormat      ddfFormat         `xml:"DFFormat"`
	Applicability *ddfApplicability `xml:"Applicability"`
	AllowedValues *ddfAllowedValues `xml:"AllowedValues"`
	Deprecated    *struct{}         `xml:"Deprecated"`
}

// ddfFormat encodes DFFormat's child-element-is-the-format shape
// (<DFFormat><int/></DFFormat>).
type ddfFormat struct {
	B64   *struct{} `xml:"b64"`
	Bin   *struct{} `xml:"bin"`
	Bool  *struct{} `xml:"bool"`
	Chr   *struct{} `xml:"chr"`
	Int   *struct{} `xml:"int"`
	Node  *struct{} `xml:"node"`
	Null  *struct{} `xml:"null"`
	XML   *struct{} `xml:"xml"`
	Date  *struct{} `xml:"date"`
	Time  *struct{} `xml:"time"`
	Float *struct{} `xml:"float"`
}

func (f ddfFormat) name() string {
	switch {
	case f.B64 != nil:
		return "b64"
	case f.Bin != nil:
		return "bin"
	case f.Bool != nil:
		return "bool"
	case f.Chr != nil:
		return "chr"
	case f.Int != nil:
		return "int"
	case f.Node != nil:
		return "node"
	case f.Null != nil:
		return "null"
	case f.XML != nil:
		return "xml"
	case f.Date != nil:
		return "date"
	case f.Time != nil:
		return "time"
	case f.Float != nil:
		return "float"
	}
	return ""
}

type ddfApplicability struct {
	OsBuildVersion string `xml:"OsBuildVersion"`
}

type ddfAllowedValues struct {
	ValueType  string    `xml:"ValueType,attr"`
	Enums      []ddfEnum `xml:"Enum"`
	Value      string    `xml:"Value"`
	AdmxBacked *struct{} `xml:"AdmxBacked"`
}

type ddfEnum struct {
	Value            string `xml:"Value"`
	ValueDescription string `xml:"ValueDescription"`
}

// ── parsing ────────────────────────────────────────────────────────

// ParseAreaDDF parses one <Area>_AreaDDF.xml document into its
// settings. Non-leaf nodes (DFFormat node) are structural and skipped;
// leaves become Setting entries with the full OMA-URI reassembled from
// the root Path + node-name chain.
func ParseAreaDDF(data []byte) ([]Setting, error) {
	// Microsoft's files open with a UTF-8 BOM, which encoding/xml
	// rejects as garbage before the declaration.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var tree ddfMgmtTree
	if err := xml.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("parsing DDF XML: %w", err)
	}

	var out []Setting
	for _, root := range tree.Nodes {
		// Root nodes carry the URI prefix in <Path> (e.g.
		// ./Device/Vendor/MSFT/Policy/Config) and the area name in
		// <NodeName>. A single file can hold both a ./Device and a
		// ./User root for the same area.
		scope := "device"
		if strings.HasPrefix(root.Path, "./User") {
			scope = "user"
		}
		base := root.Path + "/" + root.NodeName
		walkDDF(root, base, base, root.NodeName, scope, &out)
	}
	return out, nil
}

func walkDDF(n ddfNode, uri, base, area, scope string, out *[]Setting) {
	for _, child := range n.Nodes {
		childURI := uri + "/" + child.NodeName
		if child.DFProperties.DFFormat.name() == "node" || len(child.Nodes) > 0 {
			// Structural node — recurse. (Policy areas are almost
			// always flat, but a few group settings one level deeper.)
			walkDDF(child, childURI, base, area, scope, out)
			continue
		}
		s := Setting{
			Area: area,
			// Name is relative to the AREA root, not the immediate
			// parent — nested areas yield "Group/Sub" names.
			Name: strings.TrimPrefix(childURI, base+"/"),
			URI:          childURI,
			Scope:        scope,
			Format:       child.DFProperties.DFFormat.name(),
			Description:  strings.TrimSpace(child.DFProperties.Description),
			DefaultValue: child.DFProperties.DefaultValue,
			Deprecated:   child.DFProperties.Deprecated != nil,
		}
		if app := child.DFProperties.Applicability; app != nil {
			// Backported features list multiple builds comma-separated
			// ("10.0.22000, 10.0.19043.1202, …"); the first — without a
			// minor number — is the primary release. Split on the comma
			// (not just whitespace, which left a trailing "," on the
			// build — caught during the KLA-460 live full test).
			first, _, _ := strings.Cut(app.OsBuildVersion, ",")
			s.MinOSBuild = strings.TrimSpace(first)
		}
		if av := child.DFProperties.AllowedValues; av != nil {
			s.ADMXBacked = av.ValueType == "ADMX" || av.AdmxBacked != nil
			norm := &AllowedValues{Type: av.ValueType}
			for _, e := range av.Enums {
				norm.Enum = append(norm.Enum, EnumValue{
					Value:       strings.TrimSpace(e.Value),
					Description: strings.TrimSpace(e.ValueDescription),
				})
			}
			norm.Value = strings.TrimSpace(av.Value)
			s.AllowedValues = norm
		}
		*out = append(*out, s)
	}
}
