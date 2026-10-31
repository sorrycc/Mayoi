#!/bin/sh
# Exercise release updates without downloading or installing the app.
# Expected wrapper lines contain literal shell variables.
# shellcheck disable=SC2016
set -eu

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/scripts" "$tmp/Casks" "$tmp/bin"
cp "$root/scripts/update-homebrew.sh" "$tmp/scripts/"
cp "$root/Casks/mayoi.rb" "$tmp/original.rb"
cat > "$tmp/bin/gh" <<'SH'
#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
  case "$1" in
    --dir) shift; dir="$1" ;;
    --pattern) shift; printf '%s\n' "$1" > "$dir/$1" ;;
  esac
  shift
done
SH
chmod +x "$tmp/bin/gh"
PATH="$tmp/bin:$PATH"
export PATH

# A release updates its version and checksums and keeps unrelated edits.
sed 's/Small, fast code editor/Edited description/; /  app "Mayoi.app"/a\
  zap trash: "~/Library/Mayoi"\
' "$tmp/original.rb" > "$tmp/Casks/mayoi.rb"
sh "$tmp/scripts/update-homebrew.sh" 0.12.9
grep -Fxq '  version "0.12.9"' "$tmp/Casks/mayoi.rb"
for arch in arm64 amd64; do
  sum="$(printf 'mayoi-0.12.9-%s.dmg\n' "$arch" | shasum -a 256 | cut -d ' ' -f 1)"
  grep -Fq "\"$sum\"" "$tmp/Casks/mayoi.rb"
done
grep -Fq '  desc "Edited description"' "$tmp/Casks/mayoi.rb"
grep -Fq '  zap trash: "~/Library/Mayoi"' "$tmp/Casks/mayoi.rb"
grep -Fxq '    MAYOI_CLI=1 exec "$app" "$@"' "$tmp/Casks/mayoi.rb"
cp "$tmp/Casks/mayoi.rb" "$tmp/updated.rb"
sh "$tmp/scripts/update-homebrew.sh" 0.12.9
cmp "$tmp/updated.rb" "$tmp/Casks/mayoi.rb"

# Missing or duplicate fields must fail before replacing the cask.
for layout in missing duplicate; do
  case "$layout" in
    missing) sed '/^  sha256 /d' "$tmp/original.rb" ;;
    duplicate) cat "$tmp/original.rb"; printf '  version "0.12.2"\n' ;;
  esac > "$tmp/Casks/mayoi.rb"
  cp "$tmp/Casks/mayoi.rb" "$tmp/before.rb"
  if sh "$tmp/scripts/update-homebrew.sh" 0.12.9 > "$tmp/output" 2>&1; then
    echo "accepted the $layout cask layout" >&2
    exit 1
  fi
  grep -Fq 'unexpected cask layout' "$tmp/output"
  cmp "$tmp/before.rb" "$tmp/Casks/mayoi.rb"
done
echo "Homebrew release update tests passed"
