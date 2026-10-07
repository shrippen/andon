- When writing something intended for human consumption, (comment, commit message, reply to prompt) use as few words as possible. Pick every word meticulously to reduce the volume to a strict minimum. Be down to the point. Less is more.

- Avoid superlatives and praise. Stop telling me I am absolutely right. Give me the cold hard truth.

- Avoid magic numbers and strings by extracting recurring or meaningful values into descriptive constants (const) or enums. Keep self-explanatory, one-off values inline to avoid clutter. If a value comes from a spec (e.g. HTTP 200 OK), use a constant regardless.

- Reduce code indentation. Avoid Arrow Anti-Pattern. Leverage early return and continue.

- Keep function names short. Less than 30 characters.

- Use enums instead of booleans for function parameters.

- Let the reader of the code breathe. Add empty lines between logical blocks of code.

- Add a small, to the point, comment to explain *what* the block does and *why*. Use examples when possible. Propose ASCII drawings to explain complete systems.

- Treat member visibility changes as a breaking design shift. Keep all fields and functions private unless external access is strictly required by the design. Prompt the user for explicit approval before changing any access modifier from private to internal or public.

- Program to levels of abstraction. Lower-level mechanics (e.g., raw hardware I/O, sector parsing, direct socket streams) must be encapsulated in a dedicated driver/abstraction layer. Expose clean, high-level APIs to the rest of the application so calling code works with domain concepts, not raw implementation details.

- Don't touch blocks of code unrelated to the feature you implement. e.g. Don't add comments to a block of code if you did not create it or modify it. As much as possible try to minimize the number of changed lines when implementing a feature.

- Strictly adhere to the layered boundary hierarchy: each layer may only communicate with its immediate neighbor directly below it. Never "punch holes" through layers (e.g., controllers or UI components must never directly call database queries, raw hardware drivers, or low-level network clients; always route through the intermediate service/abstraction layer).

- Always use {}, even on a one-line "if" statement.

- Every visualization (chart, graph, heatmap) in a popup or page has axis labels or a legend; without them it is useless. Give it hover tooltips where possible. Tiles stay without axes and legends, so they read at a glance; their popup carries the details.

- Test integrations against a real instance where possible (`internal/testkit/live`): connections and data in `.local-test/` (outside Git), one file per service. Write only new test entries and never write to existing ones; every write is logged in `.local-test/writes.log`. Without a configured instance the test is skipped: in cloud environments and CI, tests against fakes are enough. Live tests run only with `make live` (`ANDON_LIVE=1`). Write tests against Invoice Ninja run only when the user explicitly asks for it (`ANDON_LIVE_NINJA=1`): every run leaves gaps in its invoice and expense numbers.

When you write a commit message, follow these 7 rules:
Rule 1: Separate the subject line from the body with a single blank line.
Rule 2: Limit the subject line to 50 characters (72 is the absolute hard limit).
Rule 3: Capitalize the first letter of the subject line.
Rule 4: Do not end the subject line with a period.
Rule 5: Use the imperative mood in the subject line (e.g., "Fix bug," "Add feature," 
        not "Fixed" or "Adds"). Test formula: It must complete the sentence: "If applied,
        this commit will [your subject line here]".
Rule 6: Wrap the body text manually at 72 characters to prevent Git formatting issues.
Rule 7: Use the body to explain what and why vs. how. Assume the code explains the how;
        the message must explain the context and reasoning. 

