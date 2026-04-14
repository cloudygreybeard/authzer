#!/usr/bin/env bash
# demo-script.sh — typed-command scenario for authzer public demo.
#
# Three-act narrative: current state, policy declaration, reconciliation.
# Uses type_command / run_cmd / comment / demo_pause helpers to produce
# a human-readable terminal recording via asciinema.

set -euo pipefail

# --- Tunables ---
TYPE_SPEED="${TYPE_SPEED:-0.020}"
SCREENSHOT_DIR="${SCREENSHOT_DIR:-/tmp/screenshots}"
CDP_URL="${CDP_URL:-http://127.0.0.1:9222}"

# --- ANSI colours ---
BLUE='\033[1;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Wait for Chromium CDP in the pod (shared localhost).
for i in $(seq 1 30); do
  curl -sf "${CDP_URL}/json/version" >/dev/null 2>&1 && break
  sleep 0.5
done

# --- Helpers ---

type_command() {
  local cmd="$1"
  printf "${BLUE}\$ ${NC}"
  for (( i=0; i<${#cmd}; i++ )); do
    printf "%s" "${cmd:$i:1}"
    sleep "${TYPE_SPEED}"
  done
  printf "\n"
}

run_cmd() {
  local cmd="$1"
  type_command "${cmd}"
  eval "${cmd}"
}

comment() {
  printf "${YELLOW}# %s${NC}\n" "$1"
}

demo_pause() {
  sleep "${1:-1.5}"
}

screenshot() {
  if [ -f /demo/cdp-screenshot.sh ]; then
    /demo/cdp-screenshot.sh "$1" 2>/dev/null || true
  fi
}

# --- Act 1: The problem ---

echo
comment "authzer — declarative access management,"
comment "even if the only API is a button."
echo
demo_pause 3

comment "Six entitlements. Thirty-day expiry. One web portal."
comment "What does it look like today?"
echo
demo_pause 1
run_cmd "authzer get"
demo_pause 3
screenshot "01-memberships"

# --- Act 2: The policy ---

echo
echo
comment "Access policy is declared as RBAC manifests."
demo_pause 1
run_cmd "authzer config policy"
demo_pause 5

# --- Act 3: Reconcile ---

echo
echo
comment "Reconcile policy against the portal."
comment "First, plan locally — no browser needed."
echo
demo_pause 1
run_cmd "authzer apply --dry-run=client --sort-by expiry"
demo_pause 4

echo
echo
comment "Now prepare the forms in the browser."
echo
demo_pause 1
run_cmd "authzer apply --accept-terms"
demo_pause 3
screenshot "02-renewal-forms"

echo
comment "Done."
echo
