package screen

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
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
}

func TestDashboardScreen_HandlesListResult(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	gen := d.generations["users"]
	d.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		Data: []json.RawMessage{
			json.RawMessage(`{"_id":"u1"}`),
			json.RawMessage(`{"_id":"u2"}`),
		},
		TotalCount: 42,
		Generation: gen,
	})

	if d.loading["users"] {
		t.Error("users should not be loading after result")
	}
	if d.counts["users"] != 42 {
		t.Errorf("users count = %d, want 42", d.counts["users"])
	}
}

func TestDashboardScreen_IgnoresStaleResult(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()

	d.Update(fetch.ListResultMsg{
		ResourceKey: "users",
		TotalCount:  99,
		Generation:  d.generations["users"] - 1,
	})

	if !d.loading["users"] {
		t.Error("users should still be loading (stale result)")
	}
}

func TestDashboardScreen_HandlesError(t *testing.T) {
	d := NewDashboardScreen()
	d.Init()

	gen := d.generations["devices"]
	d.Update(fetch.ListResultMsg{
		ResourceKey: "devices",
		Generation:  gen,
		Err:         errTest,
	})

	if d.loading["devices"] {
		t.Error("devices should not be loading after error")
	}
	if d.errors["devices"] == "" {
		t.Error("devices should have an error message")
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

	gen := d.generations["users"]
	d.Update(fetch.CountResultMsg{
		ResourceKey: "users",
		Count:       15,
		Generation:  gen,
	})

	view := d.View()
	if !strings.Contains(view, "15") {
		t.Error("view should show count '15' for users")
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

	view := d.View()
	if !strings.Contains(view, "Recent Events") {
		t.Error("view should contain 'Recent Events' section")
	}
	if !strings.Contains(view, "42") {
		t.Error("view should show event count 42")
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
	view := d.View()
	if !strings.Contains(view, "error") {
		t.Error("view should show error for events widget")
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
	data := []json.RawMessage{
		json.RawMessage(`{"os":"Mac OS X","lastContact":"2099-01-01T00:00:00Z"}`),
		json.RawMessage(`{"os":"Windows","lastContact":"2020-01-01T00:00:00Z"}`),
		json.RawMessage(`{"os":"","lastContact":""}`),
	}

	agg := aggregateDevices(data)
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
}

// errTest is a test error.
var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
