package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolvePath(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		def         apiVersion
		wantVersion apiVersion
		wantPath    string
	}{
		// A path pasted straight out of the API reference must work as-is.
		{"reference v2", "/api/v2/password-vault/status", apiV2, apiV2, "/password-vault/status"},
		{"reference v1", "/api/v1/systemusers", apiV2, apiV1, "/systemusers"},
		{"short v2", "/v2/usergroups", apiV1, apiV2, "/usergroups"},
		{"short v1", "/v1/systems", apiV2, apiV1, "/systems"},
		// No prefix: the --api default decides, and the path is untouched.
		{"bare uses default v2", "/usergroups", apiV2, apiV2, "/usergroups"},
		{"bare uses default v1", "/systemusers", apiV1, apiV1, "/systemusers"},
		{"missing leading slash", "usergroups", apiV2, apiV2, "/usergroups"},
		// A query string belongs to the endpoint and must survive.
		{"query preserved", "/api/v2/systems?limit=1", apiV2, apiV2, "/systems?limit=1"},
		// A path whose own segment merely contains "v1"/"v2" is not a version.
		{"not a version segment", "/api/v2/devices/v2-thing", apiV2, apiV2, "/devices/v2-thing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotV, gotP, err := resolvePath(c.in, c.def)
			if err != nil {
				t.Fatalf("resolvePath(%q) error: %v", c.in, err)
			}
			if gotV != c.wantVersion || gotP != c.wantPath {
				t.Errorf("resolvePath(%q, %v) = (%v, %q), want (%v, %q)",
					c.in, c.def, gotV, gotP, c.wantVersion, c.wantPath)
			}
		})
	}
}

func TestResolvePath_Rejects(t *testing.T) {
	for _, in := range []string{"", "   ", "https://console.jumpcloud.com/api/v2/systems"} {
		if _, _, err := resolvePath(in, apiV2); err == nil {
			t.Errorf("resolvePath(%q) should have failed", in)
		}
	}
}

func TestAppendParams(t *testing.T) {
	cases := []struct {
		endpoint string
		params   []string
		want     string
	}{
		{"/systems", nil, "/systems"},
		{"/systems", []string{"limit=1"}, "/systems?limit=1"},
		{"/systems", []string{"limit=1", "skip=2"}, "/systems?limit=1&skip=2"},
		// An existing query string is extended, not replaced.
		{"/systems?fields=id", []string{"limit=1"}, "/systems?fields=id&limit=1"},
		// An empty value is legitimate — some filters are presence-only.
		{"/systems", []string{"flag="}, "/systems?flag="},
		// Values are encoded, so a filter with a space or & cannot corrupt
		// the rest of the query string.
		{"/systems", []string{"filter=a b&c=d"}, "/systems?filter=a+b%26c%3Dd"},
	}
	for _, c := range cases {
		got, err := appendParams(c.endpoint, c.params)
		if err != nil {
			t.Fatalf("appendParams(%q, %v) error: %v", c.endpoint, c.params, err)
		}
		if got != c.want {
			t.Errorf("appendParams(%q, %v) = %q, want %q", c.endpoint, c.params, got, c.want)
		}
	}
}

func TestAppendParams_Rejects(t *testing.T) {
	for _, p := range [][]string{{"noequals"}, {"=novalue"}} {
		if _, err := appendParams("/systems", p); err == nil {
			t.Errorf("appendParams(%v) should have failed", p)
		}
	}
}

