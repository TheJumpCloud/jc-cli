package fetch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
)

func TestFlattenAssociation(t *testing.T) {
	// Typical V2 graph response item.
	input := json.RawMessage(`{"to":{"type":"application","id":"app001"},"attributes":null}`)
	result := flattenAssociation(input)

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// "to" should be promoted to top level.
	if _, ok := obj["to"]; ok {
		t.Error("'to' should be removed from top level")
	}

	typeVal := string(obj["type"])
	if typeVal != `"application"` {
		t.Errorf("type = %s, want '\"application\"'", typeVal)
	}

	idVal := string(obj["id"])
	if idVal != `"app001"` {
		t.Errorf("id = %s, want '\"app001\"'", idVal)
	}

	// Non-"to" fields should be preserved.
	if _, ok := obj["attributes"]; !ok {
		t.Error("'attributes' should be preserved")
	}
}

func TestFlattenAssociation_NoToField(t *testing.T) {
	input := json.RawMessage(`{"type":"user","id":"u001"}`)
	result := flattenAssociation(input)

	// Should pass through unchanged.
	if string(result) != string(input) {
		t.Errorf("result = %s, want passthrough", string(result))
	}
}

func TestFlattenAssociation_InvalidJSON(t *testing.T) {
	input := json.RawMessage(`not json`)
	result := flattenAssociation(input)
	if string(result) != string(input) {
		t.Errorf("invalid JSON should pass through unchanged")
	}
}

func TestExtractNameField(t *testing.T) {
	data := json.RawMessage(`{"name":"My Policy","id":"abc123"}`)
	got := extractNameField(data, "name")
	if got != "My Policy" {
		t.Errorf("extractNameField(name) = %q, want 'My Policy'", got)
	}
}

func TestExtractNameField_MissingField(t *testing.T) {
	data := json.RawMessage(`{"id":"abc123"}`)
	got := extractNameField(data, "name")
	if got != "" {
		t.Errorf("extractNameField(missing) = %q, want empty", got)
	}
}

func TestExtractNameField_EmptyFieldName(t *testing.T) {
	data := json.RawMessage(`{"name":"test"}`)
	got := extractNameField(data, "")
	if got != "" {
		t.Errorf("extractNameField(empty) = %q, want empty", got)
	}
}

func TestExtractNameField_NonString(t *testing.T) {
	data := json.RawMessage(`{"name":42}`)
	got := extractNameField(data, "name")
	if got != "" {
		t.Errorf("extractNameField(non-string) = %q, want empty", got)
	}
}

// newTestV1Client creates a V1Client that points at the test server.
func newTestV1Client(serverURL string) (*api.V1Client, error) {
	c := api.NewV1ClientWithKey("test-key")
	c.BaseURL = serverURL + "/api"
	return c, nil
}

func TestFetchV1Search(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/search/systemusers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		fmt.Fprintf(w, `{"results":[{"_id":"u001","username":"alice"}],"totalCount":1}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache: NewCache(),
		NewV1Client: func() (*api.V1Client, error) {
			return newTestV1Client(srv.URL)
		},
	}

	gen := NextGeneration()
	cmd := f.FetchV1Search("users", "/search/systemusers", "alice",
		[]string{"username", "email"}, "username", nil, gen)

	msg := cmd()
	result, ok := msg.(ListResultMsg)
	if !ok {
		t.Fatalf("expected ListResultMsg, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Data))
	}
	if result.TotalCount != 1 {
		t.Errorf("totalCount = %d, want 1", result.TotalCount)
	}
	if result.Generation != gen {
		t.Errorf("generation = %d, want %d", result.Generation, gen)
	}

	// Verify the search body structure.
	sf, ok := receivedBody["searchFilter"].(map[string]any)
	if !ok {
		t.Fatal("searchFilter missing from request body")
	}
	if sf["searchTerm"] != "alice" {
		t.Errorf("searchTerm = %v, want 'alice'", sf["searchTerm"])
	}
}

func TestFetchV1Search_WithFilters(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		fmt.Fprintf(w, `{"results":[],"totalCount":0}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache: NewCache(),
		NewV1Client: func() (*api.V1Client, error) {
			return newTestV1Client(srv.URL)
		},
	}

	filters := []filter.Expression{
		{Field: "activated", Operator: "eq", Value: "true"},
	}

	gen := NextGeneration()
	cmd := f.FetchV1Search("users", "/search/systemusers", "bob",
		[]string{"username"}, "", filters, gen)

	msg := cmd()
	result := msg.(ListResultMsg)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	// Verify filters are included in the body.
	filterVal, ok := receivedBody["filter"]
	if !ok {
		t.Fatal("filter missing from request body")
	}
	filterSlice, ok := filterVal.([]any)
	if !ok {
		t.Fatalf("filter is %T, want []any", filterVal)
	}
	if len(filterSlice) != 1 {
		t.Errorf("filter has %d items, want 1", len(filterSlice))
	}
}

