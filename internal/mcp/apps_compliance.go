package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
)

// Compliance-view tuning constants. The "list of offenders" panels are
// bounded so a large org doesn't push a 50,000-row payload through the
// iframe; the percentages still aggregate across the full population.
const (
	complianceListLimit = 200

	// Password-age histogram thresholds in days. Bucket boundaries are
	// exclusive upper bounds; the >90 bucket catches anything stale-er
	// (or any password_date in the future, defensively).
	complianceAgeLT30 = 30
	complianceAgeLT60 = 60
	complianceAgeLT90 = 90
)

// complianceData is the JSON payload pushed to the compliance.html
// iframe. Mirrors the structured-per-card pattern from the dashboard
// and device_view: top-level sections, a Warnings slice for partial
// failures, and a timestamp the UI can show as "snapshot taken at".
type complianceData struct {
	MFA       *complianceMFA      `json:"mfa,omitempty"`
	FDE       *complianceFDE      `json:"fde,omitempty"`
	Passwords *compliancePassword `json:"passwords,omitempty"`
	Admins    *complianceAdmins   `json:"admins,omitempty"`
	Timestamp string              `json:"timestamp"`
	Warnings  []string            `json:"warnings,omitempty"`
}

type complianceMFA struct {
	Total         int                 `json:"total"`
	Enrolled      int                 `json:"enrolled"`
	Percentage    float64             `json:"percentage"`
	WithoutMFA    []complianceUserRef `json:"without_mfa"`
	WithoutMFALen int                 `json:"without_mfa_total"` // total before truncation
}

type complianceUserRef struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
}

type complianceFDE struct {
	Total       int                   `json:"total"`
	Encrypted   int                   `json:"encrypted"`
	Percentage  float64               `json:"percentage"`
	ByOS        []complianceFDEByOS   `json:"by_os"`
	Unencrypted []complianceDeviceRef `json:"unencrypted"`
	// UnencryptedLen lets the UI show "showing N of M" when the list is
	// truncated. complianceListLimit-bounded.
	UnencryptedLen int `json:"unencrypted_total"`
}

type complianceFDEByOS struct {
	OS         string  `json:"os"`
	Total      int     `json:"total"`
	Encrypted  int     `json:"encrypted"`
	Percentage float64 `json:"percentage"`
}

type complianceDeviceRef struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname,omitempty"`
	OS       string `json:"os,omitempty"`
}

type compliancePassword struct {
	// Total counts users with a usable password_date — users with no
	// recorded password date are surfaced separately as NoData so the
	// histogram percentages aren't skewed by missing data.
	Total   int                        `json:"total"`
	NoData  int                        `json:"no_data"`
	Buckets []compliancePasswordBucket `json:"buckets"`
}

