---
title: A cursor-paged endpoint behind an offset-paging helper reads forever
date: 2026-10-19
category: design-patterns
module: internal/agents
tags: [api-integration, jumpcloud, pagination, parsing, data-integrity]
applies_when:
  - "Adding a list endpoint to a client that already has a generic pagination helper"
  - "An endpoint returns a nextPageToken, a cursor, or any opaque continuation value"
  - "A generic list helper falls back to incrementing an offset when it finds no Link header"
---

# A cursor-paged endpoint behind an offset-paging helper reads forever

## Context

`api.V2Client.ListAll` is jc's generic v2 list helper. It follows an
RFC 5988 `Link: rel="next"` header, and when there is none it falls back
to incrementing `skip` for as long as pages come back full. That
fallback was added deliberately, after a 100-policy tenant silently
truncated `jc policies list` to one page.

`GET /api/v2/agents` pages differently: `pageSize` and an opaque
`pageToken`, with `nextPageToken` in the response body. It emits **no
`Link` header**, and it **ignores `skip`**.

Put those two facts together and the generic helper does this:

1. Request page one. It comes back full, with no `Link` header.
2. Fall back to `skip`-based paging: re-request with `skip=100`.
3. The endpoint ignores `skip` and returns **page one again**.
4. Append those rows. The page is full and has no `Link` header. Go to 2.

The loop never ends. It is not a truncated read — the failure mode the
fallback was built to prevent — it is an **unbounded duplicating** one,
growing the result set by a page per round trip until the context is
cancelled or the process runs out of memory.

That is strictly worse than the bug the fallback fixed. A short read
gives an operator too few rows, which is visible. This gives an
ever-growing list of duplicates, which looks like a big tenant.

## The pattern

**An endpoint's paging model is part of its contract, and a generic
helper encodes exactly one model. Check which one you have before
reusing the helper.**

The giveaway is cheap to look for and worth looking for every time:

| Signal | Means |
|---|---|
| `nextPageToken`, `cursor`, `continuationToken` in the body | cursor paging; a generic offset helper is unsafe |
| No `Link` header on a full page | any offset fallback is flying blind |
| `limit`/`skip` accepted and *ignored* | the fallback's increment is a no-op, and the loop cannot terminate |

The third row is the dangerous one, because an endpoint that **rejected**
`skip` would fail loudly on the second request. One that accepts and
discards it fails silently, and in the direction that produces more data
rather than less.

## What jc does

`internal/agents` pages for itself, in one place, with the reason
written at the call site:

```go
// ListAgents reads every page of /agents by following nextPageToken.
//
// This deliberately does not use api.V2Client.ListAll. [...] /agents emits
// no Link header and ignores skip, so that fallback would re-request the
// first page indefinitely, appending the same rows each time — an unbounded
// duplicating read rather than a truncated one.
```

Two guards make the failure reportable rather than fatal, because the
terminating condition belongs to the server and the server can change:

* **A repeating cursor is an error, not a loop.** If the same
  `nextPageToken` comes back twice, stop and say so. Returning
  duplicates silently would be worse than returning nothing.
* **A page bound.** A server that keeps issuing *fresh* cursors defeats
  the first guard, so the loop also stops after a bounded number of
  pages and reports how many records it had read.

Both are tested by pointing the loop at a server that misbehaves in
exactly those two ways — a test that hangs instead of failing would be
no test at all.

The offset half of the same feature (`/agentgroups`) *does* go through
`ListAll`, because it genuinely is offset-paged and returns a bare
array. The two models live next to each other in one contract package so
that no caller has to remember which is which:

```go
func AgentsPageQuery(pageSize int, pageToken string) url.Values  // cursor
func GroupsPageQuery(limit, skip int) url.Values                 // offset
```

## Why not make the generic helper smarter

It was tempting to teach `ListAll` to notice a `nextPageToken` and
switch models. It was not done, for two reasons:

1. The helper is on the path of roughly forty list commands. A change
   there to serve one endpoint risks all of them, and the blast radius
   is invisible in review.
2. "Noticing" is guessing. The key could be `nextPageToken`,
   `next_page_token`, `cursor` or `continuation`, and a helper that
   guesses wrong falls back to the unsafe path *silently* — which is the
   original bug with an extra step.

An endpoint that pages unusually is better served by code that says so
than by a helper that infers it.

## See also

- `docs/solutions/conventions/empirical-gate-before-coding-2026-06-18.md`
  — the `Link`-header absence was found by reading response headers on a
  live probe, not from the spec, which documents none of this.
- `docs/solutions/conventions/absent-data-is-not-evidence-2026-09-21.md`
  — the sibling failure, where unreadable data becomes a confident zero.
