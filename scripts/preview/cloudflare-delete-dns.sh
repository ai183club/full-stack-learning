#!/bin/sh
set -eu
: "${CLOUDFLARE_API_TOKEN:?}"; : "${CLOUDFLARE_ZONE_ID:?}"; : "${RECORD_NAME:?}"
records=$(curl --fail-with-body -sS -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/dns_records?type=CNAME&name=$RECORD_NAME")
printf %s "$records" | jq -r '.result[].id' | while IFS= read -r id; do curl --fail-with-body -sS -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/dns_records/$id" >/dev/null; done
