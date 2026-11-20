// Package emailsubs covers the calling admin's email subscriptions:
// GET/PUT /notifications/subscriptions/email. Probed 2026-11-19: the listing
// is {subscriptions: [{emailKey, category, title, subscribed}]}; PUT
// {subscriptions: [{emailKey, subscribed}]} MERGES (omitted keys keep their
// value) and refuses an unknown key with 400 "unknown or non-selector
// email_key". It changes only the caller's own preferences.
package emailsubs

import (
	"encoding/json"
	"fmt"
)

const Path = "/notifications/subscriptions/email"

// Subscription is one preference.
type Subscription struct {
	EmailKey   string `json:"emailKey"`
	Title      string `json:"title,omitempty"`
	Subscribed bool   `json:"subscribed"`
}

// Parse reads the listing strictly, keeping the raw rows for output.
func Parse(raw []byte) ([]json.RawMessage, []Subscription, error) {
	var env struct {
		Subscriptions *[]json.RawMessage `json:"subscriptions"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, fmt.Errorf("the email subscriptions response is not an object: %w", err)
	}
	if env.Subscriptions == nil {
		return nil, nil, fmt.Errorf("the email subscriptions response has no subscriptions")
	}
	subs := make([]Subscription, 0, len(*env.Subscriptions))
	for _, r := range *env.Subscriptions {
		var s Subscription
		if err := json.Unmarshal(r, &s); err != nil {
			return nil, nil, fmt.Errorf("an email subscription could not be read: %w", err)
		}
		subs = append(subs, s)
	}
	return *env.Subscriptions, subs, nil
}

// Body is the update for one key.
func Body(key string, subscribed bool) map[string]any {
	return map[string]any{"subscriptions": []map[string]any{{"emailKey": key, "subscribed": subscribed}}}
}

// State returns a key's subscribed flag.
func State(subs []Subscription, key string) (bool, error) {
	for _, s := range subs {
		if s.EmailKey == key {
			return s.Subscribed, nil
		}
	}
	return false, fmt.Errorf("no email subscription %q", key)
}
