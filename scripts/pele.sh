#!/usr/bin/env bash
# Bundles Pele, which draws Mermaid diagrams, into cmd/mayoi/pele.js.gz:
# one ES2017 script, which goja runs, that sets the global Pele, with
# Pele's license at its top. Needs Node's npx.
# Usage: scripts/pele.sh [version, 1.9.0 by default]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-1.9.0}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cd "$TMP"
npm init -y >/dev/null
npm install --silent --no-audit --no-fund "pele@$VERSION"
{
	printf '/*! Pele %s, https://github.com/obsidianmd/pele\n' "$VERSION"
	sed 's/^/ * /; s/ *$//' node_modules/pele/LICENSE
	printf ' */\n'
} >banner.js
npx --yes esbuild@0.25.0 node_modules/pele/dist/pele.js --bundle --format=iife --global-name=Pele \
	--target=es2017 --minify --legal-comments=none --outfile=pele.js
cat banner.js pele.js | gzip -9n >"$ROOT/cmd/mayoi/pele.js.gz"
echo "Pele $VERSION in cmd/mayoi/pele.js.gz ($(wc -c <"$ROOT/cmd/mayoi/pele.js.gz" | tr -d ' ') bytes)"
