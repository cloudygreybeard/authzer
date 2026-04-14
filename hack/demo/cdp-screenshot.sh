#!/usr/bin/env bash
# cdp-screenshot.sh — capture a CDP screenshot via the Chrome DevTools Protocol.
#
# Usage: cdp-screenshot.sh NAME [CDP_URL]
#
# Captures the current browser viewport and saves it as
# $SCREENSHOT_DIR/NN-NAME.png. The sequence counter increments
# automatically. Requires curl and jq.

set -euo pipefail

NAME="${1:?Usage: cdp-screenshot.sh NAME [CDP_URL]}"
CDP_URL="${2:-${CDP_URL:-http://127.0.0.1:9222}}"
SCREENSHOT_DIR="${SCREENSHOT_DIR:-/tmp/screenshots}"

mkdir -p "${SCREENSHOT_DIR}"

# Find the sequence number for the next screenshot.
EXISTING=$(ls "${SCREENSHOT_DIR}"/*.png 2>/dev/null | wc -l || echo 0)
SEQ=$(printf "%02d" "$((EXISTING + 1))")

# Get the debugger WebSocket URL for the first non-blank page.
WS_URL=$(curl -sf "${CDP_URL}/json" | jq -r '
  [.[] | select(.type == "page" and (.url | test("^chrome://") | not))] | first | .webSocketDebuggerUrl // empty
')

if [ -z "${WS_URL}" ]; then
  echo "cdp-screenshot: no suitable page found" >&2
  exit 1
fi

# The DevTools HTTP endpoint supports a screenshot API.
# Use the /json/protocol to send a command via HTTP.
PAGE_ID=$(curl -sf "${CDP_URL}/json" | jq -r '
  [.[] | select(.type == "page" and (.url | test("^chrome://") | not))] | first | .id // empty
')

if [ -z "${PAGE_ID}" ]; then
  echo "cdp-screenshot: no page ID found" >&2
  exit 1
fi

# Capture screenshot using CDP's Page.captureScreenshot via HTTP.
RESULT=$(curl -sf "http://127.0.0.1:${CDP_URL##*:}/json/protocol" 2>/dev/null || true)

# Fallback: use the simple /screenshot endpoint if available, otherwise
# use a websocket-based approach via a small inline script.
OUTFILE="${SCREENSHOT_DIR}/${SEQ}-${NAME}.png"

# Use curl to call the CDP endpoint and base64 decode the screenshot.
# Chrome DevTools Protocol requires WebSocket, so we use a Python one-liner
# as a lightweight alternative if available, otherwise fall back to curl.
if command -v python3 &>/dev/null; then
  python3 -c "
import json, base64, sys
try:
    from websocket import create_connection
    ws = create_connection('${WS_URL}')
    ws.send(json.dumps({'id': 1, 'method': 'Page.captureScreenshot', 'params': {'format': 'png'}}))
    result = json.loads(ws.recv())
    ws.close()
    data = base64.b64decode(result['result']['data'])
    with open('${OUTFILE}', 'wb') as f:
        f.write(data)
    print('${OUTFILE}')
except ImportError:
    # websocket-client not available — try simpler approach
    import urllib.request
    # Use the devtools frontend screenshot URL
    sys.exit(1)
" 2>/dev/null && exit 0
fi

# If Python websocket approach failed, use the devtools screenshot URL.
# This is available on newer Chrome versions.
curl -sf "${CDP_URL}/screenshot/${PAGE_ID}" -o "${OUTFILE}" 2>/dev/null && {
  echo "${OUTFILE}"
  exit 0
}

echo "cdp-screenshot: capture failed (install python3-websocket for reliable screenshots)" >&2
exit 1
