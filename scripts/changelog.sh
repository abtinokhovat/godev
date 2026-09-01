#!/usr/bin/env bash
# Prints one Markdown changelog section for the range (from, HEAD],
# grouping conventional-commit subjects by type. `from` may be
# "v0.0.0" (no prior tag) to mean "the whole history".
set -euo pipefail

from="$1"
to_tag="$2"

range="HEAD"
if [ "$from" != "v0.0.0" ] && git rev-parse "$from" >/dev/null 2>&1; then
  range="${from}..HEAD"
fi

types=(feat fix perf refactor docs build ci test style chore revert)
titles=(
  "Features" "Fixes" "Performance" "Refactoring" "Documentation"
  "Build" "CI" "Tests" "Style" "Chores" "Reverts"
)
type_pattern="$(IFS='|'; echo "${types[*]}")"

echo "## ${to_tag} - $(date +%Y-%m-%d)"
echo

breaking="$(git log "$range" --no-merges --pretty=format:'%s|%h' \
  | grep -E "^(${type_pattern})(\([^)]*\))?!: " || true)"
if [ -n "$breaking" ]; then
  echo "### Breaking Changes"
  while IFS='|' read -r subject hash; do
    [ -z "$subject" ] && continue
    echo "- ${subject#*: } (${hash})"
  done <<<"$breaking"
  echo
fi

for i in "${!types[@]}"; do
  type="${types[$i]}"
  title="${titles[$i]}"
  entries="$(git log "$range" --no-merges --pretty=format:'%s|%h' \
    | grep -E "^${type}(\([^)]*\))?: " || true)"
  [ -z "$entries" ] && continue
  echo "### ${title}"
  while IFS='|' read -r subject hash; do
    [ -z "$subject" ] && continue
    echo "- ${subject#*: } (${hash})"
  done <<<"$entries"
  echo
done

others="$(git log "$range" --no-merges --pretty=format:'%s|%h' \
  | grep -Ev "^(${type_pattern})(\([^)]*\))?!?: " || true)"
if [ -n "$others" ]; then
  echo "### Other"
  while IFS='|' read -r subject hash; do
    [ -z "$subject" ] && continue
    echo "- ${subject} (${hash})"
  done <<<"$others"
  echo
fi
