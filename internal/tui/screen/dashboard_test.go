package screen

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/spf13/viper"
)

func TestDashboardScreen_Title(t *testing.T) {
	d := NewDashboardScreen()
	if d.Title() != "Dashboard" {
		t.Errorf("Title = %q, want 'Dashboard'", d.Title())
	}
}

func TestDashboardScreen_InitFiresFetches(t *testing.T) {
	d := NewDashboardScreen()
	cmd := d.Init()
	if cmd == nil {
		t.Fatal("Init should return a batch command")
	}

	// All resources should be marked loading.
	for _, res := range dashResources {
		if !d.loading[res.Key] {
			t.Errorf("resource %q should be loading after Init", res.Key)
		}
	}
	// Widget keys should also be loading.
	for _, key := range []string{wkUserList, wkDeviceList, wkEvents} {
		if !d.loading[key] {
			t.Errorf("widget %q should be loading after Init", key)
		}
	}

	// Users and devices are NOT in dashResources (derived from aggregation).
	if d.loading["users"] {
		t.Error("users should not have a separate loading key (derived from wkUserList)")
	}
	if d.loading["devices"] {
		t.Error("devices should not have a separate loading key (derived from wkDeviceList)")
	}
}

func TestDashboardScreen_HandlesListResult(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	gen := d.generations["user-groups"]
	d.Update(fetch.ListResultMsg{
		ResourceKey: "user-groups",
		Data: []json.RawMessage{
			json.RawMessage(`{"id":"g1"}`),
			json.RawMessage(`{"id":"g2"}`),
		},
		TotalCount: 42,
		Generation: gen,
	})

	if d.loading["user-groups"] {
		t.Error("user-groups should not be loading after result")
	}
	if d.counts["user-groups"] != 42 {
		t.Errorf("user-groups count = %d, want 42", d.counts["user-groups"])
	}
}

func TestDashboardScreen_IgnoresStaleResult(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()

	d.Update(fetch.ListResultMsg{
		ResourceKey: "user-groups",
		TotalCount:  99,
		Generation:  d.generations["user-groups"] - 1,
	})

	if !d.loading["user-groups"] {
		t.Error("user-groups should still be loading (stale result)")
	}
}

func TestDashboardScreen_HandlesError(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()

	gen := d.generations["commands"]
	d.Update(fetch.CountResultMsg{
		ResourceKey: "commands",
		Generation:  gen,
		Err:         errTest,
	})

	if d.loading["commands"] {
		t.Error("commands should not be loading after error")
	}
	if d.errors["commands"] == "" {
		t.Error("commands should have an error message")
	}
}

func TestDashboardScreen_EscPops(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should return a command")
	}

	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Errorf("expected PopScreenMsg, got %T", msg)
	}
}

func TestDashboardScreen_ViewShowsLabels(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	view := d.View()
	if !strings.Contains(view, "Dashboard") {
		t.Error("view should contain 'Dashboard'")
	}
	if !strings.Contains(view, "Users") {
		t.Error("view should contain 'Users'")
	}
	if !strings.Contains(view, "Devices") {
		t.Error("view should contain 'Devices'")
	}
}

func TestDashboardScreen_ViewShowsCounts(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	gen := d.generations["commands"]
	d.Update(fetch.CountResultMsg{
		ResourceKey: "commands",
		Count:       15,
		Generation:  gen,
	})

	view := d.View()
	if !strings.Contains(view, "15") {
		t.Error("view should show count '15' for commands")
	}
}

func TestDashboardScreen_UserAggregation(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	gen := d.generations[wkUserList]
	d.Update(fetch.ListResultMsg{
		ResourceKey: wkUserList,
		Data: []json.RawMessage{
			json.RawMessage(`{"activated":true,"totp_enabled":true}`),
			json.RawMessage(`{"activated":true,"totp_enabled":false}`),
			json.RawMessage(`{"suspended":true,"totp_enabled":false}`),
			json.RawMessage(`{"account_locked":true,"totp_enabled":true}`),
		},
		TotalCount: 4,
		Generation: gen,
	})

	if d.userAgg == nil {
		t.Fatal("userAgg should not be nil after user list result")
	}
	if d.userAgg.Active != 2 {
		t.Errorf("Active = %d, want 2", d.userAgg.Active)
	}
	if d.userAgg.Suspended != 1 {
		t.Errorf("Suspended = %d, want 1", d.userAgg.Suspended)
	}
	if d.userAgg.Locked != 1 {
		t.Errorf("Locked = %d, want 1", d.userAgg.Locked)
	}
	if d.userAgg.MFAOn != 2 {
		t.Errorf("MFAOn = %d, want 2", d.userAgg.MFAOn)
	}

	// View should show user status chart.
	view := d.View()
	if !strings.Contains(view, "User Status") {
		t.Error("view should contain 'User Status' chart")
	}
	if !strings.Contains(view, "MFA Adoption") {
		t.Error("view should contain 'MFA Adoption' widget")
	}
}

