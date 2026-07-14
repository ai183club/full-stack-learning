#!/bin/sh
set -eu

: "${CLOUDFLARE_API_TOKEN:?}"
: "${CLOUDFLARE_ACCOUNT_ID:?}"
: "${CLOUDFLARE_PAGES_PROJECT:?}"
: "${PAGES_DOMAIN:?}"
: "${PAGES_DOMAIN_ACTION:?}"

base_url="https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/$CLOUDFLARE_PAGES_PROJECT/domains"
auth_header="Authorization: Bearer $CLOUDFLARE_API_TOKEN"

domain_response=$(mktemp)
trap 'rm -f "$domain_response"' EXIT

fetch_domain() {
  domain_http=$(curl -sS -o "$domain_response" -w '%{http_code}' -H "$auth_header" "$base_url/$PAGES_DOMAIN" || true)
}

require_successful_response() {
  case "$domain_http" in
    2??) jq -e '.success == true' "$domain_response" >/dev/null ;;
    *)
      echo "Cloudflare Pages domain request failed: HTTP $domain_http" >&2
      cat "$domain_response" >&2
      exit 1
      ;;
  esac
}

case "$PAGES_DOMAIN_ACTION" in
  ensure)
    fetch_domain
    if [ "$domain_http" = 404 ]; then
      payload=$(jq -n --arg name "$PAGES_DOMAIN" '{name:$name}')
      response=$(curl --fail-with-body -sS -X POST -H "$auth_header" -H 'Content-Type: application/json' --data "$payload" "$base_url")
      printf '%s' "$response" | jq -e '.success == true' >/dev/null
    else
      require_successful_response
    fi
    ;;
  delete)
    fetch_domain
    if [ "$domain_http" != 404 ]; then
      require_successful_response
      response=$(curl --fail-with-body -sS -X DELETE -H "$auth_header" "$base_url/$PAGES_DOMAIN")
      printf '%s' "$response" | jq -e '.success == true' >/dev/null
    fi
    ;;
  wait)
    attempt=1
    while :; do
      fetch_domain
      if [ "$domain_http" = 404 ]; then
        status=not-found
      else
        require_successful_response
        status=$(jq -r '.result.status' "$domain_response")
      fi
      case "$status" in
        active) break ;;
        blocked | deactivated | error)
          echo "Pages domain entered terminal status: $status" >&2
          exit 1
          ;;
      esac
      if [ "$attempt" -ge 30 ]; then
        echo 'Timed out waiting for Pages domain activation' >&2
        exit 1
      fi
      attempt=$((attempt + 1))
      sleep 10
    done
    ;;
  *)
    echo 'PAGES_DOMAIN_ACTION must be ensure, wait, or delete' >&2
    exit 64
    ;;
esac
