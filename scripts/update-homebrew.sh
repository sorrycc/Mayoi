#!/bin/sh
# Refresh the tap from the signed disk images of a published release.
set -eu

version="${1:?usage: scripts/update-homebrew.sh <version>}"
case "$version" in
  *[!0-9.]* | "") echo "expected a version such as 0.12.2" >&2; exit 1 ;;
esac
root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

gh release download "v$version" --repo sorrycc/Mayoi --dir "$tmp" \
  --pattern "mayoi-$version-arm64.dmg" --pattern "mayoi-$version-amd64.dmg"
arm="$(shasum -a 256 "$tmp/mayoi-$version-arm64.dmg" | cut -d ' ' -f 1)"
intel="$(shasum -a 256 "$tmp/mayoi-$version-amd64.dmg" | cut -d ' ' -f 1)"

# Keep the committed cask as the template, preserving unrelated edits.
awk -v version="$version" -v arm="$arm" -v intel="$intel" '
  /^  version "[^"]*"$/ { $0 = "  version \"" version "\""; versions++ }
  /^  sha256 arm:   "[^"]*",$/ { $0 = "  sha256 arm:   \"" arm "\","; arms++ }
  /^         intel: "[^"]*"$/ { $0 = "         intel: \"" intel "\""; intels++ }
  { print }
  END {
    if (versions != 1 || arms != 1 || intels != 1) {
      print "unexpected cask layout; not updating Casks/mayoi.rb" > "/dev/stderr"
      exit 1
    }
  }
' "$root/Casks/mayoi.rb" > "$tmp/mayoi.rb"
mv "$tmp/mayoi.rb" "$root/Casks/mayoi.rb"
