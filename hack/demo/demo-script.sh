#!/usr/bin/env bash
# demo-script.sh — typed-command scenario for authzer public demo.
#
# Uses the type_command / run_cmd / comment / demo_pause helpers to
# produce a human-readable terminal recording via asciinema.

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

# --- Scenario ---

echo
comment "authzer — declarative access entitlement management"
demo_pause 2

comment "Check the version"
run_cmd "authzer version"
demo_pause 1

comment "List current memberships from the portal"
run_cmd "authzer get"
demo_pause 2
screenshot "01-memberships"

comment "Inspect a specific entitlement"
run_cmd "authzer get log-analytics-e5f6 -o yaml"
demo_pause 2

comment "Show the reconciliation plan (client dry-run)"
run_cmd "authzer apply --dry-run=client --sort-by expiry"
demo_pause 3

comment "Prepare extend forms in the browser (server dry-run)"
run_cmd "authzer apply --dry-run=server --accept-terms"
demo_pause 3
screenshot "02-extend-dialog"

comment "Submit the renewals"
run_cmd "authzer apply --dry-run=none --accept-terms --log-file audit.jsonl"
demo_pause 2

comment "Review the structured audit log"
run_cmd "cat audit.jsonl | jq ."
demo_pause 3

comment "Done."
echo
