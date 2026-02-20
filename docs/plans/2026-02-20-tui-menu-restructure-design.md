# TUI Menu Restructure — Admin Console Layout

**Issue**: KLA-151
**Date**: 2026-02-20

## Goal

Restructure the TUI home screen from a single-column grouped list to a three-column grid layout that mirrors the JumpCloud Admin Console's navigation hierarchy.

## Decisions

- **Three-column grid** with responsive fallback (single-column < 90 cols, two-column 90-120, three-column 120+)
- **Unimplemented items shown grayed out** with "Coming soon" flash on Enter
- **Dashboard as a separate screen** (already exists, accessible via `d` key)
- **Cloud Directories as a sub-menu** containing Google Workspace and M365

## New Category Structure

| Category | Implemented Items | Placeholder Items |
|---|---|---|
| **User Management** | Users, User Groups, Active Directory, Cloud Directories (sub-menu: Google Workspace, M365) | HR Directories, Identity Providers |
| **Device Management** | Devices, Device Groups, Commands, Policies, Policy Groups, Software Apps, Apple MDM | Asset Management, Patch Management |
| **Access** | Applications, LDAP Servers, RADIUS Servers | Access Requests, AI & SaaS Management, Vault |
| **Security** | Auth Policies, IP Lists | MFA Configurations, Device Trust, Password Policies |
| **Insights** | Directory Insights, System Insights | |
| **Settings** | Administrators, Organization, Custom Emails, User States, Bulk Operations | |

Items not in the above (App Templates, Policy Templates, Duo) are accessible via filter or bookmarks but not in the primary grid. Duo goes under Settings.

## Column Assignment

Categories are assigned to columns to balance visual height:

- **Column 1**: User Management (6 items), Security (5 items) = 11 items + 2 headers
- **Column 2**: Device Management (9 items), Settings (6 items) = 15 items + 2 headers
- **Column 3**: Access (6 items), Insights (2 items) = 8 items + 2 headers

## Layout

```
┌──────────────────────────────────────────────────────────────────┐
│  JumpCloud TUI                                       d:dashboard │
│                                                                  │
│  User Management       │ Device Management    │ Access           │
│    > Users       (13)  │   > Devices    (10)  │   > SSO Apps (3) │
│    > User Groups  (5)  │   > Device Grps (5)  │   > Access Req.  │
│    > Active Dir   (5)  │   > Commands    (8)  │   > AI & SaaS    │
│    > Cloud Dirs   (>)  │   > Asset Mgmt       │   > Vault        │
│    > HR Dirs            │   > Policies    (5)  │   > LDAP     (5) │
│    > Identity Provs     │   > Policy Grps (5)  │   > RADIUS   (5) │
│                        │   > Patch Mgmt       │                  │
│  Security              │   > Software    (5)  │ Insights         │
│    > Auth Policies (5) │   > MDM         (5)  │   > Dir Insights │
│    > IP Lists      (3) │                      │   > Sys Insights │
│    > MFA Config         │ Settings             │                  │
│    > Device Trust       │   > Admins      (3)  │                  │
│    > Password Pol.      │   > Organization     │                  │
│                        │   > Custom Emails    │                  │
│                        │   > User States      │                  │
│                        │   > Bulk Ops         │                  │
│                        │   > Duo Security     │                  │
│                                                                  │
│  /:filter  b:bookmark  d:dashboard  enter:open  arrows:navigate  │
└──────────────────────────────────────────────────────────────────┘
```

## Navigation

- **Up/Down**: Move cursor within current column
- **Left/Right**: Jump between columns (cursor stays at same relative row, clamped)
- **Enter**: Open resource (push ListScreen), open sub-menu (push SubMenuScreen), or flash "Coming soon" for placeholders
- **Filter mode** (`/`): Collapse to single-column filtered list (existing behavior)
- **Bookmarks**: Continue to work, shown above the grid when present

## Data Model Changes

```go
type ResourceEntry struct {
    // ... existing fields ...
    Placeholder bool              // True for "Coming soon" items
    SubMenu     []ResourceEntry   // Non-nil for sub-menu groupings
}
```

Cursor changes from flat `int` to `gridCursor{col, row}` tracking position across columns.

## Files to Modify

1. `internal/tui/registry.go` — New categories, mappings, placeholder entries, SubMenu field
2. `internal/tui/screen/home.go` — Three-column grid rendering, new cursor model, responsive layout
3. `internal/tui/screen/submenu.go` (new) — Small picker screen for Cloud Directories
4. `internal/tui/registry_test.go` — Updated expectations

## What Stays the Same

- All existing screens (List, Detail, Dashboard, InsightsForm, TablePicker) untouched
- Bookmarking continues to work
- `d` keyboard shortcut for Dashboard stays
- All API/schema/fetch code untouched
- Filter collapses to single-column