type compliancePasswordBucket struct {
	// Label is human-readable (e.g. "<30d", "30-60d"). Index follows
	// the ordering of complianceAgeLT* constants so the UI can render
	// left-to-right without lookups.
	Label      string  `json:"label"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

type complianceAdmins struct {
	Total int                  `json:"total"`
	List  []complianceAdminRef `json:"list"`
}

type complianceAdminRef struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	RoleName          string `json:"role_name,omitempty"`
	EnableMultiFactor bool   `json:"enable_multi_factor"`
	LastLogin         string `json:"last_login,omitempty"`
}

// fetchComplianceData runs the parallel API calls behind compliance_view
// and aggregates the result. Same best-effort contract as device_view:
// a transient sub-fetch failure surfaces as a Warning rather than
// blocking the whole snapshot.
func fetchComplianceData(ctx context.Context) (*complianceData, error) {
	now := nowFunc().UTC()
	data := &complianceData{Timestamp: now.Format(time.RFC3339)}

	var (
		mu       sync.Mutex
		warnings []string
	)
	addWarning := func(msg string) {
		mu.Lock()
		warnings = append(warnings, msg)
		mu.Unlock()
	}

	var wg sync.WaitGroup

	// V1: org users → MFA + password age. One scan, two derived stats.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v1, err := newV1ClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("v1 client (users): %v", err))
			return
		}
		result, err := v1.ListAll(ctx, "/systemusers", api.ListOptions{})
		if err != nil {
			addWarning(fmt.Sprintf("listing users: %v", err))
			return
		}
		mfa, pwd := aggregateUserCompliance(result.Data, now)
		mu.Lock()
		data.MFA = mfa
		data.Passwords = pwd
		mu.Unlock()
	}()

	// V1: devices → FDE coverage segmented by OS, with an unencrypted
	// drill-down list.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v1, err := newV1ClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("v1 client (devices): %v", err))
			return
		}
		result, err := v1.ListAll(ctx, "/systems", api.ListOptions{})
		if err != nil {
			addWarning(fmt.Sprintf("listing devices: %v", err))
			return
		}
		fde := aggregateDeviceCompliance(result.Data)
		mu.Lock()
		data.FDE = fde
		mu.Unlock()
	}()

	// V1: admins live on the V1 /users endpoint (distinct from
	// /systemusers, which is org members). One snapshot pulls every
	// admin row — orgs typically have single-digit admins.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v1, err := newV1ClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("v1 client (admins): %v", err))
			return
		}
		result, err := v1.ListAll(ctx, "/users", api.ListOptions{})
		if err != nil {
			addWarning(fmt.Sprintf("listing admins: %v", err))
			return
		}
		admins := aggregateAdmins(result.Data)
		mu.Lock()
		data.Admins = admins
		mu.Unlock()
	}()

	wg.Wait()

	if len(warnings) > 0 {
		data.Warnings = warnings
	}

	// If every section failed, the snapshot is useless — return an
	// error so the chokepoint surfaces the failure clearly rather than
	// shipping an empty card.
	if data.MFA == nil && data.FDE == nil && data.Admins == nil {
		return nil, fmt.Errorf("all compliance sub-fetches failed: %v", warnings)
	}

	return data, nil
}

// aggregateUserCompliance derives MFA adoption + password-age histogram
// from a single users list scan. Splitting the two aggregations into
// separate goroutines would double the V1 page count for no gain.
func aggregateUserCompliance(data []json.RawMessage, now time.Time) (*complianceMFA, *compliancePassword) {
	mfa := &complianceMFA{}
	pwd := &compliancePassword{}

	bucketCounts := [4]int{} // <30, 30-60, 60-90, >90

	for _, raw := range data {
		var u struct {
			ID            string `json:"_id"`
			Username      string `json:"username"`
			Email         string `json:"email"`
			Activated     bool   `json:"activated"`
			Suspended     bool   `json:"suspended"`
			AccountLocked bool   `json:"account_locked"`
			TOTPEnabled   bool   `json:"totp_enabled"`
			MFA           struct {
				Configured bool `json:"configured"`
			} `json:"mfa"`
			PasswordDate string `json:"password_date"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			continue
		}

		// MFA scope: only count active, non-suspended, non-locked users.
		// A locked user without MFA isn't a compliance concern; an
		// active one without MFA is.
		if u.Activated && !u.Suspended && !u.AccountLocked {
			mfa.Total++
			if u.TOTPEnabled || u.MFA.Configured {
				mfa.Enrolled++
			} else if len(mfa.WithoutMFA) < complianceListLimit {
				mfa.WithoutMFA = append(mfa.WithoutMFA, complianceUserRef{
					ID: u.ID, Username: u.Username, Email: u.Email,
				})
				mfa.WithoutMFALen++
			} else {
				mfa.WithoutMFALen++
			}
		}

		// Password age: bucket by days since password_date. Empty or
		// unparsable dates land in NoData so the histogram only
		// counts users with real data.
		if u.PasswordDate == "" {
			pwd.NoData++
			continue
		}
		t, err := time.Parse(time.RFC3339, u.PasswordDate)
		if err != nil {
			// Some orgs store password_date as date-only ("2026-01-01");
			// try that shape too before giving up.
			t, err = time.Parse("2006-01-02", u.PasswordDate)
		}
		if err != nil {
			pwd.NoData++
			continue
		}
		days := int(now.Sub(t).Hours() / 24)
		switch {
		case days < 0:
			// Future password_date: clock skew, API timestamp oddity,
			// or a tenant that records the *expiration* date instead
			// of the set date. Route to >90d so the anomaly surfaces
			// in the suspicious bucket rather than inflating the
			// healthy <30d bucket. Comment on complianceAgeLT* says
			// the bucket "catches anything stale-er (or any
			// password_date in the future, defensively)" — this is
			// the route that delivers on that promise.
			bucketCounts[3]++
		case days < complianceAgeLT30:
			bucketCounts[0]++
		case days < complianceAgeLT60:
			bucketCounts[1]++
		case days < complianceAgeLT90:
			bucketCounts[2]++
		default:
			bucketCounts[3]++
		}
		pwd.Total++
	}

	if mfa.Total > 0 {
		mfa.Percentage = float64(mfa.Enrolled) / float64(mfa.Total) * 100
	}

	labels := []string{"<30d", "30-60d", "60-90d", ">90d"}
	pwd.Buckets = make([]compliancePasswordBucket, len(labels))
	for i, label := range labels {
		count := bucketCounts[i]
		var pct float64
		if pwd.Total > 0 {
			pct = float64(count) / float64(pwd.Total) * 100
		}
		pwd.Buckets[i] = compliancePasswordBucket{
			Label: label, Count: count, Percentage: pct,
		}
	}

	return mfa, pwd
}

