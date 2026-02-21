# Integration Test Script — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a bash script that exercises every `jc` CLI resource type and feature against a live JumpCloud organization.

**Architecture:** Single bash script with 5 phases (preflight → mutable lifecycle → recipes → read-only probes → utilities). Trap-based cleanup ensures test resources are always deleted. Each test step is a function call to a `run_test` helper that tracks pass/fail counts and prints colored output.

**Tech Stack:** Bash, jc CLI

**Design doc:** `docs/plans/2026-02-21-integration-test-script-design.md`

---

### Task 1: Script Skeleton with Helpers and Preflight

**Files:**
- Create: `scripts/integration-test.sh`

**Step 1: Create the script with all helper functions and Phase 1**

The script skeleton includes:
- Color constants, pass/fail counters
- `run_test "label" "command" "assertion"` helper — runs command, checks assertion, prints [PASS]/[FAIL]
- `assert_exit_ok` — just checks exit code 0
- `assert_output_contains` — checks stdout contains a string
- `assert_output_line_count` — checks stdout line count
- `assert_exit_fails` — checks exit code != 0
- `phase` — section header printer
- `cleanup` function (initially empty, registered with `trap`)
- Phase 1: Preflight (version, auth, mcp tools, org)

```bash
#!/usr/bin/env bash
#
# jc CLI integration test — exercises the full CLI surface against a live
# JumpCloud organization. Requires authenticated jc on PATH.
#
# Usage:
#   ./scripts/integration-test.sh          # Run all phases
#   ./scripts/integration-test.sh --skip-mutable   # Skip create/delete phases
#
set -euo pipefail

# ── Colors ──────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
RESET='\033[0m'

# ── Counters ────────────────────────────────────────────────────────────
PASS=0
FAIL=0
SKIP=0
ERRORS=()

# ── Options ─────────────────────────────────────────────────────────────
SKIP_MUTABLE=false
for arg in "$@"; do
  case "$arg" in
    --skip-mutable) SKIP_MUTABLE=true ;;
    --help|-h) echo "Usage: $0 [--skip-mutable]"; exit 0 ;;
  esac
done

# ── Timestamp for unique resource names ─────────────────────────────────
TS=$(date +%s)

# ── Resource IDs for cleanup ────────────────────────────────────────────
TEST_USER_ID=""
TEST_GROUP_ID=""
RECIPE_GROUP_ID=""

# ── Helpers ─────────────────────────────────────────────────────────────

phase() {
  echo ""
  echo -e "${BOLD}${CYAN}Phase $1: $2${RESET}"
  echo -e "${DIM}$(printf '─%.0s' {1..50})${RESET}"
}

pass() {
  PASS=$((PASS + 1))
  echo -e "  ${GREEN}[PASS]${RESET} $1"
}

fail() {
  FAIL=$((FAIL + 1))
  ERRORS+=("$1: $2")
  echo -e "  ${RED}[FAIL]${RESET} $1 ${DIM}— $2${RESET}"
}

skip() {
  SKIP=$((SKIP + 1))
  echo -e "  ${YELLOW}[SKIP]${RESET} $1"
}

# Run a command and check exit code is 0.
run_ok() {
  local label="$1"
  shift
  local output
  if output=$("$@" 2>&1); then
    pass "$label"
    echo "$output"  # return via stdout for capture
  else
    fail "$label" "exit code $?"
    echo "$output"
  fi
}

# Run a command, check exit 0, and verify output contains a string.
run_contains() {
  local label="$1"
  local needle="$2"
  shift 2
  local output
  if output=$("$@" 2>&1); then
    if echo "$output" | grep -q "$needle"; then
      pass "$label"
    else
      fail "$label" "output missing '$needle'"
    fi
  else
    fail "$label" "exit code $?"
  fi
  echo "$output"
}

# Run a command and check that it fails (non-zero exit).
run_fails() {
  local label="$1"
  shift
  local output
  if output=$("$@" 2>&1); then
    fail "$label" "expected failure but got exit 0"
  else
    pass "$label"
  fi
}

# Run a command, check exit 0, and verify stdout line count.
run_line_count() {
  local label="$1"
  local expected="$2"
  shift 2
  local output
  if output=$("$@" 2>&1); then
    local count
    count=$(echo "$output" | wc -l | tr -d ' ')
    if [ "$count" -ge "$expected" ]; then
      pass "$label"
    else
      fail "$label" "expected >= $expected lines, got $count"
    fi
  else
    fail "$label" "exit code $?"
  fi
}

# ── Cleanup ─────────────────────────────────────────────────────────────

cleanup() {
  echo ""
  echo -e "${BOLD}Cleanup${RESET}"
  echo -e "${DIM}$(printf '─%.0s' {1..50})${RESET}"
  local cleaned=0

  if [ -n "$TEST_USER_ID" ]; then
    jc users delete "$TEST_USER_ID" --force 2>/dev/null && cleaned=$((cleaned + 1)) || true
    echo -e "  ${DIM}deleted test user $TEST_USER_ID${RESET}"
  fi

  if [ -n "$TEST_GROUP_ID" ]; then
    jc groups user delete "$TEST_GROUP_ID" --force 2>/dev/null && cleaned=$((cleaned + 1)) || true
    echo -e "  ${DIM}deleted test group $TEST_GROUP_ID${RESET}"
  fi

  if [ -n "$RECIPE_GROUP_ID" ]; then
    jc groups user delete "$RECIPE_GROUP_ID" --force 2>/dev/null && cleaned=$((cleaned + 1)) || true
    echo -e "  ${DIM}deleted recipe group $RECIPE_GROUP_ID${RESET}"
  fi

  echo -e "  ${DIM}cleaned up $cleaned resources${RESET}"
}

trap cleanup INT TERM EXIT

# ── Banner ──────────────────────────────────────────────────────────────

VERSION=$(jc --version 2>/dev/null || echo "unknown")
echo -e "${BOLD}jc integration test — $VERSION${RESET}"
echo -e "${BOLD}$(printf '═%.0s' {1..50})${RESET}"

# ═══════════════════════════════════════════════════════════════════════
# Phase 1: Preflight
# ═══════════════════════════════════════════════════════════════════════

phase 1 "Preflight"

# Version
if [ "$VERSION" != "unknown" ]; then
  pass "version check ($VERSION)"
else
  fail "version check" "jc --version returned nothing"
fi

# Auth
if jc auth status --quiet 2>/dev/null; then
  pass "auth status"
else
  fail "auth status" "not authenticated — run 'jc auth login' first"
  echo -e "\n${RED}Cannot continue without authentication. Exiting.${RESET}"
  exit 1
fi

# MCP tools count
MCP_COUNT=$(jc mcp tools 2>/dev/null | wc -l | tr -d ' ')
if [ "$MCP_COUNT" -eq 158 ]; then
  pass "mcp tools count ($MCP_COUNT)"
else
  fail "mcp tools count" "expected 158, got $MCP_COUNT"
fi

# Org list
run_ok "org list" jc org list --limit 1 > /dev/null
```

