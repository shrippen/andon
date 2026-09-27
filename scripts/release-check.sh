#!/bin/sh
# Fails when a release binary carries the demo mode or Studio Weber, the
# demo world (names of the studio, its people, customers, vendors and
# projects, the demo login). Usage: scripts/release-check.sh <binary>
set -eu
bin="$1"
world="$(dirname "$0")/../internal/sources/demoworld/world.json"

# Fixed markers plus every identifying name from the world.
markers="ANDON_DEMO
DEMO MODE
demo-password
studio-weber"
if [ -r "$world" ]; then
	markers="$markers
$(python3 - "$world" <<'PY'
import json, sys
w = json.load(open(sys.argv[1], encoding="utf-8"))
names = {w["studio"]["name"]}
names |= {p["name"] for p in w["people"]}
names |= {c["name"] for c in w["customers"]}
names |= {v["name"] for v in w["vendors"]}
names |= {r["vendor"] for r in w["receipts"]}
names |= {p["short"] for p in w["projects"] if p.get("short")}
# Short or generic project names ("Admin", "Showreel") are ordinary words.
generic = {"Showreel"}
print("\n".join(sorted(n for n in names if len(n) >= 8 and n not in generic)))
PY
)"
fi

found=0
printf '%s\n' "$markers" | while IFS= read -r m; do
	[ -n "$m" ] || continue
	if grep -aqF -- "$m" "$bin"; then
		echo "release-check: \"$m\" found in $bin" >&2
		exit 1
	fi
done || found=1
if [ "$found" -ne 0 ]; then
	exit 1
fi
echo "release-check: no demo data in $bin"
