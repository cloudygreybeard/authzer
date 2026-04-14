#!/usr/bin/env bash
# record-demo.sh — orchestrate the full demo recording pipeline.
#
# Creates a podman pod (shared network namespace), starts mock-portal
# and chromium, runs the authzer demo container under asciinema, and
# post-processes the recording into GIF and SVG formats.
#
# All containers in the pod share localhost, so Chrome's DevTools on
# 127.0.0.1:9222 and mock-portal on 127.0.0.1:8080 are both reachable
# from the authzer-demo container without DNS or proxying.
#
# Prerequisites:
#   podman (or docker)
#   brew install asciinema agg
#   npm install -g svg-term-cli
#
# Usage:
#   ./hack/record-demo.sh              # full recording + post-process
#   ./hack/record-demo.sh --no-record  # run demo interactively (no asciinema)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

CONTAINER_RT="${CONTAINER_RT:-podman}"
POD="authzer-demo-pod"
PORTAL_IMAGE="mock-portal"
CHROMIUM_IMAGE="authzer-chromium"
DEMO_IMAGE="authzer-demo"
CAST_FILE="${PROJECT_DIR}/docs/authzer-demo.cast"
GIF_FILE="${PROJECT_DIR}/docs/authzer-demo.gif"
SVG_FILE="${PROJECT_DIR}/docs/authzer-demo.svg"
SCREENSHOT_DIR="${PROJECT_DIR}/docs/screenshots"
TYPE_SPEED="${TYPE_SPEED:-0.020}"
NO_RECORD="${1:-}"

COLS=90
ROWS=30

cleanup() {
  echo "Cleaning up..." >&2
  ${CONTAINER_RT} pod rm -f "${POD}" 2>/dev/null || true
}
trap cleanup EXIT

mkdir -p "$(dirname "${CAST_FILE}")" "${SCREENSHOT_DIR}"

# Create the pod. All containers share the network namespace.
${CONTAINER_RT} pod rm -f "${POD}" 2>/dev/null || true
${CONTAINER_RT} pod create --name "${POD}"

# Start the mock portal.
echo "Starting mock portal..." >&2
${CONTAINER_RT} run -d \
  --pod "${POD}" \
  --name "${POD}-portal" \
  "${PORTAL_IMAGE}"
sleep 1
echo "Mock portal ready." >&2

# Start headless Chromium.
echo "Starting Chromium..." >&2
${CONTAINER_RT} run -d \
  --pod "${POD}" \
  --name "${POD}-chromium" \
  "${CHROMIUM_IMAGE}"

# Wait for CDP to be reachable within the pod (shared localhost).
for i in $(seq 1 30); do
  if ${CONTAINER_RT} logs "${POD}-chromium" 2>&1 | grep -q "DevTools listening"; then
    break
  fi
  sleep 0.5
done
echo "Chromium CDP ready." >&2

if [ "${NO_RECORD}" = "--no-record" ]; then
  echo "Running demo interactively (no recording)..." >&2
  ${CONTAINER_RT} run --rm -it \
    --pod "${POD}" \
    -e "TYPE_SPEED=${TYPE_SPEED}" \
    "${DEMO_IMAGE}"
  exit 0
fi

# Record with asciinema (asciicast v2 for svg-term compatibility).
echo "Recording demo..." >&2
asciinema rec "${CAST_FILE}" \
  --command "${CONTAINER_RT} run --rm --pod ${POD} -e TYPE_SPEED=${TYPE_SPEED} ${DEMO_IMAGE}" \
  --idle-time-limit 3 \
  --overwrite \
  --output-format asciicast-v2 \
  --cols "${COLS}" \
  --rows "${ROWS}"

echo "Recording saved to ${CAST_FILE}" >&2

# Post-process: GIF via agg (if available).
if command -v agg &>/dev/null; then
  echo "Generating GIF..." >&2
  agg "${CAST_FILE}" "${GIF_FILE}" \
    --cols "${COLS}" \
    --rows "${ROWS}" \
    --font-size 16
  echo "GIF saved to ${GIF_FILE}" >&2
else
  echo "agg not found — skipping GIF generation. Install: cargo install agg" >&2
fi

# Post-process: SVG via svg-term-cli (if available).
if command -v svg-term &>/dev/null; then
  echo "Generating SVG..." >&2
  svg-term \
    --in "${CAST_FILE}" \
    --out "${SVG_FILE}" \
    --window \
    --width "${COLS}" \
    --height "${ROWS}" \
    --padding 10
  echo "SVG saved to ${SVG_FILE}" >&2
else
  echo "svg-term not found — skipping SVG generation. Install: npm install -g svg-term-cli" >&2
fi

echo "Done." >&2
