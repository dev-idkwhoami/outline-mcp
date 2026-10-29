#!/usr/bin/env bash
# Invoked only by the tag workflow after tests and both native builds pass.
set -euo pipefail
: "${GH_REPO:?GH_REPO is required}"
: "${RELEASE_TAG:?RELEASE_TAG is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

if [[ ! "$RELEASE_TAG" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo 'Invalid release tag.' >&2
  exit 1
fi

assets=(bin/outline-mcp-linux-amd64 bin/outline-mcp-windows-amd64.exe bin/LICENSE bin/THIRD_PARTY_NOTICES.txt bin/build-info.json bin/SHA256SUMS)
for asset in "${assets[@]}"; do test -s "$asset"; done
(cd bin && sha256sum --check SHA256SUMS)

# Listing succeeds before any mutation. A network/auth error must not be
# mistaken for "release does not exist" and trigger an uncertain create.
existing=$(gh api --paginate "repos/$GH_REPO/releases" --jq '.[]' |
  jq -s --arg tag "$RELEASE_TAG" '[.[] | select(.tag_name == $tag)]')
count=$(jq 'length' <<< "$existing")
if [[ "$count" == 0 ]]; then
  extra=()
  if [[ "$RELEASE_TAG" == *-* ]]; then extra+=(--prerelease); fi
  gh release create "$RELEASE_TAG" "${assets[@]}" --repo "$GH_REPO" \
    --draft --verify-tag --title "Outline MCP $RELEASE_TAG" --generate-notes "${extra[@]}"
elif [[ "$count" == 1 && $(jq -r '.[0].draft' <<< "$existing") == true ]]; then
  gh release upload "$RELEASE_TAG" "${assets[@]}" --repo "$GH_REPO" --clobber
else
  echo 'Refusing to modify a published or ambiguous release.' >&2
  exit 1
fi

test "$(gh release view "$RELEASE_TAG" --repo "$GH_REPO" --json isDraft --jq .isDraft)" = true
verify_dir=$(mktemp -d "$RUNNER_TEMP/outline-release-verify.XXXXXX")
trap 'rm -rf "$verify_dir"' EXIT
for asset in "${assets[@]}"; do
  gh release download "$RELEASE_TAG" --repo "$GH_REPO" --dir "$verify_dir" --pattern "${asset#bin/}"
done
cmp bin/SHA256SUMS "$verify_dir/SHA256SUMS"
(cd "$verify_dir" && sha256sum --check SHA256SUMS)
echo "Draft $RELEASE_TAG uploaded and SHA-256 verified; publish it manually after review."