**Step 2: Make it executable and verify it runs (preflight only)**

```bash
mkdir -p scripts
# (file already created above)
chmod +x scripts/integration-test.sh
./scripts/integration-test.sh
```

Expected: Phase 1 passes (assuming authenticated), cleanup runs (nothing to clean), exits.

**Step 3: Commit**

```bash
git add scripts/integration-test.sh
git commit -m "feat: add integration test script skeleton with Phase 1 preflight"
```

---

### Task 2: Phase 2 — Mutable Lifecycle (Users + Groups + Graph)

**Files:**
- Modify: `scripts/integration-test.sh`

**Step 1: Add Phase 2 after Phase 1**

Append to the script, before the final summary:

```bash
# ═══════════════════════════════════════════════════════════════════════
# Phase 2: Mutable Lifecycle
# ═══════════════════════════════════════════════════════════════════════

if $SKIP_MUTABLE; then
  phase 2 "Mutable Lifecycle (SKIPPED)"
  skip "mutable lifecycle (--skip-mutable)"
else
  phase 2 "Mutable Lifecycle"

  TEST_USERNAME="jctest-$TS"
  TEST_EMAIL="jctest-${TS}@test.jumpcloud.invalid"
  TEST_GROUP_NAME="jctest-group-$TS"

  # Create user
  TEST_USER_ID=$(jc users create \
    --username "$TEST_USERNAME" \
    --email "$TEST_EMAIL" \
    --firstname "Test" \
    --lastname "User" \
    --ids 2>/dev/null || true)
  if [ -n "$TEST_USER_ID" ]; then
    pass "users create ($TEST_USERNAME)"
  else
    fail "users create" "no ID returned"
  fi

  # Get user
  run_contains "users get" "$TEST_USERNAME" jc users get "$TEST_USER_ID" > /dev/null

  # Search user
  run_contains "users search" "$TEST_USERNAME" jc users search "$TEST_USERNAME" > /dev/null

  # Update user
  run_ok "users update (department)" jc users update "$TEST_USER_ID" --department "Integration Test" > /dev/null

  # Lock / unlock
  run_ok "users lock" jc users lock "$TEST_USER_ID" > /dev/null
  run_ok "users unlock" jc users unlock "$TEST_USER_ID" > /dev/null

  # Create group
  TEST_GROUP_ID=$(jc groups user create --name "$TEST_GROUP_NAME" --ids 2>/dev/null || true)
  if [ -n "$TEST_GROUP_ID" ]; then
    pass "groups user create ($TEST_GROUP_NAME)"
  else
    fail "groups user create" "no ID returned"
  fi

  # Add member
  run_ok "groups add-member" jc groups add-member "$TEST_GROUP_ID" --user "$TEST_USER_ID" > /dev/null

  # Graph traverse: user → user_group
  run_contains "graph traverse (user→user_group)" "$TEST_GROUP_ID" \
    jc graph traverse --from "user:$TEST_USER_ID" --to user_group > /dev/null

  # Remove member
  run_ok "groups remove-member" jc groups remove-member "$TEST_GROUP_ID" --user "$TEST_USER_ID" > /dev/null
fi
```