// newTestV2Client creates a V2Client that points at the test server.
func newTestV2Client(serverURL string) (*api.V2Client, error) {
	c := api.NewV2ClientWithKey("test-key")
	c.BaseURL = serverURL + "/api/v2"
	return c, nil
}

func TestFetchMembership_UserGroup(t *testing.T) {
	// User group /members returns {"to":{"id":"...","type":"user"},"attributes":null}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/usergroups/ug001/members" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"to":{"id":"u001","type":"user"},"attributes":null},{"to":{"id":"u002","type":"user"},"attributes":null}]`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV2Client: func() (*api.V2Client, error) { return newTestV2Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.FetchMembership("user-groups", "/usergroups", "ug001", "user", gen)
	msg := cmd()

	result, ok := msg.(AssociationsResultMsg)
	if !ok {
		t.Fatalf("expected AssociationsResultMsg, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 members, got %d", len(result.Data))
	}

	// Verify "to" wrapper was flattened.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(result.Data[0], &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, hasToo := obj["to"]; hasToo {
		t.Error("'to' should be flattened to top level")
	}
	if string(obj["id"]) != `"u001"` {
		t.Errorf("id = %s, want '\"u001\"'", string(obj["id"]))
	}
	if string(obj["type"]) != `"user"` {
		t.Errorf("type = %s, want '\"user\"'", string(obj["type"]))
	}
}

func TestFetchMembership_DeviceGroup(t *testing.T) {
	// Device group /membership returns flat {"id":"...","type":"system",...}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/systemgroups/sg001/membership" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"s001","type":"system","compiledAttributes":null}]`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache:       NewCache(),
		NewV2Client: func() (*api.V2Client, error) { return newTestV2Client(srv.URL) },
	}

	gen := NextGeneration()
	cmd := f.FetchMembership("device-groups", "/systemgroups", "sg001", "system", gen)
	msg := cmd()

	result, ok := msg.(AssociationsResultMsg)
	if !ok {
		t.Fatalf("expected AssociationsResultMsg, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 member, got %d", len(result.Data))
	}

	// Verify flat format is preserved with type and id at top level.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(result.Data[0], &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(obj["id"]) != `"s001"` {
		t.Errorf("id = %s, want '\"s001\"'", string(obj["id"]))
	}
	if string(obj["type"]) != `"system"` {
		t.Errorf("type = %s, want '\"system\"'", string(obj["type"]))
	}
}

func TestFetchV1Search_CachesResults(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		fmt.Fprintf(w, `{"results":[{"_id":"u001"}],"totalCount":1}`)
	}))
	defer srv.Close()

	f := &Fetcher{
		Cache: NewCache(),
		NewV1Client: func() (*api.V1Client, error) {
			return newTestV1Client(srv.URL)
		},
	}

	gen := NextGeneration()
	cmd := f.FetchV1Search("users", "/search/systemusers", "alice",
		[]string{"username"}, "", nil, gen)
	cmd() // First call

	gen2 := NextGeneration()
	cmd2 := f.FetchV1Search("users", "/search/systemusers", "alice",
		[]string{"username"}, "", nil, gen2)
	msg := cmd2()

	result := msg.(ListResultMsg)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if callCount != 1 {
		t.Errorf("server called %d times, want 1 (cached)", callCount)
	}
}
