# Working rules for this repository

The rules for all shrippen repositories are in `RULES.md` (writing, code, commits, bug
fixes, repository, GUI). It is a synced copy from the rules repository; do not edit it
here. This file holds only what is particular to this repository. A Claude Code session on
the owner's machine has the rules through the global `CLAUDE.md` already and reads
`RULES.md` only for its GUI rule, before work on a user interface.

## Rules of this project

- Every visualization (chart, graph, heatmap) in a popup or page has axis labels or a legend; without them it is useless. Give it hover tooltips where possible. Tiles stay without axes and legends, so they read at a glance; their popup carries the details.

- Test integrations against a real instance where possible (`internal/testkit/live`): connections and data in `.local-test/` (outside Git), one file per service. Read tests use the user's own services; write tests never do: they write to throwaway Docker containers of the same services (`.local-test/writes/`, list in `instances.json`, started by `make live`). Write only new test entries and never write to existing ones; every write is logged in `.local-test/writes.log`. Without a configured instance the test is skipped: in cloud environments and CI, tests against fakes are enough. Live tests run only with `make live` (`ANDON_LIVE=1`). Services without an own instance run as throwaway Docker containers (`.local-test/extra/`); test against them only when the user explicitly asks for it (`scripts/live-extra.sh`).

## Andon – Projekt-Wissen

## Zweck
Selbst gehostetes Mehrbenutzer-Dashboard Andon: Startseite (Dashy-Ersatz) plus
Auswertung von Kimai, Invoice Ninja, Snipe-IT, Dawarich mit Hinweisen.
Plan und Entscheidungen: `ROADMAP.md`.

Implementierung: Go (`net/http`, `html/template`, htmx für Fragment-Lazy-Load).
Vollständig auf Go umgestellt; die frühere Python-Fassung ist nur noch in
der Git-Historie vorhanden.

## Schichten (nur zum direkten Nachbarn darunter)
```
web/        Routen, Templates, Formulare        (net/http, html/template, htmx)
  ↓
services/   Anwendungslogik, Rechteprüfung      (einzige Stelle für Right-Prüfung)
  ↓
repos/      Datenbankzugriff     sources/  Dienst-Adapter (lesen)   outbound/  Apprise (senden)
  ↓                                 ↓                                  ↓
db/         database/sql, Tx     drivers/  rohes HTTP je Dienst
```
- Routen rufen nie Repos, Sources, Outbound oder Drivers direkt auf.
- Widgets (`internal/widgets/`) rendern nur Daten, die ein Service liefert.
- Unexportierte (kleingeschriebene) Namen; Export nach außen nur mit Rückfrage.

## Konventionen
- Go (aktuelle Stable-Version), gofmt, go vet. `make check` vor jedem Commit.
- Texte nur über `t(key)` (Kataloge `internal/i18n/catalogs/*.yml`); Hinweise speichern Schlüssel + Parameter.
- CSS nur mit Theme-Tokens, keine Hex-Werte außerhalb `themes/`.
- Zugangsdaten nur verschlüsselt (`internal/crypto`), nie im Log, nie im Export.

- **Uhrzeiten nennen ihre Zone, wo der Browser abweicht:** Jede Uhrzeit, die der Server formatiert (Server-Zeit `TZ`, Andons Uhr-Zone oder die Zone einer Kachel), trägt `{{tzMark}}` bzw. `{{tzMark $v.Zone}}` dahinter, Eingabefelder für Uhrzeiten an ihrer Beschriftung; Detail-Dialoge mit Uhrzeiten setzen `Head.Zone` (`zoned(…)`). Die Marke ist versteckt und erscheint nur, wenn die Zone des Browsers anders ist (`andon.js showZones`). Vorbild: Kimai Lite.

