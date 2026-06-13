package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// All checks register in init() — keeping them in one file makes the
// catalog auditable at a glance. Add a new check by:
//   1. Writing a check function: func checkXxx(ctx, *Data) ([]Finding, error)
//   2. Registering it in init() with a unique kebab-case ID and category.
//
// Severity is set per-finding (a single check may emit findings at
// different severities — e.g. admin-mfa-coverage is CRITICAL if any
// admin lacks MFA but is silent if every admin has it).
func init() {
	Register(AuditCheck{
		ID:       "admins-without-mfa",
		Title:    "Admins without multi-factor authentication",
		Category: CategorySecurity,
		Run:      checkAdminsWithoutMFA,
	})
	Register(AuditCheck{
		ID:       "users-without-mfa",
		Title:    "Active users without MFA enrolled",
		Category: CategorySecurity,
		Run:      checkUsersWithoutMFA,
	})
	Register(AuditCheck{
		ID:       "suspended-not-locked",
		Title:    "Users suspended but not account-locked",
		Category: CategorySecurity,
		Run:      checkSuspendedNotLocked,
	})
	Register(AuditCheck{
		ID:       "iplists-empty",
		Title:    "IP lists with no entries",
		Category: CategorySecurity,
		Run:      checkEmptyIPLists,
	})

	Register(AuditCheck{
		ID:       "mfa-adoption-rate",
		Title:    "Organization-wide MFA adoption rate",
		Category: CategoryCompliance,
		Run:      checkMFAAdoptionRate,
	})
	Register(AuditCheck{
		ID:       "admin-mfa-coverage",
		Title:    "Admin MFA coverage (target: 100%)",
		Category: CategoryCompliance,
		Run:      checkAdminMFACoverage,
	})
	Register(AuditCheck{
		ID:       "password-age",
		Title:    "Users with passwords older than 90 days",
		Category: CategoryCompliance,
		Run:      checkPasswordAge,
	})
	Register(AuditCheck{
		ID:       "fde-coverage",
		Title:    "Full-disk encryption coverage",
		Category: CategoryCompliance,
		Run:      checkFDECoverage,
	})

	Register(AuditCheck{
		ID:       "stale-devices",
		Title:    "Devices that haven't checked in for 30+ days",
		Category: CategoryHygiene,
		Run:      checkStaleDevices,
	})
	Register(AuditCheck{
		ID:       "auth-policies-disabled",
		Title:    "Authentication policies that are disabled",
		Category: CategoryHygiene,
		Run:      checkDisabledAuthPolicies,
	})

	Register(AuditCheck{
		ID:       "recently-created-admins",
		Title:    "Admins created in the last 14 days",
		Category: CategoryIdentity,
		Run:      checkRecentAdmins,
	})
}

// staleDeviceThreshold is the boundary for the stale-devices check.
// 30 days mirrors the threshold used by the existing
// jc-compliance-check skill.
const staleDeviceThreshold = 30 * 24 * time.Hour

// passwordAgeThreshold is the policy line for the password-age check.
// 90 days mirrors the common compliance bar (SOC 2, ISO 27001 baselines).
const passwordAgeThreshold = 90 * 24 * time.Hour

// recentAdminWindow is the lookback for the recently-created-admins
// check. 14 days surfaces both legitimate onboardings and recently-
// rotated credentials worth a sanity check.
const recentAdminWindow = 14 * 24 * time.Hour

// ─── security ──────────────────────────────────────────────────────

