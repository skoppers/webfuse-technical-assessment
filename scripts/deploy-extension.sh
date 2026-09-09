#!/usr/bin/env bash
# Build extension/ and push dist/ to a Webfuse Space via the REST API.
# Creates the extension on first run, PUT-updates it thereafter (matched by manifest name).
#
# Config: scripts/../.env.deploy (gitignored) or env vars. See .env.deploy.example.
#   REST_KEY      rk_… (space) or ck_… (company)   — required, secret
#   SPACE_ID      numeric space id                 — required
#   WEBFUSE_HOST  default webfuse.com
#   STORAGE_APP   storage app id; auto-resolved if the space has exactly one
#   EXT_ID        extension id; auto-resolved by name if omitted
#   REFRESH       true → hot-refresh extension in all active sessions (default true)
#   BUILD         false → skip `npm run build` (default true)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$ROOT/.env.deploy}"
if [[ -f "$ENV_FILE" ]]; then
  set -a; # shellcheck disable=SC1090
  source "$ENV_FILE"; set +a
fi

: "${WEBFUSE_HOST:=webfuse.com}"
: "${SPACE_ID:?set SPACE_ID (in $ENV_FILE or env)}"
: "${REST_KEY:?set REST_KEY (rk_… or ck_…) in $ENV_FILE or env}"
STORAGE_APP="${STORAGE_APP:-}"
EXT_ID="${EXT_ID:-}"
REFRESH="${REFRESH:-true}"
BUILD="${BUILD:-true}"
EXT_DIR="$ROOT/extension"
DIST="$EXT_DIR/dist"

case "$REST_KEY" in rk_*|ck_*) ;; *) echo "REST_KEY must start with rk_ or ck_" >&2; exit 1;; esac
for t in curl jq zip; do command -v "$t" >/dev/null || { echo "missing: $t" >&2; exit 1; }; done

BASE="https://${WEBFUSE_HOST}/api/spaces/${SPACE_ID}"
AUTH="Authorization: Token ${REST_KEY}"
api() { curl -fsS -H "$AUTH" "$@"; }

# --- build ---
if [[ "$BUILD" == "true" ]]; then
  echo "→ building extension"
  (cd "$EXT_DIR" && npm run --silent build)
fi
[[ -f "$DIST/manifest.json" ]] || { echo "no $DIST/manifest.json — build failed?" >&2; exit 1; }
NAME="$(jq -r .name "$DIST/manifest.json")"

# --- storage app ---
if [[ -z "$STORAGE_APP" ]]; then
  storages="$(api "${BASE}/storages/?page_size=100")"
  n="$(jq '.results | length' <<<"$storages")"
  if [[ "$n" == "1" ]]; then
    STORAGE_APP="$(jq -r '.results[0].id' <<<"$storages")"
    echo "→ using storage app ${STORAGE_APP} ($(jq -r '.results[0].name' <<<"$storages"))"
  else
    echo "found $n storage apps; set STORAGE_APP to one of:" >&2
    jq -r '.results[] | "  \(.id)\t\(.provider)\t\(.name)"' <<<"$storages" >&2
    exit 1
  fi
fi

# --- existing extension? ---
if [[ -z "$EXT_ID" ]]; then
  EXT_ID="$(api "${BASE}/extensions/?page_size=100" \
    | jq -r --arg n "$NAME" '[.results[] | select(.name == $n)][0].id // empty')"
fi

# --- zip + upload ---
ZIP="$(mktemp -t ext-XXXXXX).zip"
RESP="$(mktemp -t ext-resp-XXXXXX)"
trap 'rm -f "$ZIP" "$RESP"' EXIT
(cd "$DIST" && zip -qr "$ZIP" .)

if [[ -n "$EXT_ID" ]]; then
  echo "→ updating extension '${NAME}' (id ${EXT_ID})"
  api -X PUT "${BASE}/extensions/${EXT_ID}/" -F "zip_file=@${ZIP};type=application/zip" -F "storage_app=${STORAGE_APP}" -o "$RESP"
else
  echo "→ creating extension '${NAME}'"
  api -X POST "${BASE}/extensions/" -F "zip_file=@${ZIP};type=application/zip" -F "storage_app=${STORAGE_APP}" -o "$RESP"
  EXT_ID="$(jq -r '.id' "$RESP")"
  echo "  created id ${EXT_ID} (add EXT_ID=${EXT_ID} to $ENV_FILE to skip lookup)"
fi
jq -c '{id, name, updated_at}' "$RESP"

# --- hot-refresh live sessions ---
if [[ "$REFRESH" == "true" ]]; then
  url="${BASE}/sessions/?active=true&page_size=100"
  count=0
  while [[ -n "$url" && "$url" != "null" ]]; do
    page="$(api "$url")"
    while read -r sid; do
      [[ -z "$sid" ]] && continue
      if api "${BASE}/sessions/${sid}/refresh-extension/${EXT_ID}/" -o /dev/null; then
        echo "  · refreshed ${sid}"; count=$((count+1))
      else
        echo "  · failed ${sid}" >&2
      fi
    done < <(jq -r '.results[] | select(.is_active == true or .duration == null) | .session_id' <<<"$page")
    url="$(jq -r '.next' <<<"$page")"
  done
  echo "→ refreshed ${count} live session(s)"
fi

echo "✓ done"
