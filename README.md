# Andon

Self-hosted, multi-user dashboard for an IT landscape and a freelance business. It replaces Dashy as the start page (links, status checks, search, RSS, clock, weather) and pulls data from **Kimai**, **Invoice Ninja**, **Snipe-IT** and **Dawarich** to turn it into hints and reminders.

- Own login (password, TOTP, API tokens, invitations)
- Users and teams, permissions down to single widgets, personal layouts
- Configuration editor with history
- Theme system on top of the [Kante design system](https://github.com/shrippen/shrippen.github.io); only the Kante theme ships
- German and English
- Push notifications through an existing Apprise API instance
- One Docker container, SQLite in `/data`, pure Go (no cgo — runs on a Raspberry Pi without a C toolchain)
- Database file, WAL and backups encrypted (Adiantum); the key comes from `MASTER_KEY` through Argon2id with a salt per install (`andon.db.salt`). A guessable `MASTER_KEY` is refused. Older databases move to this key at first start

Landing page: <https://shrippen.github.io/andon/>. Plan and decisions: [ROADMAP.md](ROADMAP.md) (German). Working rules: [agent.md](agent.md).

## Run

```sh
mkdir -p secrets data && openssl rand -base64 32 > secrets/master_key
cp docker-compose.example.yml docker-compose.yml   # adjust BASE_URL, SMTP, proxy range
docker compose up -d
docker compose logs andon | grep "SETUP CODE"      # open /setup and enter the code
```

Images: `ghcr.io/shrippen/andon`, public, built by the GitHub mirror.
The same tags go to the private `git.arianw.de/shrippen/andon`.
`latest` is the newest release, `edge` follows `main` (development
state). A tag `v1.2.3` publishes `latest`, `1.2.3`, `1.2` and `1`; set
`ANDON_TAG=1.2` in `.env` to pin production to a release line. Keep `secrets/master_key` safe and
separate from backups: without it the database can't be opened. `data/andon.db.salt` is
not secret but just as needed: back it up with the database (every backup archive and copy
in `data/backups` carries its own `.salt`).

The container starts as root, hands `/data` to `PUID:PGID` (default
`10001:10001`) and reads the secret files, then runs Andon as that
user. No `chown` needed on the host.

## Sign in instead of a token

Home Assistant, Nextcloud, Jellyfin (Quick Connect), Gitea, Snipe-IT and
Tailscale can also sign in from the connection page, next to the token
field. Gitea and Snipe-IT need an OAuth client created once in the service
with the redirect URL shown there (`BASE_URL/connections/connect/callback`);
Tailscale needs an OAuth client with `devices:core:read`. Renewing tokens
(Gitea, Snipe-IT, Tailscale) are refreshed by Andon itself.

## Develop

```sh
make run      # http://localhost:8080, data in ./data, dev master key
make check    # gofmt, go vet, go test
make build    # release binary in ./bin/andon (-tags release, checked for demo data)
```

Layers: `web → services → repos | sources | outbound → db | drivers`. See `agent.md`.

### Demo

`./start.sh demo` starts with made-up users, boards and demo:// connections in `./data-demo`.
Names, places and receipts come from Studio Weber, the demo world shared by all shrippen
projects (`internal/sources/demoworld/world.json`, copied from `shrippen.github.io/demo`).
Sign in as `mara@studio-weber.example.test` (own boards) or `lena@studio-weber.example.test`
(admin), password `demo-password-1`. `demo/shots.json` lists the screenshots that
`shrippen.github.io/demo/tools/screenshots.py` takes.

Release builds (`-tags release`: Docker image, `make build`) leave the demo mode and
Studio Weber out; gallery previews then use `sample.json`, which `demo/make-sample.py`
derives from `world.json`. `scripts/release-check.sh` fails the build on any rest.

## Operator CLI

Run as the same user (`-u` = your `PUID`), so no files end up owned by root.

```sh
docker compose exec -u 10001 andon andon backup /data/backups
docker compose exec -u 10001 andon andon rotate-key /run/secrets/new_master_key
```

`rotate-key` writes `andon.db.rekeyed`, a copy with every secret
re-encrypted under the new key; the live database stays as it is. The
next start with the new key swaps the copy in; a start with the old key
keeps the old database. Replace the secret and restart right away:
writes in between are lost. Older backups keep the old key, and webhook
URLs change with the key.
