# Review guide

Every PR is reviewed before it is merged (rule in `agent.md`): by PR-Agent
(owner, 22.10.2026); a new Claude agent checks the **Wichtig** of earlier
rounds, and reviews when PR-Agent fails. The
reviewer knows nothing of the session that wrote the change: it gets the
PR's intent, its diff and the repository, and this file. The owner does not read code; this review is the second reader.

## What failure looks like here

Andon is the start page and business dashboard of several users. A bug
that matters is one a user sees, or one that costs data or trust:

- **Wrong numbers:** a tile or hint with wrong hours, money, dates or
  counts, a time in the wrong zone or without `tzMark`, a hint that
  stays after its cause is gone or never appears.
- **Writes wrong:** a workbench that writes to the wrong connection or
  Verbund, writes on stale data, writes without OK, or without audit;
  a test that writes to the user's own services, not `.local-test/writes/`.
- **Leaks:** a board, widget or hint shown outside the user's rights,
  credentials in a log, export or page, demo data in a release build or
  in code instead of `internal/sources/demoworld/`.
- **Breaks the install:** a migration failing on an existing database,
  a database or backup that no longer opens with `MASTER_KEY` + `.salt`.
- **Stops the page:** a panic, a hung source blocking tile or scheduler.
- **Lies:** docs, `ROADMAP.md`, `QA.md` or comments the code contradicts.

## What to check in particular

- Layers: routes never call repos, sources, outbound or drivers; widgets
  only render service data; no new exported names without the owner.
  Rights are checked in `services/` only: a new route or fragment
  without a service call that checks the right is a leak.
- Secrets and data: credentials only through `internal/crypto`; new
  encrypted data gets a `*_enc` column and an entry in
  `maintenance.sealedPurpose` (else `rotate-key` refuses); the database
  never through `sql.Open` or a file copy.
- Texts only through `t(key)`, keys in `de.yml` and `en.yml`; hints
  store key plus typed parameters. GUI rule: Kante as it is, no hex or
  `px` outside `themes/`, no local copy of a Kante component.
- Other services through `verbund.Partner`/`Pairs`/`Groups`, never the
  first connection in scope; capabilities via `caps`, not flags.
- Bug fixes: a test that failed before the fix, and the cause removed,
  not one case hidden. A changed flow changes its journey in `QA.md`;
  a release follows the release rule (log in `CHANGELOG.md`, the same
  text in `docs/index.html` `#changes` and the Gitea release notes,
  shots `docs/shots/release-<version>-*`, the footer version,
  `versions.json` in shrippen.github.io).

## Skip

- `internal/web/static/vendor/kante/` and the Kante `tokens.css` in
  `internal/services/themes/builtin/kante/` (`tools/sync-design.sh`).
- `go.sum`, `docs/shots/` and other images; style that `gofmt`,
  `go vet` and `staticcheck` enforce.

## How to report

- Every finding names file and line and the concrete input or state that
  goes wrong. No guess without evidence; if unsure, say so.
- Each finding gets one label: **Wichtig** (a bug from the list above,
  must be fixed or answered before the merge), **Kleinigkeit** (optional,
  at most five) or **Bestand** (the bug was there before this PR; not
  blocking).
- The report starts with the count, e.g. "2 Wichtig, 1 Kleinigkeit" (the
  session puts the heading above it). The count of **Wichtig** holds every
  one still open, also from an earlier round, e.g. "1 Wichtig (offen aus
  Runde 1)". Only when none is open it says plainly: "Nichts zu beheben."
- In a later round: say for each open **Wichtig** of the last round whether
  it is fixed, or whether its answer is accepted, with the reason. PR-Agent
  sees no earlier round: it reports only its own findings with their labels,
  and a new Claude agent closes the open ones (`agent.md`).
- Findings in German, code names as they are.
