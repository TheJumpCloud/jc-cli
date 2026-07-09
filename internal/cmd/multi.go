package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/config"
)

// jc multi (KLA-441) — fan an inner jc command out across multiple
// configured profiles and merge the results. Built for MSPs and
// multi-org customers.
//
// Fan-out is SUBPROCESS-based, one child per profile, each invoked as
// `jc <inner...> --org <profile>`. In-process re-invocation was
// rejected deliberately: every command resolves auth, profile, and
// output options through the process-global viper singleton
// (config.OverrideActiveProfile IS viper.Set), so N concurrent inner
// commands with different --org values would race on active_profile —
// with cross-tenant credential resolution as the failure mode. The
// recipe runner is sequential for exactly this reason; subprocesses
// give each profile an isolated process and config view.

// multiMaxConcurrency caps the default parallelism. Fanning out to
// dozens of orgs at once mostly trips per-org rate limits; 8 keeps
// the common 2-15-profile MSP case fully parallel.
const multiMaxConcurrency = 8

// multiProfileResult is one profile's outcome in the merged report.
type multiProfileResult struct {
	Profile string `json:"profile"`
	// Status is "ok", "failed", or "skipped".
	Status string `json:"status"`
	// Data carries the inner command's parsed JSON output (ok only).
	Data json.RawMessage `json:"data,omitempty"`
	// Error carries the failure detail or skip reason.
	Error string `json:"error,omitempty"`
}

