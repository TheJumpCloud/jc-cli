package api

import "net/http"

// Extra request headers, scoped to ONE client.
//
// A handful of endpoints require a header the OpenAPI spec marks optional —
// GET /api/v2/agents answers 400 {"message":"x-org-id is required"} without
// one, while /api/v2/agentgroups, the same feature released the same day,
// does not want one at all — and there was no way to send it.
//
// THIS IS DELIBERATELY NOT PROCESS-WIDE. The first version of it was: a
// package-level http.Header with an install/restore pair, because the clients
// are built deep inside command code with no obvious place to thread a header
// through. A mutex made it free of data races and left two real defects,
// both of which land in `jc mcp serve`, which is long-lived and serves tool
// calls concurrently:
//
//  1. CROSS-REQUEST LEAK. While one call held an x-org-id, every other
//     request in the process carried it too. For a multi-tenant key that
//     header SELECTS the organization, so an unrelated call could be
//     answered against an organization its caller never named.
//
//  2. INTERLEAVED RESTORES RESURRECT HEADERS PERMANENTLY. With A and B
//     overlapping: A installs (saving nil), B installs (saving A's), A
//     restores to nil, B restores to — A's headers. They are now installed
//     process-wide with no caller holding them, until something else
//     happens to install. A leak with no end rather than a window.
//
// Per-client state has neither problem, because every caller gets a fresh
// client. The cost is that a caller must configure the client it is about to
// use, which is also the thing that makes the scope obvious at the call site.

// extraHeaderTransport adds headers to each request it passes along.
//
// It wraps the OUTSIDE of the chain, which is what makes it safe: the auth
// transports are outermost and Set (not Add) x-api-key, Authorization,
// Content-Type, Accept and User-Agent, so they run after this and overwrite
// anything here that collides. An extra header therefore CANNOT override
// authentication — a property of the chain's order rather than of a check
// that could be forgotten. `jc api` refuses auth header names anyway, so a
// hostile value is rejected before it gets this far and overwritten if it
// somehow does.
type extraHeaderTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t *extraHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone rather than mutate: a RoundTripper must not alter the request it
	// is given, and the retry transport may replay it.
	r := req.Clone(req.Context())
	for name, values := range t.headers {
		for _, v := range values {
			r.Header.Add(name, v)
		}
	}
	return t.base.RoundTrip(r)
}

// withExtraHeaders wraps an existing transport so requests carry h.
func withExtraHeaders(rt http.RoundTripper, h http.Header) http.RoundTripper {
	if len(h) == 0 {
		return rt
	}
	return &extraHeaderTransport{base: rt, headers: h.Clone()}
}

// SetExtraHeaders makes this client — and only this client — send h with
// every request. Call it after construction and before use; a client is not
// safe to reconfigure while requests are in flight on it.
//
// Passing an empty or nil header set is a no-op, so a caller that may or may
// not have a header to add does not need to branch.
func (c *Client) SetExtraHeaders(h http.Header) {
	if c == nil || c.HTTP == nil {
		return
	}
	c.HTTP.Transport = withExtraHeaders(c.HTTP.Transport, h)
}

// SetExtraHeaders makes this client — and only this client — send h with
// every request. See the Client method.
func (c *V2Client) SetExtraHeaders(h http.Header) {
	if c == nil || c.HTTP == nil {
		return
	}
	c.HTTP.Transport = withExtraHeaders(c.HTTP.Transport, h)
}
