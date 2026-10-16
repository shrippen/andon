#!/bin/sh
# Starts the throwaway instances the live write tests write to
# (.local-test/writes, outside Git) and waits until each answers. Without
# them the write tests are skipped (internal/testkit/live).
set -eu
cd "$(dirname "$0")/.."

dir=.local-test/writes
list=$dir/instances.json
wait_s=300

if [ ! -f "$dir/docker-compose.yml" ] || [ ! -f "$list" ]; then
	echo "no write instances ($dir): write tests are skipped" >&2
	exit 0
fi

docker compose --project-directory "$dir" up -d --quiet-pull 2>/dev/null

# Wait until every instance answers HTTP (any status).
end=$(($(date +%s) + wait_s))
for url in $(jq -r '.[].url' "$list"); do
	until curl -ks -o /dev/null --max-time 3 "$url"; do
		if [ "$(date +%s)" -ge "$end" ]; then
			echo "no answer from $url after ${wait_s}s" >&2
			exit 1
		fi
		sleep 2
	done
done