The cleanup function already handles deleting `TEST_USER_ID` and `TEST_GROUP_ID`.

**Step 2: Run the script and verify Phase 2**

```bash
./scripts/integration-test.sh
```

Expected: Phase 1 + Phase 2 pass, cleanup deletes the test user and group.

**Step 3: Commit**

```bash
git add scripts/integration-test.sh
git commit -m "feat: add Phase 2 mutable lifecycle (users, groups, graph)"
```

---

### Task 3: Phase 3 — Recipe Engine

**Files:**
- Modify: `scripts/integration-test.sh`

**Step 1: Add Phase 3 after Phase 2**

```bash
# ═══════════════════════════════════════════════════════════════════════
# Phase 3: Recipe Engine
# ═══════════════════════════════════════════════════════════════════════

if $SKIP_MUTABLE; then
  phase 3 "Recipe Engine (SKIPPED)"
  skip "recipe engine (--skip-mutable)"
else
  phase 3 "Recipe Engine"

  # Recipe list and show
  run_contains "recipe list" "onboard-user" jc recipe list -t > /dev/null
  run_contains "recipe show" "onboard-user" jc recipe show onboard-user > /dev/null

  RECIPE_USERNAME="jctest-recipe-$TS"
  RECIPE_EMAIL="jctest-recipe-${TS}@test.jumpcloud.invalid"
  RECIPE_GROUP_NAME="jctest-recipe-group-$TS"

  # Create group for recipe to use
  RECIPE_GROUP_ID=$(jc groups user create --name "$RECIPE_GROUP_NAME" --ids 2>/dev/null || true)
  if [ -n "$RECIPE_GROUP_ID" ]; then
    pass "recipe group create ($RECIPE_GROUP_NAME)"
  else
    fail "recipe group create" "no ID returned"
  fi

  # Onboard — plan mode (should exit 10)
  if jc recipe run onboard-user \
    --param "username=$RECIPE_USERNAME" \
    --param "email=$RECIPE_EMAIL" \
    --param "firstname=Test" \
    --param "lastname=Recipe" \
    --param "group=$RECIPE_GROUP_NAME" \
    --plan 2>/dev/null; then
    fail "onboard-user (plan)" "expected exit 10 but got 0"
  else
    local_exit=$?
    if [ "$local_exit" -eq 10 ]; then
      pass "onboard-user (plan mode, exit 10)"
    else
      fail "onboard-user (plan)" "expected exit 10, got $local_exit"
    fi
  fi

  # Onboard — execute
  RECIPE_OUTPUT=$(jc recipe run onboard-user \
    --param "username=$RECIPE_USERNAME" \
    --param "email=$RECIPE_EMAIL" \
    --param "firstname=Test" \
    --param "lastname=Recipe" \
    --param "group=$RECIPE_GROUP_NAME" \
    --force 2>&1 || true)
  if echo "$RECIPE_OUTPUT" | grep -qi "success\|onboarded"; then
    pass "onboard-user (execute)"
  else
    fail "onboard-user (execute)" "no success message"
  fi

  # Verify user exists
  if jc users get "$RECIPE_USERNAME" > /dev/null 2>&1; then
    pass "recipe user exists"
  else
    fail "recipe user exists" "user not found after onboard"
  fi

  # Offboard with delete
  OFFBOARD_OUTPUT=$(jc recipe run offboard-user \
    --param "user=$RECIPE_USERNAME" \
    --param "delete_user=true" \
    --force 2>&1 || true)
  if echo "$OFFBOARD_OUTPUT" | grep -qi "success\|offboarded"; then
    pass "offboard-user (execute)"
  else
    fail "offboard-user (execute)" "no success message"
  fi

  # Verify user gone
  run_fails "recipe user deleted" jc users get "$RECIPE_USERNAME" > /dev/null
fi
```

