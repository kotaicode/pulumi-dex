#!/bin/bash
# Print the body of the CHANGELOG section for a version (the lines between its
# "## [VERSION]" heading and the next "## " heading), for use as release notes.
# Fails if the section is missing or empty, so a release is never published
# without its notes.
set -euo pipefail

version="${1:?usage: changelog-section.sh VERSION [CHANGELOG]}"
changelog="${2:-CHANGELOG.md}"

section=$(awk -v heading="## [$version]" '
  found && /^## / { exit }
  found { print; next }
  index($0, heading) == 1 && (length($0) == length(heading) || substr($0, length(heading) + 1, 1) == " ") { found = 1 }
' "$changelog" | sed -e '/./,$!d')

if [ -z "$(printf '%s' "$section" | tr -d '[:space:]')" ]; then
  echo "::error::$changelog has no '## [$version]' section, or it is empty" >&2
  exit 1
fi
printf '%s\n' "$section"
