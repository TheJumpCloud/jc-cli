package accesscert

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeGetter struct{ paths []string }

func (f *fakeGetter) Get(ctx context.Context, endpoint string) (json.RawMessage, error) {
	f.paths = append(f.paths, endpoint)
	return json.RawMessage(`{"results":[{"id":"1"}],"totalCount":1}`), nil
}

func TestIsObjectID(t *testing.T) {
	if !IsObjectID("6ad0e800617e39fb0663b364") {
		t.Error("valid id rejected")
	}
	for _, s := range []string{"", "bad", "6ad0e800617e39fb0663b36", "6ad0e800617e39fb0663b36Z"} {
		if IsObjectID(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestStatusFilters(t *testing.T) {
	if f, _ := StatusFilters(""); len(f) != 1 || f[0] != "" {
		t.Errorf("default: %v", f)
	}
	if f, _ := StatusFilters("all"); len(f) != 3 {
		t.Errorf("all: %v", f)
	}
	if _, err := StatusFilters("bogus"); err == nil {
		t.Error("bogus accepted")
	}
}

func TestListAll_QueryPassThrough(t *testing.T) {
	g := &fakeGetter{}
	_, err := ListAll(context.Background(), g, CampaignsEndpoint, Query{Sort: "-name", Filters: []string{"a:eq:b", "c:eq:d"}, Metadata: true})
	if err != nil {
		t.Fatal(err)
	}
	p := g.paths[0]
	for _, w := range []string{"sort=-name", "filter=a%3Aeq%3Ab", "filter=c%3Aeq%3Ad", "metadata=true", "limit=100", "skip=0"} {
		if !strings.Contains(p, w) {
			t.Errorf("%s missing %s", p, w)
		}
	}
}

func TestStripURLs(t *testing.T) {
	raw := json.RawMessage(`{"status":"exports_ready","artifacts":{"csv":{"url":"https://x.test/a","expiresAt":"t"}}}`)
	out, err := StripURLs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "https://") || !strings.Contains(string(out), `"expiresAt":"t"`) {
		t.Fatalf("unexpected: %s", out)
	}
	if _, err := StripURLs(json.RawMessage(`{"artifacts":[]}`)); err == nil {
		t.Error("array shape accepted")
	}
}
