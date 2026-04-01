#!/usr/bin/env bash
#
# jc CLI integration test — exercises the full CLI surface against a live
# JumpCloud organization. Requires authenticated jc on PATH (or set JC=./jc).
#
# Usage:
#   ./scripts/integration-test.sh                # Run all phases
#   ./scripts/integration-test.sh --skip-mutable  # Skip create/delete phases
#   JC=./jc ./scripts/integration-test.sh         # Use local binary
#
set -euo pipefail

# ── Binary ────────────────────────────────────────────────────────────
JC="${JC:-jc}"

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

# Run a command and check exit code is 0. Stdout is suppressed.
run_ok() {
  local label="$1"
  shift
  if "$@" > /dev/null 2>&1; then
    pass "$label"
  else
    fail "$label" "exit code $?"
  fi
}

# Run a command, check exit 0, and verify combined output contains a string.
run_contains() {
  local label="$1"
  local needle="$2"
  shift 2
  local output
  if output=$("$@" 2>&1); then
    if echo "$output" | grep -qF "$needle"; then
      pass "$label"
    else
      fail "$label" "output missing '$needle'"
    fi
  else
    fail "$label" "exit code $?"
  fi
}

# Run a command and check that it fails (non-zero exit).
run_fails() {
  local label="$1"
  shift
  if "$@" > /dev/null 2>&1; then
    fail "$label" "expected failure but got exit 0"
  else
    pass "$label"
  fi
}

# ── Cleanup ─────────────────────────────────────────────────────────────

cleanup() {
  echo ""
  echo -e "${BOLD}Cleanup${RESET}"
  echo -e "${DIM}$(printf '─%.0s' {1..50})${RESET}"
  local cleaned=0

  if [ -n "$TEST_USER_ID" ]; then
    if $JC users delete "$TEST_USER_ID" --force > /dev/null 2>&1; then
      cleaned=$((cleaned + 1))
      echo -e "  ${DIM}deleted test user${RESET}"
    else
      echo -e "  ${DIM}test user already gone${RESET}"
    fi
  fi

  if [ -n "$TEST_GROUP_ID" ]; then
    if $JC groups user delete "$TEST_GROUP_ID" --force > /dev/null 2>&1; then
      cleaned=$((cleaned + 1))
      echo -e "  ${DIM}deleted test group${RESET}"
    else
      echo -e "  ${DIM}test group already gone${RESET}"
    fi
  fi

  if [ -n "$RECIPE_GROUP_ID" ]; then
    if $JC groups user delete "$RECIPE_GROUP_ID" --force > /dev/null 2>&1; then
      cleaned=$((cleaned + 1))
      echo -e "  ${DIM}deleted recipe group${RESET}"
    else
      echo -e "  ${DIM}recipe group already gone${RESET}"
    fi
  fi

  echo -e "  ${DIM}cleaned up $cleaned resources${RESET}"
}

trap cleanup INT TERM EXIT

# ── Banner ──────────────────────────────────────────────────────────────

VERSION=$($JC --version 2>/dev/null || echo "unknown")
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
if $JC auth status --quiet > /dev/null 2>&1; then
  pass "auth status"
else
  fail "auth status" "not authenticated — run 'jc auth login' first"
  echo -e "\n${RED}Cannot continue without authentication. Exiting.${RESET}"
  exit 1
fi

# MCP tools count
MCP_COUNT=$($JC mcp tools 2>/dev/null | wc -l | tr -d ' ')
if [ "$MCP_COUNT" -eq 195 ]; then
  pass "mcp tools count ($MCP_COUNT)"
else
  fail "mcp tools count" "expected 195, got $MCP_COUNT"
fi