func checkAdminsWithoutMFA(_ context.Context, d *Data) ([]Finding, error) {
	if d.Admins == nil {
		return nil, fmt.Errorf("admins fetch unavailable")
	}
	var findings []Finding
	for _, raw := range d.Admins {
		var a struct {
			ID                string `json:"_id"`
			Email             string `json:"email"`
			EnableMultiFactor bool   `json:"enableMultiFactor"`
			TotpEnrolled      bool   `json:"totpEnrolled"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			continue
		}
		if a.EnableMultiFactor && a.TotpEnrolled {
			continue
		}
		findings = append(findings, Finding{
			CheckID:         "admins-without-mfa",
			Title:           "Admin without MFA",
			Category:        CategorySecurity,
			Severity:        SeverityCritical,
			ResourceRef:     "admin:" + a.Email,
			RemediationHint: "Admin Portal → Administrators → " + a.Email + " → Enable Multi-Factor Authentication. Admin accounts without MFA are the highest-impact persistent risk in any directory.",
			Detail:          fmt.Sprintf("Admin %s has MFA disabled (enableMultiFactor=%v, totpEnrolled=%v).", a.Email, a.EnableMultiFactor, a.TotpEnrolled),
		})
	}
	return findings, nil
}

func checkUsersWithoutMFA(_ context.Context, d *Data) ([]Finding, error) {
	if d.Users == nil {
		return nil, fmt.Errorf("users fetch unavailable")
	}
	var findings []Finding
	for _, raw := range d.Users {
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
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			continue
		}
		// Only flag active users — a locked/suspended user without MFA
		// isn't an attack surface. Mirrors the compliance_view scoping.
		if !u.Activated || u.Suspended || u.AccountLocked {
			continue
		}
		if u.TOTPEnabled || u.MFA.Configured {
			continue
		}
		findings = append(findings, Finding{
			CheckID:         "users-without-mfa",
			Title:           "Active user without MFA",
			Category:        CategorySecurity,
			Severity:        SeverityHigh,
			ResourceRef:     "user:" + u.Username,
			RemediationHint: "Enforce MFA via an auth policy (`jc auth-policies`) covering this user's groups, or have the user enroll via the JumpCloud user portal.",
			Detail:          fmt.Sprintf("Active user %s (%s) has no MFA factor enrolled.", u.Username, u.Email),
		})
	}
	return findings, nil
}

func checkSuspendedNotLocked(_ context.Context, d *Data) ([]Finding, error) {
	if d.Users == nil {
		return nil, fmt.Errorf("users fetch unavailable")
	}
	var findings []Finding
	for _, raw := range d.Users {
		var u struct {
			ID            string `json:"_id"`
			Username      string `json:"username"`
			Suspended     bool   `json:"suspended"`
			AccountLocked bool   `json:"account_locked"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			continue
		}
		if !u.Suspended || u.AccountLocked {
			continue
		}
		findings = append(findings, Finding{
			CheckID:         "suspended-not-locked",
			Title:           "Suspended user without account lock",
			Category:        CategorySecurity,
			Severity:        SeverityMedium,
			ResourceRef:     "user:" + u.Username,
			RemediationHint: "Run `jc users lock " + u.Username + "` — suspension alone leaves residual session/token attack surface; locking forces re-auth on every gate.",
			Detail:          fmt.Sprintf("User %s is suspended but account_locked=false.", u.Username),
		})
	}
	return findings, nil
}

func checkEmptyIPLists(_ context.Context, d *Data) ([]Finding, error) {
	if d.IPLists == nil {
		return nil, fmt.Errorf("ip lists fetch unavailable")
	}
	var findings []Finding
	for _, raw := range d.IPLists {
		var l struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			IPs  []any  `json:"ips"`
		}
		if err := json.Unmarshal(raw, &l); err != nil {
			continue
		}
		if len(l.IPs) > 0 {
			continue
		}
		findings = append(findings, Finding{
			CheckID:         "iplists-empty",
			Title:           "IP list with no entries",
			Category:        CategorySecurity,
			Severity:        SeverityLow,
			ResourceRef:     "iplist:" + l.Name,
			RemediationHint: "Either populate the list (`jc iplists update`) or delete it (`jc iplists delete`). An empty IP list referenced by an auth policy fails open or closed depending on policy semantics — both are footguns.",
			Detail:          "IP list " + l.Name + " has no IP entries.",
		})
	}
	return findings, nil
}

// ─── compliance ────────────────────────────────────────────────────

