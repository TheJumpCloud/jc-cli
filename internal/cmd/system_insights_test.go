package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
	keyring "github.com/zalando/go-keyring"
)

func setupSystemInsightsTest(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)
	viper.Reset()
	viper.Set("api_key", "test-key")
	viper.Set("cache.directory", dir)
	return dir
}

func startSystemInsightsServer(t *testing.T, tableData []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && len(r.URL.Path) > len("/systeminsights/"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tableData)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSystemInsightsListTable(t *testing.T) {
	setupSystemInsightsTest(t)
	data := []map[string]any{
		{"system_id": "aabbccddee112233aabbcc11", "collection_time": "2026-01-01T00:00:00Z", "version": "14.0"},
		{"system_id": "aabbccddee112233aabbcc22", "collection_time": "2026-01-02T00:00:00Z", "version": "15.0"},
	}
	server := startSystemInsightsServer(t, data)
	defer server.Close()
	overrideV2Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"system-insights", "list", "os_version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected output, got empty")
	}
}

func TestSystemInsightsListTableInvalidTable(t *testing.T) {
	setupSystemInsightsTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"system-insights", "list", "nonexistent_table"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid table")
	}
}

func TestSystemInsightsTables(t *testing.T) {
	setupSystemInsightsTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"system-insights", "tables"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if out == "" {
		t.Fatal("expected table list output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("os_version")) {
		t.Error("expected os_version in table list")
	}
}

func TestSystemInsightsHelp(t *testing.T) {
	setupSystemInsightsTest(t)
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"system-insights", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
