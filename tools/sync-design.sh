#!/bin/sh
# Vendors Kante, the shrippen design system, into Andon, unchanged.
# Usage: tools/sync-design.sh [path-to-shrippen.github.io]   (or KANTE_DIR=...)
#
#   <kante>/kante/css/base.css        -> internal/web/static/vendor/kante/base.css
#   <kante>/kante/css/components.css  -> internal/web/static/vendor/kante/components.css
#   <kante>/kante/js/shrippen.js      -> internal/web/static/vendor/kante/shrippen.js
#   <kante>/kante/fonts/OFL.txt       -> internal/web/static/vendor/kante/fonts/OFL.txt
#   <kante>/kante/fonts/*.woff2       -> internal/web/static/vendor/kante/fonts/ (when the checkout has them)
#   <kante>/kante/tokens/variables.css (:root and light theme)
#                                     -> internal/services/themes/builtin/kante/tokens.css
#   VERSION                           source, commit and files of the copy
#
# Kante ships TTF fonts. Andon serves Latin subsets as WOFF2 (about 25 kB
# instead of 360 kB each), so the existing WOFF2 files stay unless the
# checkout has its own. base.css is vendored but not linked: its reset
# and language rules are for landing pages.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
src="${1:-${KANTE_DIR:-}}"
if [ -z "$src" ] || [ ! -d "$src/kante" ]; then
	echo "usage: $0 <path to a shrippen.github.io checkout>  (or set KANTE_DIR)" >&2
	exit 1
fi
src="$(cd "$src" && pwd)"

dest="$root/internal/web/static/vendor/kante"
tokens="$root/internal/services/themes/builtin/kante/tokens.css"
mkdir -p "$dest/fonts"

cp "$src/kante/css/base.css" "$src/kante/css/components.css" "$src/kante/js/shrippen.js" "$dest/"
cp "$src/kante/fonts/OFL.txt" "$dest/fonts/OFL.txt"
for f in "$src"/kante/fonts/*.woff2; do
	[ -e "$f" ] && cp "$f" "$dest/fonts/"
done

# The theme contract is the two blocks Andon's theme parser reads (:root
# and :root[data-theme="light"]); the apps' "Kante Light" blocks follow the
# marker and do not apply here.
marker="Kante Light (apps)"
line="$(grep -n "$marker" "$src/kante/tokens/variables.css" | head -n 1 | cut -d: -f1)"
if [ -n "$line" ]; then
	head -n "$((line - 3))" "$src/kante/tokens/variables.css" > "$tokens"
else
	cp "$src/kante/tokens/variables.css" "$tokens"
fi

commit="$(git -C "$src" rev-parse HEAD 2>/dev/null || echo unknown)"
branch="$(git -C "$src" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
dirty=""
if [ -n "$(git -C "$src" status --porcelain kante 2>/dev/null)" ]; then
	dirty=" plus uncommitted changes"
fi
# The newest "Added in Kante X.Y" heading of Kante's README names the version.
version="$(grep -o '^### Added in Kante [0-9.]*' "$src/kante/README.md" | tail -n 1 | awk '{print $NF}')"
cat > "$dest/VERSION" <<VER
Kante ${version:-unknown}
source: https://github.com/shrippen/shrippen.github.io (kante/)
branch: $branch
commit: $commit$dirty
files: base.css components.css shrippen.js fonts/OFL.txt fonts/*.woff2 (Latin subsets, see tools/sync-design.sh)
VER

echo "Kante synced from $src ($commit)"