func checkMFAAdoptionRate(_ context.Context, d *Data) ([]Finding, error) {
	if d.Users == nil {
		return nil, fmt.Errorf("users fetch unavailable")
	}
	total, enrolled := 0, 0
	for _, raw := range d.Users {
		var u struct {
			Activated     bool `json:"activated"`
			Suspended     bool `json:"suspended"`
			AccountLocked bool `json:"account_locked"`
			TOTPEnabled   bool `json:"totp_enabled"`
			MFA           struct {
				Configured bool `json:"configured"`
			} `json:"mfa"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			continue
		}
		if !u.Activated || u.Suspended || u.AccountLocked {
			continue
		}
		total++
		if u.TOTPEnabled || u.MFA.Configured {
			enrolled++
		}
	}
	if total == 0 {
		return nil, nil
	}
	pct := float64(enrolled) / float64(total) * 100
	sev := mfaAdoptionSeverity(pct)
	if sev == "" {
		return nil, nil // ≥95% — no finding
	}
	return []Finding{{
		CheckID:         "mfa-adoption-rate",
		Title:           "MFA adoption below target",
		Category:        CategoryCompliance,
		Severity:        sev,
		ResourceRef:     "org",
		RemediationHint: "Enforce MFA via auth policies on user groups. Tracked threshold: 95% adoption; common compliance frameworks (SOC 2, ISO 27001) consider <95% a material control gap.",
		Detail:          fmt.Sprintf("%d of %d active users enrolled in MFA (%.1f%%).", enrolled, total, pct),
	}}, nil
}

// mfaAdoptionSeverity scales severity to adoption rate. The brackets
// are deliberate: <50% is critical (controls don't exist), 50-80% is
// high (controls leak), 80-95% is medium (long tail), ≥95% silent.
func mfaAdoptionSeverity(pct float64) Severity {
	switch {
	case pct < 50:
		return SeverityCritical
	case pct < 80:
		return SeverityHigh
	case pct < 95:
		return SeverityMedium
	default:
		return ""
	}
}

func checkAdminMFACoverage(_ context.Context, d *Data) ([]Finding, error) {
	if d.Admins == nil {
		return nil, fmt.Errorf("admins fetch unavailable")
	}
	total, mfa := 0, 0
	for _, raw := range d.Admins {
		var a struct {
			EnableMultiFactor bool `json:"enableMultiFactor"`
			TotpEnrolled      bool `json:"totpEnrolled"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			continue
		}
		total++
		if a.EnableMultiFactor && a.TotpEnrolled {
			mfa++
		}
	}
	if total == 0 || mfa == total {
		return nil, nil
	}
	pct := float64(mfa) / float64(total) * 100
	return []Finding{{
		CheckID:         "admin-mfa-coverage",
		Title:           "Admin MFA coverage below 100%",
		Category:        CategoryCompliance,
		Severity:        SeverityCritical,
		ResourceRef:     "org",
		RemediationHint: "Every admin must have MFA — admin accounts are the highest-value targets in the directory. See findings from `admins-without-mfa` for the specific accounts.",
		Detail:          fmt.Sprintf("%d of %d admins have MFA enabled (%.1f%%). Target: 100%%.", mfa, total, pct),
	}}, nil
}

