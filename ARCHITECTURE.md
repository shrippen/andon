# Architecture

How this repository is meant to be built. Where the code differs today is listed under
"Known deviations". Change this file in the PR that changes the structure. The details
of adding a rule, a tile or an integration are in `agent.md`.

## What it does

Andon is a self-hosted dashboard for several users: a start page with tiles, and the
evaluation of the user's own services (Kimai, Invoice Ninja, Snipe-IT, Dawarich and many
more) with hints about what needs attention. Workbenches write back to these services
after the user's OK.

## Building blocks

All under `internal/` unless the path starts elsewhere.

| Path | Its one task |
|---|---|
| `cmd/andon/` | The program: opens the database, wires the parts, starts the server. It may use every layer for that and decides nothing. |
| `web/` | Routes, templates, forms and static files. |
| `services/` | The application logic, one package per topic; the only place that checks a right. Among them `analysis` (load records, apply rules, sync hints), `scheduler` (background jobs), `verbund` (which connections work together), `workbench` (check steps, write after OK, audit). |
| `repos/` | The SQL, per topic. |
| `db/` | Opening the encrypted database, migrations, transactions, snapshots. |
| `sources/` | Reading a service: one cached record per connection. `demoworld/` holds the demo's records. |
| `outbound/` | Writing to a service and sending (Apprise, mail). |
| `drivers/` | The raw protocol per service: HTTP, mail, LLM. |
| `metrics/` | Pure functions: record to figures. No I/O. |
| `rules/` | Pure functions: record to finding. No I/O. |
| `widgets/` | The tiles: fields, decoding, queries, a pure view. They render what a service delivers. |
| `caps/` | What each integration can do (domain and operation), declared, no I/O. |
| `settings`, `model`, `enums`, `i18n`, `crypto`, `bayes`, `weburl`, `progress`, `logbuf` | Small packages every layer may use: operating values from the environment, types, texts, encryption, helpers. |
| `arch/` | The test that keeps the layers apart. |
| `testkit/` | Help for tests, among them tests against real instances. |
| `demo/`, `scripts/`, `tools/`, `research/`, `docs/` | Demo data and screenshots; release check and notes; measuring tools, design sync and the structure check (synced copy); notes on services; landing page. |

## Layers

```
 cmd/andon ─► web/         routes, templates, forms
                │
                ▼
             services/     application logic, rights
                │
      ┌─────────┼───────────────┬──────────────────┐
      ▼         ▼               ▼                  ▼
   repos/    sources/        outbound/      widgets ─► rules ─► metrics
   (SQL)     (read)          (write, send)  (pure; they read the records
      │         │               │            of sources, never fetch)
      ▼         └───────┬───────┘
     db/                ▼
                    drivers/   raw protocol per service
```

- A route never uses repos, sources, outbound, drivers or the database; it asks a
  service. To render it may use the pure packages (`widgets`, `metrics`, `rules`).
- A service never uses a driver. It reaches the database through `repos/`; it may
  open the transaction itself, but SQL stands in `repos/` only.
- `widgets`, `metrics` and `rules` do no I/O and know no service, repository or
  driver. Of `sources` they use the record types and pure helpers, never a fetch. A rule gets the time from
  the analysis run, not from the clock.
- The lower layers never reach up: `repos`, `sources`, `outbound` and `drivers` know
  nothing of `services`, `web` or the pure packages above them.
- `internal/arch` proves these rules for every layer on each test run.
- A right is checked in a service, nowhere else.
- A service, tile or rule that needs another service's connection asks `verbund`,
  never "the first connection in the space". A workbench always works in a Verbund.
- What an integration can do is declared in `caps` and detected by its source;
  nothing else guesses it.
- A name two places need (a setting's key, a series in the history) is a constant
  defined once. A small helper or a flow several places need is written once.
- The release build (tag `release`) carries no demo mode and no demo data; the
  release check proves it on the binary.

## Where new things go

| Change | Touch |
|---|---|
| A new rule | A function in `rules/`, its texts in both catalogs of `i18n/catalogs/`, a test |
| A new figure | `metrics/`, with a test |
| A new tile | `widgets/`, its template in `web/templates/`, its name in the catalogs |
| A new integration | Its service type in `enums/`, a driver in `drivers/`, a source in `sources/` with demo records in `sources/demoworld/`, its capabilities in `caps/declared.go`, its description in `web/serviceinfo.go`, texts in both catalogs; writing to it in `outbound/` |
| A new workbench | A service package of its own built on the shared part `services/workbench/`, its page in `web/`, its writes in `outbound/` |
| A new page or form | A route and template in `web/`, the logic in a service |
| A new table or column | A migration in `db/migrations`, its functions in `repos/` |
| A new instance setting | Its name once in `services/system/`, stored through `repos/misc/`, its form on the settings page in `web/`; an operating value from the environment in `settings/` |
| A new background job | The work in the service of its topic, the job in the list in `cmd/andon/jobs.go` |
| A design element that is missing | Kante, then `tools/sync-design.sh`; no colour outside the themes |

## Known deviations

- The names of the stored instance settings stand twice, partly as plain text
  (`AUDIT.md`, finding 1).
- Twelve rules read the system clock (`AUDIT.md`, finding 2).
- Tiles read a number from a setting in four ways (`AUDIT.md`, finding 3).
- Two services write SQL themselves: renaming a team, changing a share's right
  (`AUDIT.md`, finding 4).
- The layer test covers only `web`, `services`, `widgets`, `metrics` and `rules`;
  `outbound` uses types of `metrics` (`AUDIT.md`, finding 5).
- The names of the series in the history stand as free text where they are written
  and where they are read (`AUDIT.md`, finding 6).
- The shape of values in hints is built in five places (`AUDIT.md`, finding 7).
- "Check the certificate" has four forms in the drivers (`AUDIT.md`, finding 8).
- Twelve workbenches copied the same loading flow (`AUDIT.md`, finding 9).
- Some routes check the admin right themselves, and the service behind them
  checks nothing: the settings page, own CSS, the connection test and others
  (not in `AUDIT.md`).