func TestDashboardScreen_DeviceAggregation(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	gen := d.generations[wkDeviceList]
	d.Update(fetch.ListResultMsg{
		ResourceKey: wkDeviceList,
		Data: []json.RawMessage{
			json.RawMessage(`{"os":"Mac OS X","lastContact":"2099-01-01T00:00:00Z"}`),
			json.RawMessage(`{"os":"Windows","lastContact":"2020-01-01T00:00:00Z"}`),
			json.RawMessage(`{"os":"Mac OS X","lastContact":"2099-01-01T00:00:00Z"}`),
		},
		TotalCount: 3,
		Generation: gen,
	})

	if d.deviceAgg == nil {
		t.Fatal("deviceAgg should not be nil after device list result")
	}
	if d.deviceAgg.OSCounts["Mac OS X"] != 2 {
		t.Errorf("macOS count = %d, want 2", d.deviceAgg.OSCounts["Mac OS X"])
	}
	if d.deviceAgg.OSCounts["Windows"] != 1 {
		t.Errorf("Windows count = %d, want 1", d.deviceAgg.OSCounts["Windows"])
	}

	view := d.View()
	if !strings.Contains(view, "Device OS Distribution") {
		t.Error("view should contain 'Device OS Distribution' chart")
	}
	if !strings.Contains(view, "Device Connectivity") {
		t.Error("view should contain 'Device Connectivity' chart")
	}
}

func TestDashboardScreen_InsightsCount(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	gen := d.generations[wkEvents]
	d.Update(fetch.InsightsCountResultMsg{
		ResourceKey: wkEvents,
		Count:       42,
		Generation:  gen,
	})

	if d.eventCount != 42 {
		t.Errorf("eventCount = %d, want 42", d.eventCount)
	}
}

func TestDashboardScreen_InsightsError(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	gen := d.generations[wkEvents]
	d.Update(fetch.InsightsCountResultMsg{
		ResourceKey: wkEvents,
		Generation:  gen,
		Err:         errTest,
	})

	if d.eventsErr == "" {
		t.Error("eventsErr should be set after error")
	}
}

func TestDashboardScreen_EventsByService(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Send per-service event counts.
	for _, svc := range eventServices {
		key := wkEventService(svc)
		gen := d.generations[key]
		d.Update(fetch.InsightsCountResultMsg{
			ResourceKey: key,
			Count:       10,
			Generation:  gen,
		})
	}

	// All services should have count 10.
	for _, svc := range eventServices {
		if d.eventsByService[svc] != 10 {
			t.Errorf("eventsByService[%s] = %d, want 10", svc, d.eventsByService[svc])
		}
	}

	view := d.View()
	if !strings.Contains(view, "Events by Service") {
		t.Error("view should contain 'Events by Service' chart")
	}
}

func TestDashboardScreen_RetryOnError(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// First error should schedule a retry.
	gen := d.generations["commands"]
	_, cmd := d.Update(fetch.CountResultMsg{
		ResourceKey: "commands",
		Generation:  gen,
		Err:         errTest,
	})

	if d.retries["commands"] != 1 {
		t.Errorf("retries[commands] = %d, want 1", d.retries["commands"])
	}
	if cmd == nil {
		t.Error("error should schedule a retry command")
	}
}

func TestDashboardScreen_RetryMaxExceeded(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Exhaust retries.
	d.retries["commands"] = maxRetries

	gen := d.generations["commands"]
	_, cmd := d.Update(fetch.CountResultMsg{
		ResourceKey: "commands",
		Generation:  gen,
		Err:         errTest,
	})

	if cmd != nil {
		t.Error("should not schedule retry after max retries exceeded")
	}
}

func TestDashboardScreen_AutoRefreshConfig(t *testing.T) {
	d := NewDashboardScreen()
	// Default should be 0 (disabled).
	if d.refreshInterval != 0 {
		t.Errorf("default refreshInterval = %v, want 0", d.refreshInterval)
	}
}

