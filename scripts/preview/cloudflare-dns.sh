#!/bin/sh
set -eu
: "${CLOUDFLARE_API_TOKEN:?}"; : "${CLOUDFLARE_ZONE_ID:?}"; : "${RECORD_NAME:?}"; : "${RECORD_TARGET:?}"
RECORD_PROXIED=${RECORD_PROXIED:-true}
case "$RECORD_PROXIED" in
  true|false) ;;
  *) echo 'RECORD_PROXIED must be true or false' >&2; exit 2 ;;
esac
existing=$(curl --fail-with-body -sS -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/dns_records?type=CNAME&name=$RECORD_NAME")
payload=$(jq -n --arg name "$RECORD_NAME" --arg target "$RECORD_TARGET" --argjson proxied "$RECORD_PROXIED" '{type:"CNAME",name:$name,content:$target,proxied:$proxied,ttl:1}')
record_id=$(printf %s "$existing" | jq -r '.result[0].id // empty')
if [ -n "$record_id" ]; then curl --fail-with-body -sS -X PUT -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" -H 'Content-Type: application/json' --data "$payload" "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/dns_records/$record_id" >/dev/null; else curl --fail-with-body -sS -X POST -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" -H 'Content-Type: application/json' --data "$payload" "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/dns_records" >/dev/null; fi
