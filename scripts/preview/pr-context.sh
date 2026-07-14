#!/bin/sh
set -eu

: "${PR_NUMBER:?PR_NUMBER is required}"
: "${GIT_SHA:?GIT_SHA is required}"

case "$PR_NUMBER" in
  *[!0-9]* | '' | 0 | 0*)
    printf '%s\n' 'PR_NUMBER must be a positive decimal integer without leading zeroes' >&2
    exit 64
    ;;
esac

if [ "${#PR_NUMBER}" -gt 9 ]; then
  printf '%s\n' 'PR_NUMBER must contain at most 9 digits' >&2
  exit 64
fi

case "$GIT_SHA" in
  *[!0123456789abcdef]* | ?????? | ?????????????????????????????????????????*)
    printf '%s\n' 'GIT_SHA must be 7 to 40 lowercase hexadecimal characters' >&2
    exit 64
    ;;
esac

short_sha=$(printf '%s' "$GIT_SHA" | cut -c1-12)
preview_branch="pr-${PR_NUMBER}"

emit() {
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s=%s\n' "$1" "$2" >> "$GITHUB_OUTPUT"
  else
    printf '%s=%s\n' "$1" "$2"
  fi
}

emit pr_number "$PR_NUMBER"
emit schema "pr_${PR_NUMBER}"
emit database_role "profile_pr_${PR_NUMBER}"
emit image_tag "pr-${PR_NUMBER}-${short_sha}"
emit ecs_service "profile-api-pr-${PR_NUMBER}"
emit target_group "profile-pr-${PR_NUMBER}-tg"
emit preview_branch "$preview_branch"
emit web_host "pr-${PR_NUMBER}.preview.seebyte.xyz"
emit api_host "api-pr-${PR_NUMBER}.preview.seebyte.xyz"
