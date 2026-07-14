#!/bin/sh
set -eu

: "${WEB_URL:?}"
: "${API_URL:?}"
API_CONNECT_TARGET=${API_CONNECT_TARGET:-}

api_host=${API_URL#https://}
api_host=${api_host%%/*}

attempt=1
while :; do
  web_status=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 "$WEB_URL" || true)
  if [ "$web_status" = 200 ]; then break; fi
  if [ "$attempt" -ge 30 ]; then
    echo "Web preview did not become ready: HTTP $web_status" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  sleep 10
done

body=$(mktemp)
trap 'rm -f "$body"' EXIT
attempt=1
while :; do
  # Keep the preview hostname for SNI/Host, but optionally connect directly to
  # the API Gateway target to avoid a stale recursive DNS answer after a
  # proxied-to-DNS-only record transition.
  if [ -n "$API_CONNECT_TARGET" ]; then
    api_status=$(curl -sS -o "$body" -w '%{http_code}' --max-time 30 --connect-to "$api_host:443:$API_CONNECT_TARGET:443" "$API_URL/api/profiles/preview_smoke_user?view=public" || true)
  else
    api_status=$(curl -sS -o "$body" -w '%{http_code}' --max-time 30 "$API_URL/api/profiles/preview_smoke_user?view=public" || true)
  fi
  if [ "$api_status" = 404 ] && jq -e '.error == "profile not found"' "$body" >/dev/null 2>&1; then break; fi
  if [ "$attempt" -ge 30 ]; then
    echo "API preview did not return the expected business response: HTTP $api_status" >&2
    cat "$body" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  sleep 10
done