// multiRunInner executes the inner command for one profile and
// returns its stdout/stderr. Overridable for tests — the default
// re-invokes this same binary as a subprocess.
var multiRunInner = func(ctx context.Context, profile string, innerArgs []string) (stdout, stderr []byte, err error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, fmt.Errorf("locating jc binary: %w", err)
	}
	args := append(append([]string{}, innerArgs...), "--org", profile)
	cmd := exec.CommandContext(ctx, exe, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

func newMultiCmd() *cobra.Command {
	var (
		profilesCSV  string
		filterGlob   string
		concurrency  int
		allowDestruc bool
	)

	cmd := &cobra.Command{
		Use:   "multi [--profiles a,b,c | --filter 'glob'] -- <jc command...>",
		Short: "Run a jc command across multiple profiles and merge the results",
		Long: `Fan one jc command out across multiple configured profiles in
parallel and merge the output — the MSP view of a fleet of orgs.

Each profile runs the inner command as its own jc subprocess with
` + "`--org <profile>`" + `, so auth, caching, and output resolution stay fully
isolated per org (this is also why the inner command must not carry
its own --org flag).

Profile selection (exactly one):
  --profiles a,b,c     explicit list
  --filter 'prod-*'    shell-style glob over configured profile names
                       (use --filter '*' for every profile)

Output merging by the inner command's format:
  JSON (default)   [{profile, status, data|error}, ...] — one entry
                   per profile, machine-readable, failures included
  --ids            each ID line prefixed with <profile>/
  table / other    per-profile sections, concatenated

Safety:
  - Destructive inner commands (per their jc:class annotation — users
    delete, devices erase, recipe run, ...) are refused without
    --allow-destructive.
  - Profiles whose auth_profile_role is read_only are skipped for
    mutating/destructive inner commands and reported as skipped.
  - Per-profile failures don't abort the rest; jc multi exits non-zero
    if any profile failed.`,
		Example: `  # Count users in every prod org
  jc multi --filter 'prod-*' -- users list --ids | wc -l

  # Aggregate JSON across three orgs
  jc multi --profiles acme,globex,initech -- policies list

  # Locked users everywhere (IDs prefixed with the profile)
  jc multi --filter '*' -- users list --filter 'state:eq:SUSPENDED' --ids

  # Destructive fan-out needs the extra gate
  jc multi --profiles staging-a,staging-b --allow-destructive -- users delete bot-account --force`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := selectMultiProfiles(profilesCSV, filterGlob)
			if err != nil {
				return err
			}
			if err := validateInnerArgs(args); err != nil {
				return err
			}

			// Classify the inner command via the jc:class annotation
			// map — the same source the MCP filter roadmap uses. The
			// worst-case-capability convention applies: an unknown
			// leaf is treated as destructive, not waved through.
			class, leafPath := classifyInner(args)
			if class == ClassDestructive && !allowDestruc {
				return fmt.Errorf(
					"%q is a destructive command (jc:class); fanning it out across %d profiles requires --allow-destructive",
					leafPath, len(profiles))
			}

			// Read-only profiles can't run write commands — skip them
			// up front and say so, rather than letting N children fail
			// with permission errors.
			var runnable []string
			var skipped []multiProfileResult
			for _, p := range profiles {
				if config.ProfileRole(p) == "read_only" && class != ClassReadOnly && class != ClassInternal {
					skipped = append(skipped, multiProfileResult{
						Profile: p,
						Status:  "skipped",
						Error:   fmt.Sprintf("profile role is read_only; %q is %s", leafPath, class),
					})
					continue
				}
				runnable = append(runnable, p)
			}

			if concurrency <= 0 {
				concurrency = len(runnable)
			}
			if concurrency > multiMaxConcurrency {
				concurrency = multiMaxConcurrency
			}

			results := runMultiFanOut(cmd.Context(), runnable, args, concurrency)
			results = append(results, skipped...)
			sort.Slice(results, func(i, j int) bool { return results[i].Profile < results[j].Profile })

			if err := renderMultiResults(cmd, args, results); err != nil {
				return err
			}
			failed := 0
			for _, r := range results {
				if r.Status == "failed" {
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d profiles failed", failed, len(results))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&profilesCSV, "profiles", "", "Comma-separated profile names to fan out to")
	cmd.Flags().StringVar(&filterGlob, "filter", "", "Shell-style glob over configured profile names (e.g. 'prod-*', '*')")
	cmd.Flags().IntVar(&concurrency, "concurrency", 0,
		fmt.Sprintf("Max parallel profiles (default: number of profiles, capped at %d)", multiMaxConcurrency))
	cmd.Flags().BoolVar(&allowDestruc, "allow-destructive", false,
		"Permit fanning out a destructive inner command (per its jc:class annotation)")

	return cmd
}

// selectMultiProfiles resolves the target profile list from exactly
// one of --profiles / --filter.
func selectMultiProfiles(profilesCSV, filterGlob string) ([]string, error) {
	if (profilesCSV == "") == (filterGlob == "") {
		return nil, fmt.Errorf("select profiles with exactly one of --profiles or --filter (use --filter '*' for all)")
	}
	configured := config.ProfileNames()

	if profilesCSV != "" {
		var out []string
		var missing []string
		for _, p := range strings.Split(profilesCSV, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !config.ProfileExists(p) {
				missing = append(missing, p)
				continue
			}
			out = append(out, p)
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("unknown profile(s): %s (configured: %s)",
				strings.Join(missing, ", "), strings.Join(configured, ", "))
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("--profiles selected nothing")
		}
		return out, nil
	}

	var out []string
	for _, p := range configured {
		ok, err := path.Match(filterGlob, p)
		if err != nil {
			return nil, fmt.Errorf("invalid --filter glob %q: %w", filterGlob, err)
		}
		if ok {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--filter %q matched none of the configured profiles (%s)",
			filterGlob, strings.Join(configured, ", "))
	}
	return out, nil
}

// validateInnerArgs rejects inner flags that conflict with the
// fan-out contract.
func validateInnerArgs(args []string) error {
	if args[0] == "multi" {
		return fmt.Errorf("jc multi cannot fan out jc multi")
	}
	for _, a := range args {
		if a == "--org" || strings.HasPrefix(a, "--org=") {
			return fmt.Errorf("the inner command must not set --org — jc multi supplies it per profile")
		}
	}
	return nil
}

// classifyInner resolves the inner command's jc:class via the central
// classification map. Unknown commands classify as destructive — the
// worst-case-capability convention — so a typo'd or brand-new leaf
// can't slip past the gate.
func classifyInner(args []string) (class string, leafPath string) {
	root := NewRootCmd()
	leaf, _, err := root.Find(args)
	if err != nil || leaf == root {
		return ClassDestructive, strings.Join(args, " ")
	}
	leafPath = leaf.CommandPath()
	if c, ok := commandClass[leafPath]; ok {
		return c, leafPath
	}
	return ClassDestructive, leafPath
}

// runMultiFanOut executes the inner command across profiles with
// bounded concurrency. Results land in a pre-sized slice — no shared
// mutable state beyond it (the groups.go enrichment pattern).
func runMultiFanOut(ctx context.Context, profiles []string, innerArgs []string, concurrency int) []multiProfileResult {
	results := make([]multiProfileResult, len(profiles))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, p := range profiles {
		wg.Add(1)
		go func(idx int, profile string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			stdout, stderr, err := multiRunInner(ctx, profile, innerArgs)
			if err != nil {
				detail := strings.TrimSpace(string(stderr))
				if detail == "" {
					detail = strings.TrimSpace(string(stdout))
				}
				if detail == "" {
					detail = err.Error()
				}
				results[idx] = multiProfileResult{Profile: profile, Status: "failed", Error: detail}
				return
			}
			results[idx] = multiProfileResult{Profile: profile, Status: "ok", Data: normalizeInnerOutput(stdout)}
		}(i, p)
	}
	wg.Wait()
	return results
}

// normalizeInnerOutput keeps valid JSON verbatim and wraps anything
// else (plain-text confirmations, table output) as a JSON string so
// the aggregate envelope always marshals.
func normalizeInnerOutput(stdout []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return json.RawMessage("null")
	}
	if json.Valid(trimmed) {
		return json.RawMessage(trimmed)
	}
	quoted, _ := json.Marshal(string(trimmed))
	return json.RawMessage(quoted)
}

// renderMultiResults merges per the inner command's output format:
// --ids lines get a <profile>/ prefix, table/human formats get
// per-profile sections, everything else aggregates as JSON.
func renderMultiResults(cmd *cobra.Command, innerArgs []string, results []multiProfileResult) error {
	w := cmd.OutOrStdout()
	switch innerOutputKind(innerArgs) {
	case "ids":
		for _, r := range results {
			if r.Status != "ok" {
				fmt.Fprintf(cmd.ErrOrStderr(), "# %s: %s: %s\n", r.Profile, r.Status, r.Error)
				continue
			}
			// r.Data is a JSON string wrapping the line-oriented IDs.
			var text string
			if err := json.Unmarshal(r.Data, &text); err != nil {
				// Valid JSON output despite --ids (e.g. empty null).
				text = strings.Trim(string(r.Data), `"`)
			}
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fmt.Fprintf(w, "%s/%s\n", r.Profile, line)
			}
		}
		return nil

	case "sections":
		for _, r := range results {
			fmt.Fprintf(w, "── %s ──\n", r.Profile)
			if r.Status != "ok" {
				fmt.Fprintf(w, "(%s) %s\n\n", r.Status, r.Error)
				continue
			}
			var text string
			if err := json.Unmarshal(r.Data, &text); err != nil {
				text = string(r.Data)
			}
			fmt.Fprintln(w, text)
			fmt.Fprintln(w)
		}
		return nil

	default: // JSON aggregate — always JSON, regardless of outer flags:
		// the envelope is jc multi's own machine-readable report.
		payload, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(payload))
		return err
	}
}

// innerOutputKind inspects the inner args for output-shaping flags.
func innerOutputKind(args []string) string {
	for i, a := range args {
		switch {
		case a == "--ids":
			return "ids"
		case a == "-t" || a == "--table":
			return "sections"
		case a == "-o" || a == "--output":
			if i+1 < len(args) && args[i+1] != "json" {
				return "sections"
			}
		case strings.HasPrefix(a, "--output="):
			if strings.TrimPrefix(a, "--output=") != "json" {
				return "sections"
			}
		}
	}
	return "json"
}
