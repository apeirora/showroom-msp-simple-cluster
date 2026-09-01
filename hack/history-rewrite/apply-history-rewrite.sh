#!/usr/bin/env bash
# One-time history rewrite for open-sourcing showroom-msp-simple-cluster.
#
# Strips the three AI-assistant attribution trailers (F-03), the internal shoot /
# cluster names and private-repo issue links in two commit messages (F-07), and
# redacts the two demo SSH private keys from every historical blob (F-01, F-02).
# Tree content is otherwise byte-identical.
#
# It rewrites EVERY commit and tag. Nothing is pushed: it produces a rewritten
# mirror you inspect, and prints the push command for you to run deliberately.
#
#   ./apply-history-rewrite.sh <source-repo> <work-dir>
set -euo pipefail

SRC="${1:?usage: $0 <source-repo> <work-dir>}"
WORK="${2:?usage: $0 <source-repo> <work-dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

command -v python3 >/dev/null || { echo "python3 required" >&2; exit 1; }
[ -f "$HERE/git-filter-repo" ] || \
  curl -sSfL -o "$HERE/git-filter-repo" \
    https://raw.githubusercontent.com/newren/git-filter-repo/v2.47.0/git-filter-repo
chmod +x "$HERE/git-filter-repo"

rm -rf "$WORK"
git clone --no-local --mirror "$SRC" "$WORK"
cd "$WORK"

python3 "$HERE/git-filter-repo" --force \
  --blob-callback "$(cat "$HERE/blob_cb.py")" \
  --message-callback "$(cat "$HERE/msg_cb.py")"

echo
echo "=== verification ==="
printf 'AI trailers remaining:      %s\n' "$(git log --all --format='%B' | grep -ci 'anthropic' || true)"
printf 'internal refs remaining:    %s\n' "$(git log --all --format='%B' | grep -cE 'sh-[0-9a-f]{8}|demo-sap-ironcore|apeirora/showroom#' || true)"
printf 'commits: %s   tags: %s   branches: %s\n' \
  "$(git rev-list --all --count)" "$(git tag | wc -l)" "$(git branch | wc -l)"

cat <<MSG

Inspect the result, then publish deliberately:

  cd $WORK
  git remote add origin git@github.com:apeirora/showroom-msp-simple-cluster.git
  git push --force --mirror origin

Note: --mirror rewrites every branch and tag on the remote. The tag ruleset
restricts tag creation to the apeirora-showroom-bot App, so the ruleset must be
relaxed for the duration of the push and restored afterwards. Every open pull
request will need rebasing, and every clone re-cloning.
MSG