func TestDashboardScreen_RefreshResetsState(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Simulate loaded state.
	gen := d.generations[wkUserList]
	d.Update(fetch.ListResultMsg{
		ResourceKey: wkUserList,
		Data:        []json.RawMessage{json.RawMessage(`{"activated":true}`)},
		TotalCount:  1,
		Generation:  gen,
	})

	if d.userAgg == nil {
		t.Fatal("userAgg should be set")
	}

	// Press 'r' to refresh.
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("refresh should return a command")
	}
	if d.userAgg != nil {
		t.Error("userAgg should be nil after refresh")
	}
}

func TestDashboardScreen_ResponsiveColumns(t *testing.T) {
	d := NewDashboardScreen()

	d.width = 120
	if got := d.responsiveColumns(); got != 2 {
		t.Errorf("width=120: columns = %d, want 2", got)
	}
	d.width = 80
	if got := d.responsiveColumns(); got != 1 {
		t.Errorf("width=80: columns = %d, want 1", got)
	}
}

func TestAggregateUsers(t *testing.T) {
	data := []json.RawMessage{
		json.RawMessage(`{"activated":true,"totp_enabled":true}`),
		json.RawMessage(`{"activated":true,"totp_enabled":false}`),
		json.RawMessage(`{"suspended":true}`),
		json.RawMessage(`{"account_locked":true}`),
	}

	agg := aggregateUsers(data)
	if agg.Total != 4 {
		t.Errorf("Total = %d, want 4", agg.Total)
	}
	if agg.Active != 2 {
		t.Errorf("Active = %d, want 2", agg.Active)
	}
	if agg.Suspended != 1 {
		t.Errorf("Suspended = %d, want 1", agg.Suspended)
	}
	if agg.Locked != 1 {
		t.Errorf("Locked = %d, want 1", agg.Locked)
	}
	if agg.MFAOn != 1 {
		t.Errorf("MFAOn = %d, want 1", agg.MFAOn)
	}
}

func TestAggregateDevices(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	data := []json.RawMessage{
		// Online: lastContact < 1h ago
		json.RawMessage(`{"os":"Mac OS X","lastContact":"2026-01-15T11:30:00Z"}`),
		// Offline: lastContact far in the past
		json.RawMessage(`{"os":"Windows","lastContact":"2020-01-01T00:00:00Z"}`),
		// Offline: no lastContact
		json.RawMessage(`{"os":"","lastContact":""}`),
	}

	agg := aggregateDevices(data, now)
	if agg.Total != 3 {
		t.Errorf("Total = %d, want 3", agg.Total)
	}
	if agg.OSCounts["Mac OS X"] != 1 {
		t.Errorf("macOS = %d, want 1", agg.OSCounts["Mac OS X"])
	}
	if agg.OSCounts["Windows"] != 1 {
		t.Errorf("Windows = %d, want 1", agg.OSCounts["Windows"])
	}
	if agg.OSCounts["Unknown"] != 1 {
		t.Errorf("Unknown = %d, want 1", agg.OSCounts["Unknown"])
	}
	if agg.Online != 1 {
		t.Errorf("Online = %d, want 1", agg.Online)
	}
	if agg.Offline != 2 {
		t.Errorf("Offline = %d, want 2", agg.Offline)
	}
}

func TestAggregateDevices_ConnectivityBuckets(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	data := []json.RawMessage{
		// Online: 30min ago
		json.RawMessage(`{"os":"Mac OS X","lastContact":"2026-01-15T11:30:00Z"}`),
		// Recent: 12h ago
		json.RawMessage(`{"os":"Mac OS X","lastContact":"2026-01-15T00:00:00Z"}`),
		// Stale: 3 days ago
		json.RawMessage(`{"os":"Windows","lastContact":"2026-01-12T12:00:00Z"}`),
		// Offline: 30 days ago
		json.RawMessage(`{"os":"Linux","lastContact":"2025-12-16T12:00:00Z"}`),
	}

	agg := aggregateDevices(data, now)
	if agg.Online != 1 {
		t.Errorf("Online = %d, want 1", agg.Online)
	}
	if agg.Recent != 1 {
		t.Errorf("Recent = %d, want 1", agg.Recent)
	}
	if agg.Stale != 1 {
		t.Errorf("Stale = %d, want 1", agg.Stale)
	}
	if agg.Offline != 1 {
		t.Errorf("Offline = %d, want 1", agg.Offline)
	}
}

