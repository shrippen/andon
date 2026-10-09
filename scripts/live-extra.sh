#!/bin/sh
# Runs the live tests against throwaway containers for services without
# an own instance (.local-test/extra, outside Git), only when asked for.
# Starts the containers, waits until each instance answers, runs the
# tests, and stops the containers again if this script started them.
#
#   scripts/live-extra.sh                all live tests
#   scripts/live-extra.sh Sources/grocy  only matching tests (go test -run)
set -eu
cd "$(dirname "$0")/.."

dir=.local-test/extra
list=$dir/instances.json
wait_s=180

if [ ! -f "$dir/docker-compose.yml" ] || [ ! -f "$list" ]; then
	echo "no throwaway instances: $dir/docker-compose.yml and $list missing" >&2
	exit 1
fi

# Start what is not running; stop only that afterwards.
running=$(docker compose --project-directory "$dir" ps -q --status running | wc -l)
docker compose --project-directory "$dir" up -d --quiet-pull 2>/dev/null
if [ "$running" -eq 0 ]; then
	trap 'docker compose --project-directory "$dir" stop >/dev/null 2>&1' EXIT
fi

# Wait until every instance answers HTTP (any status).
end=$(($(date +%s) + wait_s))
for url in $(jq -r '.[].url | select(startswith("http://127.0.0.1") or startswith("https://127.0.0.1"))' "$list"); do
	until curl -ks -o /dev/null --max-time 3 "$url"; do
		if [ "$(date +%s)" -ge "$end" ]; then
			echo "no answer from $url after ${wait_s}s" >&2
			exit 1
		fi
		sleep 2
	done
done

ANDON_LIVE=1 ANDON_LIVE_EXTRA=1 go test -count=1 -run "${1:-Live}" ./...