## Bausteine
```
internal/sources/*.go           <service>.data: `var XData = source{…}`, ein gecachter Datensatz je Verbindung (+ .test)
internal/metrics/*.go           reine Funktionen: Datensatz → Kennzahlen (kein I/O)
internal/rules/*.go             Register(id, scope, defaults, run): Datensatz → Finding (kein I/O)
internal/widgets/*.go           Tile[C]: Felder, Thema, Decode(Raw), Queries, View (rein), Calm
internal/services/analysis/     Job: Datensätze laden, Regeln anwenden, hints.Sync()
internal/services/scheduler/    Background-Jobs (Timer je Job, ±5 % gestreut, panic-/error-isoliert)
internal/caps/                  Fähigkeiten je Integration: Domäne × Operation (+ Arten, Voraussetzungen), kein I/O
internal/services/verbund/      Verbünde: welche Verbindungen zusammenarbeiten (Partner) und ihre Zuordnungen
internal/services/workbench/    Werkbänke: Verbund wählen (Scopes), Schritte prüfen, nach OK gegen frische Daten schreiben, Audit; Formular `scope`, `pick`/`act_<fall>`/`<feld>_<fall>` (web/bench.go)
```
- Neue Regel: Funktion in `internal/rules/`, Texte `hint.<message>.title|why` in beiden Katalogen, Test in `internal/rules/*_test.go`.
- Neues Widget: `Tile[XConfig]{…}.add()` in `internal/widgets/` (Grenzen/Defaults nur im Field, gelesen über `Raw`), Template-Define `widgets/<key>` in `internal/web/templates/widgets_*.html`, `wtype.<key>` in den Katalogen.
- Neue Integration: ihre Fähigkeiten in `internal/caps/declared.go` deklarieren (Domäne, Operation, Arten, Voraussetzungen wie Plugin, Recht, Schnittstelle, Einstellung). Die Quelle erkennt sie mit `caps.Detect`, der Datensatz liefert sie über `CapSet()`, die Test-Quelle unter `sources.TestCaps`; Texte `caps.*` in beiden Katalogen. Wer speichert, entscheidet `caps.Store`, nicht ein eigenes Flag im Datensatz (Tests: `TestDeclaredServicesReportCaps`, `TestCapsKeysExist`). Speichert sie IDs eines anderen Dienstes, nennt `Cap.Refs` sie. Eigenschaften neben den Domänen (Webhooks, Wiederherstellungstest, Anmeldung, OAuth-Client) stehen in `caps.Traits`, nicht in eigenen Tabellen.
- Hält eine Integration dieselbe Art Daten wie eine vorhandene (Inventar, Bank), wird sie Mitglied der Kategorie (`caps.Members`) und liefert deren Form (`InventorySource`, `BankSource`); Auswertungen lesen nur über `metrics.InventoryOf`/`BankOf`, Regeln der Kategorie (`inventory.*`, `bank.*`) laufen dann von selbst, eigene Regeln nur für das, was nur sie kann. Eine neue Kategorie erst mit dem zweiten Dienst ([`CAPABILITIES.md`](CAPABILITIES.md), „Kategorien“).
- Neue Querregel über Dienste mit Fähigkeiten: `rules.Uses(id, …)` in `internal/rules/uses.go` nennt, was sie liest; daraus folgt, bei welchem Ausfall ihre Hinweise stehen bleiben.
- Dienste ohne deklarierte Fähigkeiten liest Andon bereichsweit (alle Docker-Hosts, alle Borg-Server). Liest eine Querprüfung oder Kachel einen solchen Dienst, der mehrfach vorkommen kann, bekommt sein Datensatz `Merge` (`sources.Merger`, `internal/sources/merge.go`), sonst läuft die Prüfung reihum je Verbindung.
- Braucht ein Service, eine Kachel oder eine Regel die Verbindung eines anderen Dienstes, fragt er `verbund.Partner` (bzw. `Pairs`/`Groups`), nie „die erste Verbindung im Bereich“. Daten, die nur zwischen Diensten bestehen (z. B. Kunde Kimai ↔ Invoice Ninja), sind Zuordnungen im Verbund (`link_entries`).
- Eine Werkbank arbeitet immer in einem Verbund: ihre Verbindungen kommen aus `workbench.Scopes` (Need: Pflicht- und Zusatzdienste), nie aus einer eigenen Verbindungswahl. Passen mehrere Verbünde, zeigt die Seite `bench_scopes` und merkt sich die Wahl.
- Hinweis-Parameter typisiert übergeben (`money()`, `day()`, `num()` aus `internal/i18n`).