// aggregateDeviceCompliance derives the FDE coverage stats. Per-OS
// buckets are surfaced because compliance reviews often need "we have
// 100 % FDE on Macs but only 60 % on Windows" — a single org-wide
// percentage hides those gaps.
func aggregateDeviceCompliance(data []json.RawMessage) *complianceFDE {
	fde := &complianceFDE{}

	type osStat struct{ total, encrypted int }
	byOS := make(map[string]*osStat)

	for _, raw := range data {
		var d struct {
			ID       string `json:"_id"`
			Hostname string `json:"hostname"`
			OS       string `json:"os"`
			FDE      struct {
				Active bool `json:"active"`
			} `json:"fde"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			continue
		}
		fde.Total++

		osName := d.OS
		if osName == "" {
			osName = "Unknown"
		}
		stat, ok := byOS[osName]
		if !ok {
			stat = &osStat{}
			byOS[osName] = stat
		}
		stat.total++

		if d.FDE.Active {
			fde.Encrypted++
			stat.encrypted++
		} else {
			if len(fde.Unencrypted) < complianceListLimit {
				fde.Unencrypted = append(fde.Unencrypted, complianceDeviceRef{
					ID: d.ID, Hostname: d.Hostname, OS: d.OS,
				})
			}
			fde.UnencryptedLen++
		}
	}

	if fde.Total > 0 {
		fde.Percentage = float64(fde.Encrypted) / float64(fde.Total) * 100
	}

	// Stable per-OS slice sorted by total desc so the busiest platforms
	// surface first — that's what an auditor scrolls to.
	osList := make([]complianceFDEByOS, 0, len(byOS))
	for name, stat := range byOS {
		var pct float64
		if stat.total > 0 {
			pct = float64(stat.encrypted) / float64(stat.total) * 100
		}
		osList = append(osList, complianceFDEByOS{
			OS: name, Total: stat.total, Encrypted: stat.encrypted, Percentage: pct,
		})
	}
	sort.Slice(osList, func(i, j int) bool {
		if osList[i].Total != osList[j].Total {
			return osList[i].Total > osList[j].Total
		}
		return osList[i].OS < osList[j].OS
	})
	fde.ByOS = osList

	return fde
}

// aggregateAdmins compiles the admin inventory from the V1 /users
// response. Orgs typically have single-digit admins; complianceListLimit
// applies defensively in case an org somehow has hundreds.
func aggregateAdmins(data []json.RawMessage) *complianceAdmins {
	admins := &complianceAdmins{Total: len(data)}
	limit := complianceListLimit
	if len(data) < limit {
		limit = len(data)
	}
	for i := 0; i < limit; i++ {
		var a struct {
			ID                string `json:"_id"`
			Email             string `json:"email"`
			RoleName          string `json:"roleName"`
			EnableMultiFactor bool   `json:"enableMultiFactor"`
			LastLogin         string `json:"lastLogin"`
		}
		if err := json.Unmarshal(data[i], &a); err != nil {
			continue
		}
		admins.List = append(admins.List, complianceAdminRef{
			ID: a.ID, Email: a.Email, RoleName: a.RoleName,
			EnableMultiFactor: a.EnableMultiFactor, LastLogin: a.LastLogin,
		})
	}
	// Stable alphabetical sort so an audit run-to-run sees the same
	// order even when the V1 endpoint shuffles results.
	sort.Slice(admins.List, func(i, j int) bool {
		return admins.List[i].Email < admins.List[j].Email
	})
	return admins
}