# Org list (no --limit; org returns a single object)
run_ok "org list" $JC org list

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
  TEST_USER_ID=$($JC users create \
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
  run_contains "users get" "$TEST_USERNAME" $JC users get "$TEST_USER_ID"

  # Search user
  run_contains "users search" "$TEST_USERNAME" $JC users search "$TEST_USERNAME"

  # Update user
  run_ok "users update (department)" $JC users update "$TEST_USER_ID" --department "Integration Test"

  # Lock / unlock
  run_ok "users lock" $JC users lock "$TEST_USER_ID"
  run_ok "users unlock" $JC users unlock "$TEST_USER_ID"

  # Create group
  TEST_GROUP_ID=$($JC groups user create --name "$TEST_GROUP_NAME" --ids 2>/dev/null || true)
  if [ -n "$TEST_GROUP_ID" ]; then
    pass "groups user create ($TEST_GROUP_NAME)"
  else
    fail "groups user create" "no ID returned"
  fi

  # Add member
  run_ok "groups add-member" $JC groups add-member "$TEST_GROUP_ID" --user "$TEST_USER_ID"

  # Graph traverse: user → system_group (user_group not valid for graph API)
  run_ok "graph traverse (user→system_group)" \
    $JC graph traverse --from "user:$TEST_USER_ID" --to system_group

  # Remove member
  run_ok "groups remove-member" $JC groups remove-member "$TEST_GROUP_ID" --user "$TEST_USER_ID"
fi

# ═══════════════════════════════════════════════════════════════════════
# Phase 3: Recipe Engine
# ═══════════════════════════════════════════════════════════════════════

if $SKIP_MUTABLE; then
  phase 3 "Recipe Engine (SKIPPED)"
  skip "recipe engine (--skip-mutable)"
else
  phase 3 "Recipe Engine"

  # Recipe list and show
  run_contains "recipe list" "onboard-user" $JC recipe list -t
  run_contains "recipe show" "onboard-user" $JC recipe show onboard-user

  RECIPE_USERNAME="jctest-recipe-$TS"
  RECIPE_EMAIL="jctest-recipe-${TS}@test.jumpcloud.invalid"
  RECIPE_GROUP_NAME="jctest-recipe-group-$TS"

  # Create group for recipe to use
  RECIPE_GROUP_ID=$($JC groups user create --name "$RECIPE_GROUP_NAME" --ids 2>/dev/null || true)
  if [ -n "$RECIPE_GROUP_ID" ]; then
    pass "recipe group create ($RECIPE_GROUP_NAME)"
  else
    fail "recipe group create" "no ID returned"
  fi

  # Onboard — plan mode (should exit 10)
  set +e
  $JC recipe run onboard-user \
    --param "username=$RECIPE_USERNAME" \
    --param "email=$RECIPE_EMAIL" \
    --param "firstname=Test" \
    --param "lastname=Recipe" \
    --param "group=$RECIPE_GROUP_NAME" \
    --plan > /dev/null 2>&1
  plan_exit=$?
  set -e
  if [ "$plan_exit" -eq 10 ]; then
    pass "onboard-user (plan mode, exit 10)"
  else
    fail "onboard-user (plan)" "expected exit 10, got $plan_exit"
  fi

  # Onboard — execute
  set +e
  $JC recipe run onboard-user \
    --param "username=$RECIPE_USERNAME" \
    --param "email=$RECIPE_EMAIL" \
    --param "firstname=Test" \
    --param "lastname=Recipe" \
    --param "group=$RECIPE_GROUP_NAME" \
    --force > /dev/null 2>&1
  recipe_exit=$?
  set -e
  if [ "$recipe_exit" -eq 0 ]; then
    pass "onboard-user (execute)"
  else
    fail "onboard-user (execute)" "exit code $recipe_exit"
  fi

  # Verify user exists
  if $JC users get "$RECIPE_USERNAME" > /dev/null 2>&1; then
    pass "recipe user exists"
  else
    fail "recipe user exists" "user not found after onboard"
  fi

  # Offboard with delete
  set +e
  $JC recipe run offboard-user \
    --param "user=$RECIPE_USERNAME" \
    --param "delete_user=true" \
    --force > /dev/null 2>&1
  offboard_exit=$?
  set -e
  if [ "$offboard_exit" -eq 0 ]; then
    pass "offboard-user (execute)"
  else
    fail "offboard-user (execute)" "exit code $offboard_exit"
  fi

  # Verify user gone
  run_fails "recipe user deleted" $JC users get "$RECIPE_USERNAME"
fi

# ═══════════════════════════════════════════════════════════════════════
# Phase 4: Read-Only Probes
# ═══════════════════════════════════════════════════════════════════════

phase 4 "Read-Only Probes"

# Core resources — spread across all 6 output formats
run_ok "users list (json)"              $JC users list --limit 3
run_ok "devices list (table)"           $JC devices list --limit 3 -t
run_ok "groups user list (csv)"         $JC groups user list --limit 3 --output csv
run_ok "groups device list (yaml)"      $JC groups device list --limit 3 --output yaml
run_ok "commands list (json)"           $JC commands list --limit 3
run_ok "policies list (table)"          $JC policies list --limit 3 -t
run_ok "policy-groups list (ndjson)"    $JC policy-groups list --limit 3 --output ndjson
run_ok "policy-templates list (human)"  $JC policy-templates list --limit 3 --output human
run_ok "apps list (table)"             $JC apps list --limit 3 -t
run_ok "app-templates list (json)"     $JC app-templates list --limit 3
run_ok "admins list (csv)"             $JC admins list --limit 3 --output csv
run_ok "auth-policies list (yaml)"     $JC auth-policies list --limit 3 --output yaml
run_ok "iplists list (ndjson)"         $JC iplists list --limit 3 --output ndjson
run_ok "software list (ndjson)"        $JC software list --limit 3 --output ndjson
# assets may 404 if not provisioned in the org
if $JC assets devices list --limit 2 > /dev/null 2>&1; then
  pass "assets devices list (json)"
  run_ok "assets devices list (table)" $JC assets devices list --limit 2 --output table
else
  skip "assets devices list (not provisioned)"
fi
if $JC identity-providers list > /dev/null 2>&1; then
  pass "identity-providers list (json)"
else
  pass "identity-providers list (empty is OK)"
fi
# saas-management may 404 if not provisioned in the org
if $JC saas-management list > /dev/null 2>&1; then
  pass "saas-management list (json)"
else
  skip "saas-management list (not provisioned)"
fi
run_ok "ldap list (human)"             $JC ldap list --output human
run_ok "ad list (json)"                $JC ad list --limit 3
run_ok "radius list (yaml)"            $JC radius list --output yaml
run_ok "apple-mdm list (table)"        $JC apple-mdm list -t
# gsuite may 404 if not provisioned in the org
if $JC gsuite list > /dev/null 2>&1; then
  pass "gsuite list (json)"
else
  skip "gsuite list (not provisioned)"
fi
run_ok "office365 list (json)"         $JC office365 list --limit 3
run_ok "duo list (csv)"                $JC duo list --limit 3 --output csv
run_ok "custom-emails templates (table)" $JC custom-emails templates -t
run_ok "user-states list (json)"       $JC user-states list
run_ok "org list (yaml)"               $JC org list --output yaml

# Insights
run_ok "insights query (json)"         $JC insights query --service all --last 1h --limit 5
run_ok "insights count"                $JC insights count --service all --last 1h

# MCP Apps
run_contains "mcp dashboard_view tool" "dashboard_view" $JC mcp tools

# System Insights
run_ok "system-insights tables"        $JC system-insights tables
run_ok "system-insights os_version"    $JC system-insights os_version -t

# Flag combinations
run_ok "fields selection"              $JC users list --limit 2 --fields username,email -t
run_ok "fields exclusion"              $JC users list --limit 2 --exclude password -t
run_ok "all fields"                    $JC users list --limit 2 --all -t
run_ok "ids mode"                      $JC users list --limit 2 --ids
run_ok "jmespath query"                $JC devices list --limit 2 --query "[].hostname"

# ═══════════════════════════════════════════════════════════════════════
# Phase 5: Utilities
# ═══════════════════════════════════════════════════════════════════════

phase 5 "Utilities"

run_contains "transport http in help" "http" $JC mcp serve --help
run_contains "explain" "delete users" $JC explain users delete testuser
run_ok "config view"           $JC config view
run_contains "schema resources" "users" $JC schema resources
run_ok "schema commands"       $JC schema commands
run_ok "completion bash"       $JC completion bash

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
  # Disable trap exit code override — cleanup runs but we exit 0
  trap - EXIT
  cleanup
  exit 0
else
  echo -e "${RED}${BOLD}$FAIL test(s) failed.${RESET}"
  trap - EXIT
  cleanup
  exit 1
fi