## Gelernte Fehler
- Go 1.22+ Mux: Pfadmuster wie `/theme/{id}.css` (Wildcard + fester Suffix in einem Segment) werden nicht unterstützt ("bad wildcard segment"). Ganzes Segment als Wildcard registrieren, Suffix im Handler abschneiden.
- `html/template` kann kein Template mit zur Laufzeit berechnetem Namen einbinden (`{{template}}` braucht einen String-Literal) — anders als Jinjas `include`. Für pro-Typ-Fragmente (Widgets) daher `ExecuteTemplate` mit dynamischem Namen aus einer eigenen Route aufrufen (siehe `/widget-fragments/{id}`), nicht versuchen, es inline im Template zu lösen.
- Der SQLite-Treiber liefert TEXT-Spalten als `string`, nicht als `time.Time` — auch wenn der Wert wie ein Zeitstempel aussieht. Erst in `string` scannen, dann `db.ParseTime`.
- String-Enum mit explizitem Zero-Value versehen, wenn die Go-Zero-Value (`""`) semantisch "Standard"/"keiner" bedeuten soll (z. B. `ConnUse`s `ConnNone`); sonst weicht ein Feld, das nie explizit gesetzt wird, unbemerkt vom Default ab.
- DB-Datei ist verschlüsselt (Adiantum-VFS, Schlüssel = Argon2id(MASTER_KEY, `andon.db.salt`)); öffnen nur über `maintenance.Unlock`, Kopien nur über `db.Snapshot`/`db.OpenReadOnly` (tragen ihr `.salt` mit), nie per Dateikopie oder `sql.Open`. Neue verschlüsselte Daten: Spalte `*_enc` bzw. JSON-Schlüssel `*_enc` und Zweck in `maintenance.sealedPurpose`, sonst verweigert `rotate-key`. Reine Lesepfade über `db.WithRead`, `db.WithTx` nimmt die Schreibsperre sofort.
- Board-Freigabe muss Widgets aus dem Bereich des Boards sichtbar machen (`boards.seenRight`).
- `html/template` behandelt Attribute, deren Name ohne `data-` mit `on` beginnt (`data-on`), als Event-Handler (JS-Kontext); ein `{{if}}` darum bricht mit "branches end in different contexts". Anderen Namen wählen (`data-pinned`).
- CSS-Spalten (`column-count`): Chrome bricht den ganzen Spaltenfluss bei jeder Änderung in einem Block-Kind neu um (35 ms je Tastendruck), Firefox immer (200 ms je Fragment-Swap). Kinder als `inline-block`, und viele Einzel-Swaps im Fluss bündeln (`/boards/{id}/live`).
- Formulare mit `<select>` in eingeklappten Menüs (`details`) scannt Chrome beim Laden (1,7 s für 112 Selects). Erst beim Öffnen laden (htmx) oder aus einem `<template>` klonen.
- `window.Kante` heißt nicht, dass `shrippen.js` lief: `kante-wall.js` legt es schon beim Parsen an, die Live-API (`edit`, `tick` …) kommt erst mit DOMContentLoaded. Auf Kante nur über `andonKante` warten (prüft `Kante.edit`); sonst bricht der Bearbeitungsmodus nach einem Neuladen ab (`tools/edit-reload.py`).
- Gleichzeitige DNS-Abfragen begrenzen (`httpclient.dnsParallel`): Der Heim-Resolver verwirft ab ~30 parallelen Abfragen, jede kostet dann 5 s Timeout.

## QA rule

- Exploratory QA follows the user journeys in `QA.md`: play them in the browser against the demo (and the empty instance for the first start), note friction, illogical behaviour and bugs with evidence, and record the round in `ROADMAP.md`.
- A feature that adds or changes a flow adds or changes its journey in `QA.md`.

## Release

Release steps: `RELEASE.md`. Before a release, load the skill `release-changelog`. A release
is published, so the owner confirms it in words.

## Repository (this project)

- CI runs on the GitHub mirror (`.github/workflows/ci.yml`, since 16.10.2026) and reports back to Gitea as the commit status `github / test`. The image goes to `ghcr.io/shrippen/andon` only.
- The merge waits for: CI is green on the PR's latest commit: every CI check that runs on each push (such as `github / test` from the GitHub mirror) shows success there. No status there yet means wait, not merge; if it never comes, check the mirror or ask the owner. PR-Agent's status does not count here. Without CI, the local checks count (here `make check`).
- Everything else: `RULES.md` (Repository) and the skill `pr-review`.
