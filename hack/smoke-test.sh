#!/usr/bin/env bash
set -euo pipefail

# Validates authzer against a live portal.
#
# Prerequisites:
#   - Chrome/Edge with CDP on tcp/9222
#   - authzer config deployed to ~/.config/authzer/
#
# Usage:
#   hack/smoke-test.sh
#   GROUP=senior-sre hack/smoke-test.sh
#   AUTHZER=/path/to/authzer hack/smoke-test.sh

AUTHZER="${AUTHZER:-authzer}"
GROUP="${GROUP:-sre}"

echo "authzer smoke test"
echo "=================="
echo "Binary:  $AUTHZER"
echo "Group:   $GROUP"
echo ""

echo "--- Version ---"
$AUTHZER version
echo ""

echo "--- Get (list memberships) ---"
$AUTHZER get --group "$GROUP" 2>&1
echo ""

echo "--- Get (structured output) ---"
$AUTHZER get --group "$GROUP" -o wide 2>&1
echo ""

echo "--- Describe (cached details) ---"
$AUTHZER describe 2>&1 | head -40
echo ""

echo "--- Apply (client dry-run: policy plan) ---"
$AUTHZER apply --group "$GROUP" --dry-run=client 2>&1
echo ""

echo "Smoke test complete."