**Step 2: Run and verify**

```bash
./scripts/integration-test.sh
```

Expected: Phase 3 creates a user via recipe, verifies, offboards with deletion, verifies gone.

**Step 3: Commit**

```bash
git add scripts/integration-test.sh
git commit -m "feat: add Phase 3 recipe engine tests (onboard/offboard)"
```

---

### Task 4: Phase 4 — Read-Only Probes (All Resources + Output Formats)

**Files:**
- Modify: `scripts/integration-test.sh`

**Step 1: Add Phase 4**

This phase just runs `list` (or equivalent) for every resource type with different output formats. Each call just needs exit code 0.

```bash
# ═══════════════════════════════════════════════════════════════════════
# Phase 4: Read-Only Probes
# ═══════════════════════════════════════════════════════════════════════

phase 4 "Read-Only Probes"

# Core resources — spread across all 6 output formats
run_ok "users list (json)"              jc users list --limit 3                     > /dev/null
run_ok "devices list (table)"           jc devices list --limit 3 -t               > /dev/null
run_ok "groups user list (csv)"         jc groups user list --limit 3 --output csv  > /dev/null
run_ok "groups device list (yaml)"      jc groups device list --limit 3 --output yaml > /dev/null
run_ok "commands list (json)"           jc commands list --limit 3                  > /dev/null
run_ok "policies list (table)"          jc policies list --limit 3 -t              > /dev/null
run_ok "policy-groups list (ndjson)"    jc policy-groups list --limit 3 --output ndjson > /dev/null
run_ok "policy-templates list (human)"  jc policy-templates list --limit 3 --output human > /dev/null
run_ok "apps list (table)"             jc apps list --limit 3 -t                   > /dev/null
run_ok "app-templates list (json)"     jc app-templates list --limit 3             > /dev/null
run_ok "admins list (csv)"             jc admins list --limit 3 --output csv       > /dev/null
run_ok "auth-policies list (yaml)"     jc auth-policies list --limit 3 --output yaml > /dev/null
run_ok "iplists list (ndjson)"         jc iplists list --limit 3 --output ndjson   > /dev/null
run_ok "software list (ndjson)"        jc software list --limit 3 --output ndjson  > /dev/null
run_ok "ldap list (human)"             jc ldap list --output human                 > /dev/null
run_ok "ad list (json)"                jc ad list --limit 3                        > /dev/null
run_ok "radius list (yaml)"            jc radius list --output yaml                > /dev/null
run_ok "apple-mdm list (table)"        jc apple-mdm list --limit 3 -t             > /dev/null
run_ok "gsuite list (json)"            jc gsuite list --limit 3                    > /dev/null
run_ok "office365 list (json)"         jc office365 list --limit 3                 > /dev/null
run_ok "duo list (csv)"                jc duo list --limit 3 --output csv          > /dev/null
run_ok "custom-emails templates (table)" jc custom-emails templates -t             > /dev/null
run_ok "user-states list (json)"       jc user-states list --limit 3               > /dev/null
run_ok "org list (yaml)"               jc org list --output yaml                   > /dev/null

# Insights
run_ok "insights query (json)"         jc insights query --service all --last 1h --limit 5 > /dev/null
run_ok "insights count"                jc insights count --service all --last 1h   > /dev/null

# System Insights
run_ok "system-insights tables"        jc system-insights tables                   > /dev/null
run_ok "system-insights os_version"    jc system-insights os_version --limit 3 -t  > /dev/null

# Flag combinations
run_ok "fields selection"              jc users list --limit 2 --fields username,email -t > /dev/null
run_ok "fields exclusion"              jc users list --limit 2 --exclude password -t > /dev/null
run_ok "all fields"                    jc users list --limit 2 --all -t            > /dev/null
run_ok "ids mode"                      jc users list --limit 2 --ids               > /dev/null
run_ok "jmespath query"                jc devices list --limit 2 --query "[].hostname" > /dev/null
```

