#!/usr/bin/env bash
set -euo pipefail

# Validates that policy.yaml can be parsed and resolved for a given
# group without requiring a browser connection.
#
# Usage:
#   hack/validate-policy.sh          # default group: sre
#   hack/validate-policy.sh ops      # specify group

GROUP="${1:-sre}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/authzer"

echo "authzer policy validation"
echo "========================="
echo "Config:  $CONFIG_DIR/config.yaml"
echo "Policy:  $CONFIG_DIR/policy.yaml"
echo "Group:   $GROUP"
echo ""

if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    echo "ERROR: config.yaml not found at $CONFIG_DIR/config.yaml"
    exit 1
fi

if [ ! -f "$CONFIG_DIR/policy.yaml" ]; then
    echo "ERROR: policy.yaml not found at $CONFIG_DIR/policy.yaml"
    exit 1
fi

echo "--- Policy resolution ---"
authzer apply --group "$GROUP" --dry-run=client
echo ""
echo "Policy resolved successfully."