func checkPasswordAge(_ context.Context, d *Data) ([]Finding, error) {
	if d.Users == nil {
		return nil, fmt.Errorf("users fetch unavailable")
	}
	cutoff := d.Now.Add(-passwordAgeThreshold)
	var stale int
	for _, raw := range d.Users {
		var u struct {
			Activated     bool   `json:"activated"`
			Suspended     bool   `json:"suspended"`
			AccountLocked bool   `json:"account_locked"`
			PasswordDate  string `json:"password_date"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			continue
		}
		if !u.Activated || u.Suspended || u.AccountLocked || u.PasswordDate == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, u.PasswordDate)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			stale++
		}
	}
	if stale == 0 {
		return nil, nil
	}
	return []Finding{{
		CheckID:         "password-age",
		Title:           "Users with passwords older than 90 days",
		Category:        CategoryCompliance,
		Severity:        SeverityMedium,
		ResourceRef:     "org",
		RemediationHint: "Rotate stale passwords via an org-wide password policy or targeted `jc users reset-password` runs. If your compliance framework has dropped mandatory rotation (NIST SP 800-63B), this check can be filtered out via --severity high.",
		Detail:          fmt.Sprintf("%d active users have a password_date older than 90 days.", stale),
	}}, nil
}

func checkFDECoverage(_ context.Context, d *Data) ([]Finding, error) {
	if d.Devices == nil {
		return nil, fmt.Errorf("devices fetch unavailable")
	}
	var total, encrypted int
	for _, raw := range d.Devices {
		var dev struct {
			OS  string `json:"os"`
			FDE struct {
				Active bool `json:"active"`
			} `json:"fde"`
		}
		if err := json.Unmarshal(raw, &dev); err != nil {
			continue
		}
		// FDE is only meaningful on macOS/Windows. Linux/iOS/etc.
		// don't expose a comparable FDE state via this API.
		if dev.OS != "Mac OS X" && dev.OS != "Windows" {
			continue
		}
		total++
		if dev.FDE.Active {
			encrypted++
		}
	}
	if total == 0 || encrypted == total {
		return nil, nil
	}
	pct := float64(encrypted) / float64(total) * 100
	sev := fdeCoverageSeverity(pct)
	return []Finding{{
		CheckID:         "fde-coverage",
		Title:           "Full-disk encryption coverage below target",
		Category:        CategoryCompliance,
		Severity:        sev,
		ResourceRef:     "org",
		RemediationHint: "Push the JumpCloud FDE policy to unencrypted macOS/Windows devices. FileVault/BitLocker keys are escrowed back to JumpCloud for recovery.",
		Detail:          fmt.Sprintf("%d of %d managed macOS/Windows devices have FDE active (%.1f%%).", encrypted, total, pct),
	}}, nil
}

func fdeCoverageSeverity(pct float64) Severity {
	switch {
	case pct < 50:
		return SeverityCritical
	case pct < 90:
		return SeverityHigh
	default:
		return SeverityMedium
	}
}

// ─── hygiene ───────────────────────────────────────────────────────

func checkStaleDevices(_ context.Context, d *Data) ([]Finding, error) {
	if d.Devices == nil {
		return nil, fmt.Errorf("devices fetch unavailable")
	}
	cutoff := d.Now.Add(-staleDeviceThreshold)
	var findings []Finding
	for _, raw := range d.Devices {
		var dev struct {
			ID          string `json:"_id"`
			Hostname    string `json:"hostname"`
			DisplayName string `json:"displayName"`
			OS          string `json:"os"`
			LastContact string `json:"lastContact"`
		}
		if err := json.Unmarshal(raw, &dev); err != nil {
			continue
		}
		if dev.LastContact == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, dev.LastContact)
		if err != nil {
			continue
		}
		if !t.Before(cutoff) {
			continue
		}
		name := dev.Hostname
		if name == "" {
			name = dev.DisplayName
		}
		findings = append(findings, Finding{
			CheckID:         "stale-devices",
			Title:           "Device has not checked in for 30+ days",
			Category:        CategoryHygiene,
			Severity:        SeverityMedium,
			ResourceRef:     "device:" + name,
			RemediationHint: "Either the device is decommissioned (delete via `jc devices delete`) or the agent has crashed (investigate via `jc devices get`). Stale devices count toward your license while contributing no telemetry.",
			Detail:          fmt.Sprintf("Device %s (%s) last contact: %s.", name, dev.OS, dev.LastContact),
		})
	}
	return findings, nil
}

func checkDisabledAuthPolicies(_ context.Context, d *Data) ([]Finding, error) {
	if d.AuthPolicies == nil {
		return nil, fmt.Errorf("auth policies fetch unavailable")
	}
	var findings []Finding
	for _, raw := range d.AuthPolicies {
		var p struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Disabled bool   `json:"disabled"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		if !p.Disabled {
			continue
		}
		findings = append(findings, Finding{
			CheckID:         "auth-policies-disabled",
			Title:           "Disabled auth policy",
			Category:        CategoryHygiene,
			Severity:        SeverityLow,
			ResourceRef:     "auth-policy:" + p.Name,
			RemediationHint: "Either re-enable (`jc auth-policies enable " + p.Name + "`) or delete (`jc auth-policies delete " + p.Name + "`). A disabled policy is dead code — it's silent until a future operator wonders why traffic isn't being gated.",
			Detail:          "Auth policy " + p.Name + " is disabled.",
		})
	}
	return findings, nil
}

// ─── identity ──────────────────────────────────────────────────────

func checkRecentAdmins(_ context.Context, d *Data) ([]Finding, error) {
	if d.Admins == nil {
		return nil, fmt.Errorf("admins fetch unavailable")
	}
	cutoff := d.Now.Add(-recentAdminWindow)
	var findings []Finding
	for _, raw := range d.Admins {
		var a struct {
			ID      string `json:"_id"`
			Email   string `json:"email"`
			Created string `json:"created"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			continue
		}
		if a.Created == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, a.Created)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			continue
		}
		findings = append(findings, Finding{
			CheckID:         "recently-created-admins",
			Title:           "Admin created in the last 14 days",
			Category:        CategoryIdentity,
			Severity:        SeverityInfo,
			ResourceRef:     "admin:" + a.Email,
			RemediationHint: "Confirm this admin account was created intentionally — newly-created admin accounts are a common post-compromise persistence mechanism. Cross-check against your IAM tickets / Slack approvals.",
			Detail:          fmt.Sprintf("Admin %s was created at %s.", a.Email, a.Created),
		})
	}
	return findings, nil
}
