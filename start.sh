#!/usr/bin/env bash
# Starts a local dev server on http://localhost:$PORT (default 8080; dev master key, not for production).
#   ./start.sh        data in ./data
#   ./start.sh demo   demo users and boards in ./data-demo (login printed at start);
#                     Studio Weber, the demo world shared by all shrippen projects
#   DATA_DIR=... overrides the folder (screenshots use a fresh one per run)
set -euo pipefail
cd "$(dirname "$0")"

export MASTER_KEY=dev-only-not-secret
export PORT="${PORT:-8080}"
export BASE_URL="${BASE_URL:-http://localhost:$PORT}"
data_dir="${DATA_DIR:-}"
export DATA_DIR="${data_dir:-./data}"

if [ "${1:-}" = "demo" ]; then
	export ANDON_DEMO=true
	export DATA_DIR="${data_dir:-./data-demo}"
	# Same values as internal/services/seed (DemoAdmin, DemoUser, DemoPassword);
	# the server logs them only on the first start.
	echo "Demo login: mara@studio-weber.example.test (own boards) or lena@studio-weber.example.test (admin), password demo-password-1"
fi

mkdir -p "$DATA_DIR"
exec go run ./cmd/andon
