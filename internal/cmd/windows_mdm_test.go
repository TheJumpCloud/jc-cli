package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKVSegments_Simple(t *testing.T) {
	kv, err := parseKVSegments(
		"uri=./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera,format=int,value=0",
		[]string{"uri", "format", "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kv["uri"] != "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera" ||
		kv["format"] != "int" || kv["value"] != "0" {
		t.Errorf("parsed wrong: %v", kv)
	}
}

func TestParseKVSegments_CommasInValuePreserved(t *testing.T) {
	// An xml-format OMA-URI value (or multiString registry data) can
	// legitimately contain commas. Segments that don't start with a
	// known key= are continuations of the previous field.
	kv, err := parseKVSegments(
		"uri=./Vendor/MSFT/X,format=xml,value=<items><a>1,2,3</a></items>",
		[]string{"uri", "format", "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kv["value"] != "<items><a>1,2,3</a></items>" {
		t.Errorf("commas in value not preserved: %q", kv["value"])
	}
}

func TestParseKVSegments_MissingFieldErrors(t *testing.T) {
	_, err := parseKVSegments("uri=./Vendor/MSFT/X,format=int", []string{"uri", "format", "value"})
	if err == nil || !strings.Contains(err.Error(), "value") {
		t.Errorf("expected missing-field error naming value, got %v", err)
	}
}

func TestParseKVSegments_DuplicateFieldErrors(t *testing.T) {
	_, err := parseKVSegments("uri=a,uri=b,format=int,value=1", []string{"uri", "format", "value"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate-field error, got %v", err)
	}
}

func TestParseKVSegments_LeadingGarbageErrors(t *testing.T) {
	_, err := parseKVSegments("bogus=1,uri=a,format=int,value=1", []string{"uri", "format", "value"})
	if err == nil {
		t.Error("expected error for unknown leading segment")
	}
}

func TestCollectOMAURISettings_FileAndFlagsMerge(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	fromFile := []map[string]string{
		{"uri": "./Device/Vendor/MSFT/Policy/Config/A/B", "format": "int", "value": "1"},
	}
	b, _ := json.Marshal(fromFile)
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}

	settings, err := collectOMAURISettings(
		[]string{"uri=./Device/Vendor/MSFT/Policy/Config/C/D,format=bool,value=true"}, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(settings) != 2 {
		t.Fatalf("expected 2 settings (file + flag), got %d", len(settings))
	}
	// File entries come first, flags append.
	if settings[0].URI != "./Device/Vendor/MSFT/Policy/Config/A/B" || settings[1].Format != "bool" {
		t.Errorf("merge order wrong: %+v", settings)
	}
}

func TestCollectOMAURISettings_NoneSupplied(t *testing.T) {
	_, err := collectOMAURISettings(nil, "")
	if err == nil || !strings.Contains(err.Error(), "--setting") {
		t.Errorf("expected no-settings error mentioning the flags, got %v", err)
	}
}

func TestCollectRegistryKeys_FriendlyFieldNames(t *testing.T) {
	// Both --key flags and the JSON file use the friendly names
	// (location/name/type/data) — the wire column names
	// (customLocation etc.) are a JumpCloud implementation detail.
	dir := t.TempDir()
	file := filepath.Join(dir, "keys.json")
	if err := os.WriteFile(file, []byte(
		`[{"location":"SOFTWARE\\Policies\\X","name":"V","type":"DWORD","data":"1"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	keys, err := collectRegistryKeys(
		[]string{`location=SOFTWARE\Policies\Y,name=W,type=String,data=hello`}, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].Location != `SOFTWARE\Policies\X` || keys[0].ValueName != "V" {
		t.Errorf("file key mapped wrong: %+v", keys[0])
	}
	if keys[1].RegType != "String" || keys[1].Data != "hello" {
		t.Errorf("flag key mapped wrong: %+v", keys[1])
	}
}

func TestWindowsMDMCommandTreeRegistered(t *testing.T) {
	root := NewRootCmd()
	for _, path := range []string{
		"windows-mdm oma-uri create-policy",
		"windows-mdm registry create-policy",
	} {
		cmd, _, err := root.Find(strings.Split(path, " "))
		if err != nil || cmd.Name() != "create-policy" {
			t.Errorf("command %q not registered: %v", path, err)
		}
	}
}

func TestWindowsMDMLeavesClassifiedMutating(t *testing.T) {
	// Regression guard on the worst-case-capability convention: both
	// leaves POST to /policies and must stay `mutating` — see
	// docs/solutions/conventions/worst-case-capability-classification.
	for _, path := range []string{
		"jc windows-mdm oma-uri create-policy",
		"jc windows-mdm registry create-policy",
	} {
		if got := commandClass[path]; got != ClassMutating {
			t.Errorf("%s classified %q, want %q", path, got, ClassMutating)
		}
	}
}
