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

# Repeating the current release must retain its old-app fallback.
cp "$tmp/original.rb" "$tmp/Casks/mayoi.rb"
sh "$tmp/scripts/update-homebrew.sh" 0.11.11
grep -Fq '    if [ "$version" = "0.11.11" ]; then' "$tmp/Casks/mayoi.rb"

# Future releases remove that fallback and preserve unrelated cask edits.
sed 's/Small, fast code editor/Edited description/; /  app "Mayoi.app"/a\
  zap trash: "~/Library/Mayoi"\
' "$tmp/original.rb" > "$tmp/Casks/mayoi.rb"
sh "$tmp/scripts/update-homebrew.sh" 0.11.12
grep -Fxq '  version "0.11.12"' "$tmp/Casks/mayoi.rb"
for arch in arm64 amd64; do
  sum="$(printf 'Mayoi.0.11.12.%s.dmg\n' "$arch" | shasum -a 256 | cut -d ' ' -f 1)"
  grep -Fq "\"$sum\"" "$tmp/Casks/mayoi.rb"
done
grep -Fq '  desc "Edited description"' "$tmp/Casks/mayoi.rb"
grep -Fq '  zap trash: "~/Library/Mayoi"' "$tmp/Casks/mayoi.rb"
grep -Fxq '    MAYOI_CLI=1 exec #{(appdir/"Mayoi.app/Contents/MacOS/Mayoi").to_s.shellescape} "$@"' "$tmp/Casks/mayoi.rb"
if grep -Eq 'PlistBuddy|0\.11\.11|/usr/bin/open' "$tmp/Casks/mayoi.rb"; then
  echo "the new wrapper retained the old-app fallback" >&2
  exit 1
fi
cp "$tmp/Casks/mayoi.rb" "$tmp/updated.rb"
sh "$tmp/scripts/update-homebrew.sh" 0.11.12
cmp "$tmp/updated.rb" "$tmp/Casks/mayoi.rb"

# Missing or duplicate fields must fail before replacing the cask.
for layout in missing duplicate unfinished; do
  case "$layout" in
    missing) sed '/^  sha256 /d' "$tmp/original.rb" ;;
    duplicate) cat "$tmp/original.rb"; printf '  version "0.11.11"\n' ;;
    unfinished) sed '/^    MAYOI_CLI=1 exec /d' "$tmp/original.rb" ;;
  esac > "$tmp/Casks/mayoi.rb"
  cp "$tmp/Casks/mayoi.rb" "$tmp/before.rb"
  if sh "$tmp/scripts/update-homebrew.sh" 0.11.12 > "$tmp/output" 2>&1; then
    echo "accepted the $layout cask layout" >&2
    exit 1
  fi
  grep -Fq 'unexpected cask layout' "$tmp/output"
  cmp "$tmp/before.rb" "$tmp/Casks/mayoi.rb"
done
echo "Homebrew release update tests passed"