func TestDashboardScreen_DerivedUserCount(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Send user aggregation data — users count should be derived.
	gen := d.generations[wkUserList]
	d.Update(fetch.ListResultMsg{
		ResourceKey: wkUserList,
		Data: []json.RawMessage{
			json.RawMessage(`{"activated":true}`),
			json.RawMessage(`{"activated":true}`),
			json.RawMessage(`{"activated":true}`),
		},
		TotalCount: 3,
		Generation: gen,
	})

	if d.userAgg == nil {
		t.Fatal("userAgg should be set")
	}
	if d.userAgg.Total != 3 {
		t.Errorf("userAgg.Total = %d, want 3", d.userAgg.Total)
	}
	// View should show "3" for users card.
	view := d.View()
	if !strings.Contains(view, "Users") {
		t.Error("view should contain 'Users' card")
	}
}

func TestDashboardScreen_ScrollIndicator(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 10})

	// At the top.
	indicator := d.scrollIndicator()
	if indicator != "[Top]" {
		t.Errorf("scrollIndicator at top = %q, want [Top]", indicator)
	}
}

func TestDashboardScreen_CardColorHealthy(t *testing.T) {
	d := NewDashboardScreen()
	// No aggregation data — should return fallback.
	c := d.cardColor("users", style.ColorSecondary)
	if c != style.ColorSecondary {
		t.Errorf("cardColor without data = %v, want fallback", c)
	}
}

func TestDashboardScreen_CardColorUsersAmber(t *testing.T) {
	d := NewDashboardScreen()
	// >10% suspended+locked → amber
	d.userAgg = &userAggregation{Active: 8, Suspended: 1, Locked: 1, Total: 10}
	c := d.cardColor("users", style.ColorSecondary)
	if c != style.ColorWarning {
		t.Errorf("cardColor users 20%% bad = %v, want amber", c)
	}
}

func TestDashboardScreen_CardColorUsersRed(t *testing.T) {
	d := NewDashboardScreen()
	// >25% suspended+locked → red
	d.userAgg = &userAggregation{Active: 7, Suspended: 2, Locked: 1, Total: 10}
	c := d.cardColor("users", style.ColorSecondary)
	if c != style.ColorError {
		t.Errorf("cardColor users 30%% bad = %v, want red", c)
	}
}

func TestDashboardScreen_CardColorDevicesAmber(t *testing.T) {
	d := NewDashboardScreen()
	// >25% offline → amber
	d.deviceAgg = &deviceAggregation{Online: 7, Offline: 3, Total: 10}
	c := d.cardColor("devices", style.ColorSuccess)
	if c != style.ColorWarning {
		t.Errorf("cardColor devices 30%% offline = %v, want amber", c)
	}
}

func TestDashboardScreen_CardColorDevicesRed(t *testing.T) {
	d := NewDashboardScreen()
	// >50% offline → red
	d.deviceAgg = &deviceAggregation{Online: 4, Offline: 6, Total: 10}
	c := d.cardColor("devices", style.ColorSuccess)
	if c != style.ColorError {
		t.Errorf("cardColor devices 60%% offline = %v, want red", c)
	}
}

func TestDashboardScreen_CardNavigation(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if d.gridCur != 0 {
		t.Errorf("initial gridCur = %d, want 0", d.gridCur)
	}

	// Move right.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if d.gridCur != 1 {
		t.Errorf("after right: gridCur = %d, want 1", d.gridCur)
	}

	// Move left.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if d.gridCur != 0 {
		t.Errorf("after left: gridCur = %d, want 0", d.gridCur)
	}

	// Don't go below 0.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if d.gridCur != 0 {
		t.Errorf("below zero: gridCur = %d, want 0", d.gridCur)
	}
}

func TestDashboardScreen_TabCyclesFocus(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if d.focusIdx != -1 {
		t.Errorf("initial focusIdx = %d, want -1 (cards)", d.focusIdx)
	}

	// First tab moves to first widget zone.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	if d.focusIdx != 0 {
		t.Errorf("after first tab: focusIdx = %d, want 0", d.focusIdx)
	}

	// Tab through all zones wraps back to -1 (cards).
	for range focusZones {
		d.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if d.focusIdx != -1 {
		t.Errorf("after full cycle: focusIdx = %d, want -1", d.focusIdx)
	}
}

