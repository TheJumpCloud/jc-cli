package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

func TestMerge(t *testing.T) {
	cur := json.RawMessage(`{"_id":"x","name":"bookmark","ssoUrl":"https://a","description":"d","displayLabel":"old"}`)
	body, err := Merge(cur, map[string]any{"displayLabel": "new", "aiReady": true})
	if err != nil {
		t.Fatal(err)
	}
	if body["name"] != "bookmark" || body["ssoUrl"] != "https://a" || body["description"] != "d" {
		t.Errorf("current fields lost: %v", body)
	}
	if body["displayLabel"] != "new" || body["aiReady"] != true {
		t.Errorf("changes not overlaid: %v", body)
	}
	for _, bad := range []string{`null`, `[]`, `x`} {
		if _, err := Merge(json.RawMessage(bad), nil); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestUnapplied(t *testing.T) {
	result := json.RawMessage(`{"name":"bookmark","displayLabel":"old","aiReady":false,"n":3,"config":{"a":1}}`)
	changes := map[string]any{"displayLabel": "new", "name": "bookmark", "aiReady": true, "n": 3, "config": map[string]any{"a": 2}}
	missing, err := Unapplied(result, changes, "aiReady")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(missing, ",") != "displayLabel" {
		t.Errorf("missing = %v, want [displayLabel] (aiReady skipped, objects not compared, ints equal floats)", missing)
	}
	if !strings.Contains(ErrUnapplied(missing).Error(), "did not change") {
		t.Error("message")
	}
}

func TestMerge_ConfigMergedKeyByKey(t *testing.T) {
	current := json.RawMessage(`{"name":"aws-sso","config":{"acsUrl":{"value":"https://a"},"idpEntityId":{"value":"jc"}}}`)
	got, err := Merge(current, map[string]any{"config": map[string]any{"acsUrl": map[string]any{"value": "https://b"}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := got["config"].(map[string]any)
	if cfg["acsUrl"].(map[string]any)["value"] != "https://b" {
		t.Errorf("acsUrl not replaced: %v", cfg)
	}
	if cfg["idpEntityId"] == nil {
		t.Errorf("idpEntityId dropped: a partial config must not wipe the rest: %v", cfg)
	}
}

func TestMerge_ConfigReplacedWhenCurrentIsNull(t *testing.T) {
	got, err := Merge(json.RawMessage(`{"name":"bookmark","config":null}`), map[string]any{"config": map[string]any{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if got["config"].(map[string]any)["k"] != "v" {
		t.Errorf("config = %v", got["config"])
	}
}

type fakeClient struct {
	app     string
	putBody any
	result  string
}

func (f *fakeClient) Get(ctx context.Context, endpoint string) (json.RawMessage, error) {
	return json.RawMessage(f.app), nil
}

func (f *fakeClient) Update(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	f.putBody = body
	return json.RawMessage(f.result), nil
}

func TestApply(t *testing.T) {
	c := &fakeClient{
		app:    `{"_id":"x","name":"bookmark","ssoUrl":"https://u","displayLabel":"Old"}`,
		result: `{"_id":"x","name":"bookmark","ssoUrl":"https://u","displayLabel":"New"}`,
	}
	if _, err := Apply(context.Background(), c, "x", map[string]any{"displayLabel": "New"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	body := c.putBody.(map[string]any)
	if body["name"] != "bookmark" || body["ssoUrl"] != "https://u" || body["displayLabel"] != "New" {
		t.Errorf("PUT body %v: want the full app with the change overlaid", body)
	}

	// The live no-op: 200 with the unchanged app must be an error.
	c.result = c.app
	if _, err := Apply(context.Background(), c, "x", map[string]any{"displayLabel": "New"}); err == nil ||
		!strings.Contains(err.Error(), "displayLabel") {
		t.Errorf("want an unapplied error naming displayLabel, got %v", err)
	}
	// ...unless the caller checks that field itself.
	if _, err := Apply(context.Background(), c, "x", map[string]any{"displayLabel": "New"}, "displayLabel"); err != nil {
		t.Errorf("skipped field should not be checked: %v", err)
	}
}

func TestIsUnknownConnector(t *testing.T) {
	live := &api.APIError{StatusCode: 400, Endpoint: "/applications", Message: `{"status":400,"error":"Could not find the service provider."}`}
	if !IsUnknownConnector(fmt.Errorf("wrapped: %w", live)) {
		t.Error("want the live 400 recognised through wrapping")
	}
	if IsUnknownConnector(&api.APIError{StatusCode: 400, Message: "must include config field"}) {
		t.Error("other 400s are not an unknown connector")
	}
	if IsUnknownConnector(errors.New("Could not find the service provider")) {
		t.Error("only API errors count")
	}
}
