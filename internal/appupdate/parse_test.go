package appupdate

import (
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig(`{"idpCertificate":"x"}`, "--config", "--fields")
	if err != nil || cfg["idpCertificate"] != "x" {
		t.Fatalf("got %v %v", cfg, err)
	}
	// The old CLI form must be refused, naming the fix, not nested.
	_, err = ParseConfig(`{"config":{"idpCertificate":"x"}}`, "--config", "--fields")
	if err == nil || !strings.Contains(err.Error(), "outer \"config\" wrapper") {
		t.Errorf("nested config: %v", err)
	}
	for _, bad := range []string{`[]`, `"x"`, `null`, `{`} {
		if _, err := ParseConfig(bad, "--config", "--fields"); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
}

func TestParseFields(t *testing.T) {
	f, err := ParseFields(`{"description":"d","displayLabel":"L"}`, "--fields", "--config")
	if err != nil || f["description"] != "d" {
		t.Fatalf("got %v %v", f, err)
	}
	for _, bad := range []string{`{"config":{}}`, `{"name":"slack"}`, `{"_id":"a"}`, `{"id":"a"}`, `[]`} {
		if _, err := ParseFields(bad, "--fields", "--config"); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
}