func TestDashboardScreen_PolicyCompliance(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Send policy list result.
	gen := d.generations[wkPolicies]
	_, cmd := d.Update(fetch.ListResultMsg{
		ResourceKey: wkPolicies,
		Data: []json.RawMessage{
			json.RawMessage(`{"id":"pol1","name":"Policy 1"}`),
			json.RawMessage(`{"id":"pol2","name":"Policy 2"}`),
		},
		TotalCount: 2,
		Generation: gen,
	})

	if cmd == nil {
		t.Fatal("policy list result should trigger status fetches")
	}

	// Send policy status results.
	for _, polID := range []string{"pol1", "pol2"} {
		key := wkPolicyStatus(polID)
		statusGen := d.generations[key]
		d.Update(fetch.ListResultMsg{
			ResourceKey: key,
			Data: []json.RawMessage{
				json.RawMessage(`{"status":"applied"}`),
				json.RawMessage(`{"status":"failed"}`),
			},
			TotalCount: 2,
			Generation: statusGen,
		})
	}

	if d.policyCompliance == nil {
		t.Fatal("policyCompliance should be set")
	}
	if d.policyCompliance.Applied != 2 {
		t.Errorf("Applied = %d, want 2", d.policyCompliance.Applied)
	}
	if d.policyCompliance.Failed != 2 {
		t.Errorf("Failed = %d, want 2", d.policyCompliance.Failed)
	}

	view := d.View()
	if !strings.Contains(view, "Policy Compliance") {
		t.Error("view should contain 'Policy Compliance' chart")
	}
}

func TestDashboardScreen_StatCardSelected(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// First card should be selected by default (focusIdx < 0, gridCur = 0).
	d.focusIdx = -1
	d.gridCur = 0
	view := d.View()
	// The view should render — just verify no panic.
	if view == "" {
		t.Error("view should not be empty")
	}
}

func TestDashboardScreen_SparklineData(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Send all 7 daily event counts.
	for i := 0; i < sparklineDays; i++ {
		key := wkEventDay(i)
		gen := d.generations[key]
		d.Update(fetch.InsightsCountResultMsg{
			ResourceKey: key,
			Count:       (i + 1) * 10,
			Generation:  gen,
		})
	}

	if d.sparklineReady != sparklineDays {
		t.Errorf("sparklineReady = %d, want %d", d.sparklineReady, sparklineDays)
	}

	// Verify sparkline data (index 0 = oldest = day offset 6).
	if d.eventSparkline[0] != 70 { // offset 6 → count (6+1)*10=70
		t.Errorf("eventSparkline[0] = %d, want 70", d.eventSparkline[0])
	}
	if d.eventSparkline[6] != 10 { // offset 0 → count (0+1)*10=10
		t.Errorf("eventSparkline[6] = %d, want 10", d.eventSparkline[6])
	}
}

func TestDashboardConfigScreen_Title(t *testing.T) {
	c := NewDashboardConfigScreen()
	if c.Title() != "Dashboard Settings" {
		t.Errorf("Title = %q, want 'Dashboard Settings'", c.Title())
	}
}

func TestDashboardConfigScreen_ToggleWidget(t *testing.T) {
	c := NewDashboardConfigScreen()
	// First widget should be enabled by default.
	first := allWidgets[0].Key
	if !c.enabled[first] {
		t.Errorf("widget %q should be enabled by default", first)
	}

	// Toggle off.
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if c.enabled[first] {
		t.Errorf("widget %q should be disabled after toggle", first)
	}

	// Toggle back on.
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !c.enabled[first] {
		t.Errorf("widget %q should be enabled after second toggle", first)
	}
}

func TestDashboardConfigScreen_Navigation(t *testing.T) {
	c := NewDashboardConfigScreen()

	if c.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", c.cursor)
	}

	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if c.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", c.cursor)
	}

	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if c.cursor != 0 {
		t.Errorf("after k: cursor = %d, want 0", c.cursor)
	}
}

func TestDashboardConfigScreen_EscPops(t *testing.T) {
	c := NewDashboardConfigScreen()
	_, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should return a command")
	}
	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Errorf("expected PopScreenMsg, got %T", msg)
	}
}

func TestIsWidgetEnabled_Default(t *testing.T) {
	viper.Reset()
	// All widgets enabled by default when no config.
	if !IsWidgetEnabled("user-status") {
		t.Error("user-status should be enabled by default")
	}
	if !IsWidgetEnabled("events") {
		t.Error("events should be enabled by default")
	}
}

func TestDashboardScreen_ConfigKeyOpensConfigScreen(t *testing.T) {
	d := NewDashboardScreen()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("c key should return a command")
	}
	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if _, ok := pushMsg.Screen.(*DashboardConfigScreen); !ok {
		t.Errorf("expected DashboardConfigScreen, got %T", pushMsg.Screen)
	}
}

// errTest is a test error.
var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
