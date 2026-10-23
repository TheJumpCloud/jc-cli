package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The 404 on these routes is ambiguous, and this is where that is pinned down.
//
// getrecoverylockconfig, getrecoverylockpassword and lockpin answer HTTP 404
// both for a device that simply has nothing set and for a device id belonging
// to no device — verified live on 2026-10-22, including against
// 000000000000000000000000, a well-formed object id owned by nothing. The
// bodies are identical, so the status cannot be read on its own.
//
// mdmDeviceGet therefore reads the device before interpreting the 404. An
// agent told "this device has no recovery lock" when the id was simply wrong
// would carry that false negative into whatever it did next, which is the
// worse of the two possible mistakes.
//
// The server below replays exactly the behaviour observed live: the detail
// route answers 200 for the real device and 404 for the unknown one, and the
// sub-route 404s for both.
func liveLike404Server(t *testing.T, realDevice string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		// A sub-route read: 404 whatever the device, as observed.
		if strings.HasSuffix(path, "/getrecoverylockconfig") || strings.HasSuffix(path, "/lockstate") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		// The device detail read: exists only for realDevice.
		if strings.HasSuffix(path, "/devices/"+realDevice) {
			_, _ = w.Write([]byte(`{"id":"` + realDevice + `","serialNumber":"F60GG20MC2"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMDMDeviceTools_AbsenceIsNotABadID(t *testing.T) {
	const mdm = "690b6c939c0bf5000100039a"
	const real = "69c45aabb695320001aff063"
	const unknown = "000000000000000000000000"

	setupToolTest(t)
	overrideV2ClientForTest(t, liveLike404Server(t, real).URL)
	cs := connectToolTestServer(t, Options{})

	// The device exists, so the 404 meant "nothing set".
	out := getResultText(t, callTool(t, cs, "apple_mdm_recovery_lock_config",
		map[string]any{"apple_mdm_id": mdm, "device_id": real}))
	if !strings.Contains(out, "has nothing set") {
		t.Errorf("a real device with no recovery lock should be reported as absence.\nGot: %s", out)
	}
	if strings.Contains(out, "no device") {
		t.Errorf("a real device was reported as not existing.\nGot: %s", out)
	}

	// The device does not exist, so the 404 must NOT be reported as absence.
	out = getResultText(t, callTool(t, cs, "apple_mdm_recovery_lock_config",
		map[string]any{"apple_mdm_id": mdm, "device_id": unknown}))
	if !strings.Contains(out, "no device") {
		t.Errorf("an unknown device id should be named as unknown.\nGot: %s", out)
	}
	if strings.Contains(out, "has nothing set") {
		t.Errorf("an unknown device id was reported as a device with no recovery lock — "+
			"the false negative this two-step exists to prevent.\nGot: %s", out)
	}
}

// The same discrimination on the other device route, so the two-step is not
// wired into one tool only.
func TestMDMDeviceTools_AbsenceIsNotABadIDForLockState(t *testing.T) {
	const mdm = "690b6c939c0bf5000100039a"
	const real = "69c45aabb695320001aff063"

	setupToolTest(t)
	overrideV2ClientForTest(t, liveLike404Server(t, real).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apple_mdm_device_lock_state",
		map[string]any{"apple_mdm_id": mdm, "device_id": real}))
	if !strings.Contains(out, "has nothing set") {
		t.Errorf("got: %s", out)
	}
	out = getResultText(t, callTool(t, cs, "apple_mdm_device_lock_state",
		map[string]any{"apple_mdm_id": mdm, "device_id": "000000000000000000000000"}))
	if !strings.Contains(out, "no device") {
		t.Errorf("got: %s", out)
	}
}
