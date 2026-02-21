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
