#!/usr/bin/env bash
#
# jc CLI integration test — exercises the full CLI surface against a live
# JumpCloud organization. Requires authenticated jc on PATH.
#
# Usage:
#   ./scripts/integration-test.sh                # Run all phases
#   ./scripts/integration-test.sh --skip-mutable  # Skip create/delete phases
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
    if echo "$output" | grep -q "$needle"; then
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
    jc users delete "$TEST_USER_ID" --force > /dev/null 2>&1 && cleaned=$((cleaned + 1)) || true
    echo -e "  ${DIM}deleted test user${RESET}"
  fi

  if [ -n "$TEST_GROUP_ID" ]; then
    jc groups user delete "$TEST_GROUP_ID" --force > /dev/null 2>&1 && cleaned=$((cleaned + 1)) || true
    echo -e "  ${DIM}deleted test group${RESET}"
  fi

  if [ -n "$RECIPE_GROUP_ID" ]; then
    jc groups user delete "$RECIPE_GROUP_ID" --force > /dev/null 2>&1 && cleaned=$((cleaned + 1)) || true
    echo -e "  ${DIM}deleted recipe group${RESET}"
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
run_ok "org list" jc org list --limit 1

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
  run_contains "users get" "$TEST_USERNAME" jc users get "$TEST_USER_ID"

  # Search user
  run_contains "users search" "$TEST_USERNAME" jc users search "$TEST_USERNAME"

  # Update user
  run_ok "users update (department)" jc users update "$TEST_USER_ID" --department "Integration Test"

  # Lock / unlock
  run_ok "users lock" jc users lock "$TEST_USER_ID"
  run_ok "users unlock" jc users unlock "$TEST_USER_ID"

  # Create group
  TEST_GROUP_ID=$(jc groups user create --name "$TEST_GROUP_NAME" --ids 2>/dev/null || true)
  if [ -n "$TEST_GROUP_ID" ]; then
    pass "groups user create ($TEST_GROUP_NAME)"
  else
    fail "groups user create" "no ID returned"
  fi

  # Add member
  run_ok "groups add-member" jc groups add-member "$TEST_GROUP_ID" --user "$TEST_USER_ID"

  # Graph traverse: user → user_group
  run_contains "graph traverse (user→user_group)" "$TEST_GROUP_ID" \
    jc graph traverse --from "user:$TEST_USER_ID" --to user_group

  # Remove member
  run_ok "groups remove-member" jc groups remove-member "$TEST_GROUP_ID" --user "$TEST_USER_ID"
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
  run_contains "recipe list" "onboard-user" jc recipe list -t
  run_contains "recipe show" "onboard-user" jc recipe show onboard-user

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
  set +e
  jc recipe run onboard-user \
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
  RECIPE_OUTPUT=$(jc recipe run onboard-user \
    --param "username=$RECIPE_USERNAME" \
    --param "email=$RECIPE_EMAIL" \
    --param "firstname=Test" \
    --param "lastname=Recipe" \
    --param "group=$RECIPE_GROUP_NAME" \
    --force 2>&1)
  recipe_exit=$?
  set -e
  if [ "$recipe_exit" -eq 0 ]; then
    pass "onboard-user (execute)"
  else
    fail "onboard-user (execute)" "exit code $recipe_exit"
  fi

  # Verify user exists
  if jc users get "$RECIPE_USERNAME" > /dev/null 2>&1; then
    pass "recipe user exists"
  else
    fail "recipe user exists" "user not found after onboard"
  fi

  # Offboard with delete
  set +e
  OFFBOARD_OUTPUT=$(jc recipe run offboard-user \
    --param "user=$RECIPE_USERNAME" \
    --param "delete_user=true" \
    --force 2>&1)
  offboard_exit=$?
  set -e
  if [ "$offboard_exit" -eq 0 ]; then
    pass "offboard-user (execute)"
  else
    fail "offboard-user (execute)" "exit code $offboard_exit"
  fi

  # Verify user gone
  run_fails "recipe user deleted" jc users get "$RECIPE_USERNAME"
fi