**Step 2: Run and verify**

```bash
./scripts/integration-test.sh --skip-mutable   # fast: just preflight + probes
```

Expected: All probes pass (some resources may be empty — that's OK, exit 0 is the check).

**Step 3: Commit**

```bash
git add scripts/integration-test.sh
git commit -m "feat: add Phase 4 read-only probes (28 resources, all output formats)"
```

---

### Task 5: Phase 5 — Utilities + Summary

**Files:**
- Modify: `scripts/integration-test.sh`

**Step 1: Add Phase 5 and the final summary**

```bash
# ═══════════════════════════════════════════════════════════════════════
# Phase 5: Utilities
# ═══════════════════════════════════════════════════════════════════════

phase 5 "Utilities"

run_contains "explain" "DELETE" jc explain users delete testuser > /dev/null
run_ok "config view"           jc config view                    > /dev/null
run_contains "schema resources" "users" jc schema resources      > /dev/null
run_ok "schema commands"       jc schema commands                > /dev/null
run_ok "completion bash"       jc completion bash                > /dev/null

# ═══════════════════════════════════════════════════════════════════════
# Summary
# ═══════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}$(printf '═%.0s' {1..50})${RESET}"
TOTAL=$((PASS + FAIL + SKIP))
echo -e "${BOLD}Results: ${GREEN}$PASS passed${RESET}, ${RED}$FAIL failed${RESET}, ${YELLOW}$SKIP skipped${RESET} ${DIM}($TOTAL total)${RESET}"

if [ ${#ERRORS[@]} -gt 0 ]; then
  echo ""
  echo -e "${RED}Failures:${RESET}"
  for err in "${ERRORS[@]}"; do
    echo -e "  ${RED}•${RESET} $err"
  done
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo -e "${GREEN}${BOLD}All tests passed!${RESET}"
  exit 0
else
  echo -e "${RED}${BOLD}$FAIL test(s) failed.${RESET}"
  exit 1
fi
```

**Step 2: Run the complete script**

```bash
./scripts/integration-test.sh
```

Expected: All 5 phases run, cleanup succeeds, summary printed.

**Step 3: Commit**

```bash
git add scripts/integration-test.sh
git commit -m "feat: add Phase 5 utilities and summary, complete integration test script"
```

---

### Task 6: Add Makefile Target and README Entry

**Files:**
- Modify: `Makefile`
- Modify: `README.md`

**Step 1: Add Makefile target**

Add to Makefile after the existing targets:

```makefile
integration-test:
	@./scripts/integration-test.sh

integration-test-readonly:
	@./scripts/integration-test.sh --skip-mutable
```

**Step 2: Add to README Development section**

In the Development section, after `make install`, add:

```markdown
make integration-test           # Run full integration test (requires auth)
make integration-test-readonly  # Read-only probes only (no create/delete)
```

**Step 3: Commit**

```bash
git add Makefile README.md
git commit -m "docs: add integration-test Makefile targets and README entry"
```