- If the prompt indicates that a bug is being fixed, don't write the fix right away. First write the test. Observe it failing. Then write the fix. And observe the test passing.

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
```
- Neue Regel: Funktion in `internal/rules/`, Texte `hint.<message>.title|why` in beiden Katalogen, Test in `internal/rules/*_test.go`.
- Neues Widget: `Tile[XConfig]{…}.add()` in `internal/widgets/` (Grenzen/Defaults nur im Field, gelesen über `Raw`), Template-Define `widgets/<key>` in `internal/web/templates/widgets_*.html`, `wtype.<key>` in den Katalogen.
- Neue Integration: ihre Fähigkeiten in `internal/caps/declared.go` deklarieren (Domäne, Operation, Arten, Voraussetzungen wie Plugin, Recht, Schnittstelle, Einstellung). Die Quelle erkennt sie mit `caps.Detect`, der Datensatz liefert sie über `CapSet()`, die Test-Quelle unter `sources.TestCaps`; Texte `caps.*` in beiden Katalogen. Wer speichert, entscheidet `caps.Store`, nicht ein eigenes Flag im Datensatz (Tests: `TestDeclaredServicesReportCaps`, `TestCapsKeysExist`). Speichert sie IDs eines anderen Dienstes, nennt `Cap.Refs` sie. Eigenschaften neben den Domänen (Webhooks, Wiederherstellungstest, Anmeldung, OAuth-Client) stehen in `caps.Traits`, nicht in eigenen Tabellen.
- Neue Querregel über Dienste mit Fähigkeiten: `rules.Uses(id, …)` in `internal/rules/uses.go` nennt, was sie liest; daraus folgt, bei welchem Ausfall ihre Hinweise stehen bleiben.
- Dienste ohne deklarierte Fähigkeiten liest Andon bereichsweit (alle Docker-Hosts, alle Borg-Server). Liest eine Querprüfung oder Kachel einen solchen Dienst, der mehrfach vorkommen kann, bekommt sein Datensatz `Merge` (`sources.Merger`, `internal/sources/merge.go`), sonst läuft die Prüfung reihum je Verbindung.
- Braucht ein Service, eine Kachel oder eine Regel die Verbindung eines anderen Dienstes, fragt er `verbund.Partner` (bzw. `Pairs`/`Groups`), nie „die erste Verbindung im Bereich“. Daten, die nur zwischen Diensten bestehen (z. B. Kunde Kimai ↔ Invoice Ninja), sind Zuordnungen im Verbund (`link_entries`).
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
- Gleichzeitige DNS-Abfragen begrenzen (`httpclient.dnsParallel`): Der Heim-Resolver verwirft ab ~30 parallelen Abfragen, jede kostet dann 5 s Timeout.

## QA rule

- Exploratory QA follows the user journeys in `QA.md`: play them in the browser against the demo (and the empty instance for the first start), note friction, illogical behaviour and bugs with evidence, and record the round in `ROADMAP.md`.
- A feature that adds or changes a flow adds or changes its journey in `QA.md`.

## Release rule

- Every release gets a detailed release log, written before the tag: a lead (what the release is about), then the changes grouped as New, Improved, Fixed and Note (breaking changes, steps the user must take). Each point says what changed for the user and why, not which code moved.
- Graphics wherever possible: for every visible change a screenshot from the demo world in every language of the landing page (`docs/shots/release-<version>-<name>-<lang>.webp`), for changes inside a diagram (Kante `.flow` or an inline SVG).
- The log goes into the release notes on Gitea and as a new entry at the top of `#changes` on the landing page (`docs/index.html`, Kante `.changelog`); from the fourth entry on, the oldest move into `details.changelog-more`. Bump the version in the footer. A release is done when the landing page shows it.

## GUI rule

- Every GUI of this project is generated from Kante, not inspired by it: landing pages,
  web apps, Qt Quick / Kirigami apps, Plasma widgets, dialogs, e-mail and print layouts.
  Source: https://github.com/shrippen/Kante (checkout `../Kante`).
  Web: link `https://shrippen.github.io/v1/shrippen.css` and `shrippen.js`, or vendor them
  unchanged. Apps: copy `qml/Kante` (and `KantePlasma` for Plasma widgets) unchanged.
- Use Kante's tokens, roles, components, classes, QML components and motion as they are.
  No own colours, fonts, sizes, radii, cuts, shadows, animation timings, no own copy or
  variant of a component that Kante has. Raw values (`#hex`, `px` for controls) are a bug;
  use roles (`--primary`, `--focus`, `--warn`, `KanteStyle.*`).
- A missing element is added to Kante first (CSS or QML, docs, catalogue), then used here.
  Never solve it locally in this project and never wait with a "temporary" copy.
- Exception: Kimai plugins take their GUI from Knust (`kimai/knust/` in shrippen/Kante), the
  Kante spinoff that adapts Kante to Kimai's look. The same rule applies to Knust: use it
  as it is, and add missing elements to Knust.
- A project without a GUI (library, CLI, scripts) has nothing to do here.
- Rule text: https://github.com/shrippen/Kante/blob/main/AGENT-RULE.md

## Repository rule

- This repository lives on Gitea (`git.arianw.de`). GitHub is only a push mirror of it.
- Changes arrive as pull requests: work on a branch, open a PR, merge it on Gitea (the mirror follows).
- Never merge a PR, push to `main` (or any default branch), push tags or publish releases on GitHub. A merge there is overwritten by the next Gitea push.
- Never force-push a branch that someone else's PR depends on.
