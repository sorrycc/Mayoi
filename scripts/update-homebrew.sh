#!/bin/sh
# Refresh the tap from the signed disk images of a published release.
set -eu

version="${1:?usage: scripts/update-homebrew.sh <version>}"
case "$version" in
  *[!0-9.]* | "") echo "expected a version such as 0.11.11" >&2; exit 1 ;;
esac
root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

gh release download "v$version" --repo sorrycc/Mayoi --dir "$tmp" \
  --pattern "Mayoi.$version.arm64.dmg" --pattern "Mayoi.$version.amd64.dmg"
arm="$(shasum -a 256 "$tmp/Mayoi.$version.arm64.dmg" | cut -d ' ' -f 1)"
intel="$(shasum -a 256 "$tmp/Mayoi.$version.amd64.dmg" | cut -d ' ' -f 1)"

# Keep the committed cask as the template, preserving unrelated edits.
# The first release with MAYOI_CLI removes the old app's compatibility shim.
awk -v version="$version" -v arm="$arm" -v intel="$intel" '
  /^  version "[^"]*"$/ { $0 = "  version \"" version "\""; versions++ }
  /^  sha256 arm:   "[^"]*",$/ { $0 = "  sha256 arm:   \"" arm "\","; arms++ }
  /^         intel: "[^"]*"$/ { $0 = "         intel: \"" intel "\""; intels++ }
  version != "0.11.11" && $0 == "    app=#{(appdir/\"Mayoi.app\").to_s.shellescape}" {
    legacy = 1
    next
  }
  legacy {
    if ($0 == "    MAYOI_CLI=1 exec \"$app/Contents/MacOS/Mayoi\" \"$@\"") {
      print "    MAYOI_CLI=1 exec #{(appdir/\"Mayoi.app/Contents/MacOS/Mayoi\").to_s.shellescape} \"$@\""
      legacy = 0
      migrated++
    }
    next
  }
  { print }
  END {
    if (versions != 1 || arms != 1 || intels != 1 || legacy || migrated > 1) {
      print "unexpected cask layout; not updating Casks/mayoi.rb" > "/dev/stderr"
      exit 1
    }
  }
' "$root/Casks/mayoi.rb" > "$tmp/mayoi.rb"
mv "$tmp/mayoi.rb" "$root/Casks/mayoi.rb"