func TestReadBody(t *testing.T) {
	cmd := &cobra.Command{}

	if got, err := readBody(cmd, ""); err != nil || got != nil {
		t.Errorf("empty --data should yield no body, got %q / %v", got, err)
	}

	if got, err := readBody(cmd, `{"a":1}`); err != nil || string(got) != `{"a":1}` {
		t.Errorf("inline JSON = %q / %v", got, err)
	}

	// A file, via @path.
	dir := t.TempDir()
	path := dir + "/body.json"
	if err := os.WriteFile(path, []byte(`{"from":"file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readBody(cmd, "@"+path); err != nil || string(got) != `{"from":"file"}` {
		t.Errorf("@file = %q / %v", got, err)
	}

	// Stdin, via "-".
	cmd.SetIn(strings.NewReader(`{"from":"stdin"}`))
	if got, err := readBody(cmd, "-"); err != nil || string(got) != `{"from":"stdin"}` {
		t.Errorf("stdin = %q / %v", got, err)
	}

	// Invalid JSON fails locally rather than costing a round trip and a 400.
	if _, err := readBody(cmd, `{not json`); err == nil {
		t.Error("invalid JSON should have failed before the request")
	}
	if _, err := readBody(cmd, "@"+dir+"/missing.json"); err == nil {
		t.Error("missing file should have failed")
	}
}

func TestWriteAPIResult(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // substring
		none bool
	}{
		// 204 and an empty body print nothing, not "null".
		{name: "empty", body: "", none: true},
		{name: "null", body: "null", none: true},
		{name: "object", body: `{"id":"abc"}`, want: "abc"},
		{name: "array", body: `[{"id":"a"},{"id":"b"}]`, want: `"b"`},
		// A body that is not JSON at all is printed verbatim: on a probe the
		// broken body is the finding, so a decode error in its place would
		// lose it. This is what a gateway HTML error page looks like.
		{name: "not json", body: `<html>502 Bad Gateway</html>`, want: `502 Bad Gateway`},
		{name: "malformed json", body: `[{"id":]`, want: `[{"id":]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			if err := writeAPIResult(cmd, json.RawMessage(c.body)); err != nil {
				t.Fatalf("writeAPIResult error: %v", err)
			}
			got := out.String()
			if c.none {
				if strings.TrimSpace(got) != "" {
					t.Errorf("expected no output, got %q", got)
				}
				return
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("output %q does not contain %q", got, c.want)
			}
		})
	}
}

func TestAPI_GetRejectsBody(t *testing.T) {
	// --data on a GET is a silent no-op in most HTTP clients. Saying so is
	// better than sending a request whose body the server ignores.
	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"api", "get", "/systems", "-d", `{"a":1}`})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--data is not sent on a GET") {
		t.Errorf("expected a GET-body rejection, got %v", err)
	}
}

func TestAPI_InvalidVersionFlag(t *testing.T) {
	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"api", "get", "/systems", "--api", "v3"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid --api") {
		t.Errorf("expected an --api rejection, got %v", err)
	}
}

func TestAPI_WriteVerbsAreClassifiedDestructive(t *testing.T) {
	// A raw write can do anything the key can do. If someone ever softens
	// this to "mutating", the MCP capability filter and `jc multi
	// --allow-destructive` would both quietly widen.
	for _, verb := range []string{"post", "put", "patch", "delete"} {
		if got := commandClass["jc api "+verb]; got != ClassDestructive {
			t.Errorf("jc api %s is %q, want %q", verb, got, ClassDestructive)
		}
	}
	if got := commandClass["jc api get"]; got != ClassReadOnly {
		t.Errorf("jc api get is %q, want %q", got, ClassReadOnly)
	}
}

func TestIsWriteVerb(t *testing.T) {
	if isWriteVerb("GET") {
		t.Error("GET is not a write")
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if !isWriteVerb(m) {
			t.Errorf("%s should be a write", m)
		}
	}
}

func TestTruncateForPlan(t *testing.T) {
	if got := truncateForPlan("{\n  \"a\": 1\n}"); got != `{ "a": 1 }` {
		t.Errorf("whitespace should collapse, got %q", got)
	}
	long := strings.Repeat("x", 500)
	got := truncateForPlan(long)
	if len([]rune(got)) != 201 || !strings.HasSuffix(got, "…") {
		t.Errorf("long body should truncate to 200 + ellipsis, got %d runes", len([]rune(got)))
	}
}
