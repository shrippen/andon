# Changelog

The release notes of Andon, newest first. Each section is the text of the release on Gitea and of
its entry in `#changes` on the landing page (`docs/index.html`).

## [0.8.0] — 2026-11-03

### English

Andon now does the paperwork with you instead of only pointing at it. Workbenches take the open cases of a service, propose what to write, show every step before anything leaves Andon, and write only what you confirmed, checked again against fresh data. The largest is the VAT check on Invoice Ninja's bank feed, up to the ELSTER figures.

#### New
- Workbenches, in their own menu with an overview and favourites: VAT check, receipt hunt, EÜR lines, close the week in Kimai, reminders on Invoice Ninja's schedule, subscriptions, Sure categories, vendor clean-up, updates as pull requests, inventory of the devices on the network, complete purchases and release feeds. Each one works inside a bundle and shows what it would write before it writes.
- VAT check: the period's bank transactions from Invoice Ninja (with Sure beside it) are linked to expenses, invoices and payments, or become new expenses with scan, EÜR line and invoice number; a debit can be split over several receipts, receipts without an expense become one. Period cards show what is open and due; the ELSTER figures and the recapitulative statement (ZM) are ready to copy.
- Work in progress stays: every change on a workbench is kept as a draft, so closing the page loses nothing. A draft gives way when its proposal changes (another reminder stage, another tag, another amount).
- "Review this one" on the VAT check writes a single transaction while the others wait.
- Accounts in a bundle are business, private, mixed, clearing, reserve or ignored; the reserve covers VAT and income tax before free cash does.
- Write journal: every write of a workbench, with undo where the service allows it.
- Year: EÜR preparation with depreciation, home office and the documents for the tax return; analyses of client health, price increases and project post-calculation; a year review and "Ask Andon".
- Photovoltaics from Home Assistant: yield, own use and feed-in as a tile, with hints for a low or missing yield, an idle battery and falling own use.
- AI costs: token use and cost of the LLM providers (xAI included), and a hint when a provider charges without an expense in Invoice Ninja.
- Kept as a second inventory beside Snipe-IT: deadlines, receipts, warranties, location and room of new devices.
- Homelab cost per service, a dismissible notice for admins when a newer Andon is out, and moving a connection to another server (or taking over from another connection) without losing tiles, history or notes.

#### Improved
- FRITZ!Box reads everything over TR-064; Vikunja 2, current Ghostfolio versions and GoToSocial are recognised.
- Number format in the profile: amounts read and typed as "1.234,56" or "1,234.56", independent of the language.
- One loading feedback everywhere; dialogs show texts instead of keys; counts use the singular ("1 hint").
- Fewer and truer hints from real data: duplicates removed, reverse charge named as such, deleted Invoice Ninja entries ignored.
- The container dialog opens in 2 instead of 10 seconds; Vaultwarden keeps its admin session.

#### Fixed
- The edit mode broke after reloading a board.
- Snipe-IT costs in German format were misread.
- A typo in a rule threshold ("18.40" in German) saved a rule that fired on every value; it is now refused.
- Status checks asked DNS twice when many ran at once.

#### Note
- The Claude plan integration is gone: it read an undocumented claude.ai endpoint, and Anthropic suspended the account it read. Migration 0025 deletes such connections; AI costs come from the providers' admin APIs.
- The EÜR line of an expense comes from its vendor's last expense, not from the category: one category belongs to several lines. The connection option "euer_map" is no longer read.
- Rules of a category are named after it: snipe.* becomes inventory.*, sure.* and firefly.* become bank.*. Migration 0026 moves hints and rule settings along.
- The image is published only on ghcr.io/shrippen/andon, signed. Migrations 0025 to 0027 run at start.
- Workbenches write to your services. They were tested against throwaway containers of the services, not yet over months of real bookkeeping: read each review before you confirm.

![The workbenches overview (demo data)](https://shrippen.github.io/andon/shots/release-0.8.0-benches-en.webp)

![The VAT check: list and desk of one transaction (demo data)](https://shrippen.github.io/andon/shots/release-0.8.0-vat-en.webp)

![One transaction reviewed below its desk (demo data)](https://shrippen.github.io/andon/shots/release-0.8.0-vat-one-en.webp)

### Deutsch

Andon erledigt den Papierkram jetzt mit dir, statt nur darauf zu zeigen. Werkbänke nehmen die offenen Fälle eines Dienstes, schlagen vor, was zu schreiben ist, zeigen jeden Schritt, bevor etwas Andon verlässt, und schreiben nur, was du bestätigt hast, noch einmal gegen frische Daten geprüft. Die größte ist der USt-Abgleich auf Invoice Ninjas Bankumsätzen, bis zu den ELSTER-Kennzahlen.

#### Neu
- Werkbänke, in einem eigenen Menü mit Übersicht und Favoriten: USt-Abgleich, Belegjagd, EÜR-Zeilen, Woche in Kimai schließen, Mahnlauf nach Invoice Ninjas Mahnplan, Abos, Sure-Kategorien, Lieferanten aufräumen, Updates als Pull Requests, Inventur der Geräte im Netz, Anschaffungen vervollständigen und Release-Feeds. Jede arbeitet in einem Verbund und zeigt, was sie schreiben würde, bevor sie schreibt.
- USt-Abgleich: Die Bankumsätze des Zeitraums aus Invoice Ninja (mit Sure daneben) werden mit Ausgaben, Rechnungen und Zahlungen verknüpft oder zu neuen Ausgaben mit Beleg, EÜR-Zeile und Rechnungsnummer; eine Abbuchung lässt sich auf mehrere Belege aufteilen, Belege ohne Ausgabe werden zu einer. Zeitraum-Karten zeigen, was offen und fällig ist; ELSTER-Kennzahlen und Zusammenfassende Meldung stehen zum Kopieren bereit.
- Zwischenstand bleibt: Jede Änderung in einer Werkbank wird gesichert, das Schließen der Seite verliert nichts. Ändert sich der Vorschlag (andere Mahnstufe, anderer Tag, anderer Betrag), gilt wieder der neue.
- „Nur diesen prüfen“ im USt-Abgleich schreibt einen einzelnen Umsatz, die anderen warten.
- Konten im Verbund sind geschäftlich, privat, gemischt, Verrechnung, Rücklage oder ignoriert; die Rücklage deckt Umsatz- und Einkommensteuer, bevor freies Geld es tut.
- Schreib-Journal: jeder Schreibvorgang einer Werkbank, mit Rückgängig, wo der Dienst es erlaubt.
- Jahr: EÜR-Vorbereitung mit AfA, Homeoffice und den Unterlagen für die Steuererklärung; Auswertungen zu Kundenzustand, Preiserhöhung und Nachkalkulation; Jahresrückblick und „Frag Andon“.
- Photovoltaik aus Home Assistant: Ertrag, Eigenverbrauch und Einspeisung als Kachel, mit Hinweisen bei niedrigem oder fehlendem Ertrag, ruhendem Speicher und sinkendem Eigenverbrauch.
- KI-Kosten: Token-Nutzung und Kosten der LLM-Anbieter (auch xAI), und ein Hinweis, wenn ein Anbieter abbucht, ohne dass in Invoice Ninja eine Ausgabe steht.
- Kept als zweites Inventar neben Snipe-IT: Fristen, Belege, Garantien, Ort und Raum neuer Geräte.
- Homelab-Kosten je Dienst, ein ausblendbarer Hinweis für Admins, wenn eine neuere Andon-Version da ist, und Verbindungen auf einen anderen Server umziehen (oder von einer anderen übernehmen), ohne Kacheln, Verlauf oder Notizen zu verlieren.

#### Verbessert
- FRITZ!Box liest alles über TR-064; Vikunja 2, aktuelle Ghostfolio-Versionen und GoToSocial werden erkannt.
- Zahlenformat im Profil: Beträge lesen und tippen sich als „1.234,56“ oder „1,234.56“, unabhängig von der Sprache.
- Ein Ladefeedback überall; Dialoge zeigen Texte statt Schlüssel; Zähltexte kennen die Einzahl („1 Hinweis“).
- Weniger und treffendere Hinweise aus echten Daten: Doppelte entfernt, Reverse Charge als solches benannt, gelöschte Einträge in Invoice Ninja übergangen.
- Der Container-Dialog öffnet in 2 statt 10 Sekunden; Vaultwarden behält seine Admin-Sitzung.

#### Behoben
- Der Bearbeitungsmodus brach nach dem Neuladen eines Boards ab.
- Snipe-IT-Kosten im deutschen Format wurden falsch gelesen.
- Ein Tippfehler in der Schwelle einer Regel („18.40“ auf Deutsch) speicherte eine Regel, die bei jedem Wert anschlug; er wird jetzt abgelehnt.
- Statusprüfungen fragten DNS doppelt, wenn viele gleichzeitig liefen.

#### Hinweis
- Die Claude-Abo-Integration ist entfernt: Sie las einen undokumentierten Endpunkt von claude.ai, und Anthropic hat das gelesene Konto gesperrt. Migration 0025 löscht solche Verbindungen; KI-Kosten kommen aus den Admin-APIs der Anbieter.
- Die EÜR-Zeile einer Ausgabe kommt von der letzten Ausgabe ihres Lieferanten, nicht von der Kategorie: Eine Kategorie gehört zu mehreren Zeilen. Die Verbindungsoption „euer_map“ wird nicht mehr gelesen.
- Regeln einer Kategorie heißen nach ihr: aus snipe.* wird inventory.*, aus sure.* und firefly.* wird bank.*. Migration 0026 zieht Hinweise und Regeleinstellungen mit.
- Das Image erscheint nur noch auf ghcr.io/shrippen/andon, signiert. Die Migrationen 0025 bis 0027 laufen beim Start.
- Werkbänke schreiben in deine Dienste. Getestet wurden sie gegen Wegwerf-Container der Dienste, noch nicht über Monate echter Buchhaltung: Lies jede Prüfung, bevor du bestätigst.

![Die Übersicht der Werkbänke (Demodaten)](https://shrippen.github.io/andon/shots/release-0.8.0-benches-de.webp)

![Der USt-Abgleich: Liste und Arbeitsfläche eines Umsatzes (Demodaten)](https://shrippen.github.io/andon/shots/release-0.8.0-vat-de.webp)

![Ein einzelner Umsatz geprüft, unter seiner Arbeitsfläche (Demodaten)](https://shrippen.github.io/andon/shots/release-0.8.0-vat-one-de.webp)

```
Change a case → draft (per user, bundle, period) → review → write, checked against fresh data → journal
```

All changes / Alle Änderungen: https://github.com/shrippen/andon/compare/v0.7.0...v0.8.0 · Changelog: https://shrippen.github.io/andon/#v0.8.0

## [0.7.0] — 2026-10-07

### English

This release clears the friction a QA round over full user journeys found: fifteen points, each decided with drafts first. On top come a wall display that changes between screen-filling sets, a phone layout, and the IT docs drawn into Homelable.

#### New
- Wall display without scrolling: tiles come in sets that fill one screen, changed with a transition (Kante cut, cross-fade, tile by tile, split-flap, shutter, scan line or all in turn) and an easing of your choice; time, transition and easing in the board settings, with a preview. Every set is at least a third full, so a full set is never followed by a lone tile.
- Phone layout: a drawer instead of three lines of navigation (54 instead of 294 px before the content), and tables become cards with the amount top right.
- Homelable as a view of the IT docs: Andon draws devices, services, networks and dependencies from the Obsidian vault and the compose stacks into its own canvas, sends only changes and never positions, so your layout stays.
- The docs meet what runs: stacks in the repo but not deployed, deployed stacks without a repo, and notes whose URL, ports or image drift from the compose file or from Komodo.
- New cross rules: a deploy on red CI, a PBS datastore on a nearly full TrueNAS pool, a UPS whose runtime no longer covers its hosts, and charging costs for business trips (EVCC × rides). The ISP report lists the FRITZ!Box reconnects with their downtime.
- From a connection to a tile in one click: "Add to board" in the library, starter tiles on an empty board, and matching tiles offered after a green connection test.

#### Improved
- Hints page with a filter sidebar (levels and services with counts) and "Grouped / Single"; groups only from two hints, all as compact rows. On a phone the first hint starts at 385 instead of 1614 px.
- Forms keep what you typed when something is refused (never passwords, tokens or codes), also after a stale version, and say "Saved" after a save.
- New connection: services in groups with search; test errors translated with cause and link; a new token is tested at once.
- Money pages name their period and let you choose "12 months / Year / Last year"; year-over-year and seasonal comparisons only with full history; subscriptions as monthly amounts.
- Hosts count their hints as problems; billing names an open draft of the same client; tiles with Sure as partner also read Firefly III.
- English throughout: "Instance" and "Bundle", rule titles instead of IDs, a DE/EN switch in the footer of every page, the login included.

#### Fixed
- Receipts: tab counters matched their lists only by chance, "Linked: 0" showed as success, the tab was lost after "Create", and deleted expenses went into the tax export.
- Fields had no visible keyboard focus on a light ground; htmx copied inline styles that the CSP refused.
- The mileage formula named all business km, though bike rides get no km rate.

#### Note
- Registration no longer logs in: it answers the same for free and taken addresses, so nobody learns which exist. With mail, a taken address gets "You already have an account".
- Only admins open the instance settings, connections and bundles of the instance; others see a translated refusal with the reason. Buttons without the right are hidden.
- The database gains three board columns for the wall display (migration 0024, runs at start). Homelable, the Komodo comparison and the new cross rules were tested against fakes and the demo world, not yet against real instances.

![Hints with a filter sidebar (demo data)](https://shrippen.github.io/andon/shots/release-0.7.0-hints-en.webp)

![Wall display: one set per screen (demo data)](https://shrippen.github.io/andon/shots/release-0.7.0-wall-en.webp)

![Tables as cards on a phone](https://shrippen.github.io/andon/shots/release-0.7.0-phone-cards-en.webp)

![The navigation drawer](https://shrippen.github.io/andon/shots/release-0.7.0-phone-drawer-en.webp)

![Homelable sync on the connection record](https://shrippen.github.io/andon/shots/release-0.7.0-homelable-en.webp)

![Reconnects in the ISP report (demo data)](https://shrippen.github.io/andon/shots/release-0.7.0-isp-en.webp)

### Deutsch

Diese Version räumt die Reibung ab, die eine QA-Runde über ganze Nutzerwege gefunden hat: fünfzehn Punkte, jeder vorher mit Entwürfen entschieden. Dazu kommen eine Wandanzeige, die zwischen bildschirmfüllenden Sätzen wechselt, ein Layout fürs Handy und die IT-Doku als Grafik in Homelable.

#### Neu
- Wandanzeige ohne Scrollen: Kacheln kommen in Sätzen, die einen Bildschirm füllen, gewechselt mit einem Übergang (Kante-Schnitt, Überblenden, Kachel für Kachel, Fallblatt, Rollladen, Scanlinie oder alle abwechselnd) und einem Verlauf nach Wahl; Zeit, Übergang und Verlauf in den Board-Einstellungen, mit Vorschau. Jeder Satz ist mindestens zu einem Drittel gefüllt, nie ein voller Satz und danach eine einzelne Kachel.
- Handy-Layout: eine Schublade statt drei Zeilen Navigation (54 statt 294 px vor dem Inhalt), und Tabellen werden Karten mit dem Betrag oben rechts.
- Homelable als Ansicht der IT-Doku: Andon zeichnet Geräte, Dienste, Netze und Abhängigkeiten aus dem Obsidian-Vault und den Compose-Stacks in einen eigenen Canvas, sendet nur Änderungen und nie Positionen, dein Layout bleibt.
- Die Doku trifft, was läuft: Stacks im Repo, aber nicht deployt, deployte Stacks ohne Repo, und Notizen, deren URL, Ports oder Image von der Compose-Datei oder von Komodo abweichen.
- Neue Querregeln: ein Deploy auf roter CI, ein PBS-Datastore auf einem fast vollen TrueNAS-Pool, eine USV, deren Laufzeit ihre Hosts nicht mehr deckt, und Ladekosten für Dienstfahrten (EVCC × Fahrten). Der ISP-Bericht nennt die FRITZ!Box-Neuverbindungen mit Ausfallzeit.
- Von der Verbindung zur Kachel mit einem Klick: „Auf Board legen“ in der Bibliothek, Startkacheln auf einem leeren Board und passende Kacheln nach einem grünen Verbindungstest.

#### Verbessert
- Hinweisseite mit Filter-Seitenleiste (Stufen und Dienste mit Zählern) und „Gruppiert / Einzeln“; Gruppen erst ab zwei Hinweisen, alles als kompakte Zeilen. Am Handy beginnt der erste Hinweis bei 385 statt 1614 px.
- Formulare behalten das Getippte, wenn etwas abgelehnt wird (nie Passwörter, Tokens oder Codes), auch bei einer veralteten Fassung, und sagen „Gespeichert“ nach dem Speichern.
- Neue Verbindung: Dienste in Gruppen mit Suche; Testfehler übersetzt mit Ursache und Link; ein neuer Token wird sofort getestet.
- Geld-Seiten nennen ihren Zeitraum und lassen „12 Monate / Jahr / Vorjahr“ wählen; Vorjahres- und Saisonvergleich nur bei voller Historie; Abos als Monatsbetrag.
- Hosts zählen ihre Hinweise als Probleme; die Abrechnung nennt einen offenen Entwurf desselben Kunden; Kacheln mit Sure als Partner lesen auch Firefly III.
- Englisch durchgehend: „Instance“ und „Bundle“, Regeltitel statt IDs, ein DE/EN-Umschalter im Fuß jeder Seite, auch auf der Anmeldung.

#### Behoben
- Belege: Reiter-Zähler passten nur zufällig zu den Listen, „Verknüpft: 0“ erschien als Erfolg, der Reiter ging nach „Anlegen“ verloren, und gelöschte Ausgaben landeten im Steuer-Export.
- Felder hatten auf hellem Grund keinen sichtbaren Tastatur-Fokus; htmx kopierte Inline-Styles, die die CSP ablehnte.
- Die Kilometerformel nannte alle beruflichen km, obwohl Radfahrten keine Pauschale bekommen.

#### Beachten
- Die Registrierung meldet nicht mehr an: Sie antwortet für freie und vergebene Adressen gleich, damit niemand erfährt, welche es gibt. Mit Mailversand bekommt eine vergebene Adresse „Du hast schon ein Konto“.
- Nur Admins öffnen Einstellungen, Verbindungen und Verbünde der Instanz; andere sehen eine übersetzte Ablehnung mit Grund. Knöpfe ohne Recht sind ausgeblendet.
- Die Datenbank bekommt drei Board-Spalten für die Wandanzeige (Migration 0024, läuft beim Start). Homelable, der Komodo-Abgleich und die neuen Querregeln sind gegen Nachbauten und die Demowelt getestet, noch nicht gegen echte Instanzen.

![Hinweise mit Filter-Seitenleiste (Demodaten)](https://shrippen.github.io/andon/shots/release-0.7.0-hints-de.webp)

![Wandanzeige: ein Satz je Bildschirm (Demodaten)](https://shrippen.github.io/andon/shots/release-0.7.0-wall-de.webp)

![Tabellen als Karten am Handy](https://shrippen.github.io/andon/shots/release-0.7.0-phone-cards-de.webp)

![Die Navigations-Schublade](https://shrippen.github.io/andon/shots/release-0.7.0-phone-drawer-de.webp)

![Homelable-Abgleich in der Verbindungsakte](https://shrippen.github.io/andon/shots/release-0.7.0-homelable-de.webp)

![Neuverbindungen im ISP-Bericht (Demodaten)](https://shrippen.github.io/andon/shots/release-0.7.0-isp-de.webp)

```
Tiles (board order) → pack into screen-high sets → balance (each ≥ 1/3 of the fullest) → transition (Kante 1.20)
```

All changes / Alle Änderungen: https://github.com/shrippen/andon/compare/v0.6.0...v0.7.0 · Changelog: https://shrippen.github.io/andon/#v0.7.0

Docker: `ghcr.io/shrippen/andon:0.7.0` (also `latest`, `0.7`)

## [0.6.0] — 2026-10-07

### English

Andon now reads 80 services instead of 47, and it checks them against each other: backups against heartbeats, running images against new CVEs, releases against red CI. Connections, boards and settings were reworked, and a QA round over full user journeys fixed what got in the way.

#### New
- Backups: Proxmox Backup Server, Kopia, Duplicati, Backrest and UrBackup. One backup overview lists the last backup of every item from all tools, problems first.
- Monitoring and updates: Healthchecks, Prometheus (alerts become hints, PromQL values become tiles), NVD, Drone with one CI tile for GitHub, Gitea and Drone, What's Up Docker and Watchtower.
- Power, network and home: PeaNUT, apcupsd, OpenDTU, EVCC, Traefik, Caddy, Nginx Proxy Manager, Technitium, FRITZ!Box and ESPHome.
- Media, tasks and money: Tautulli, Jellystat, Navidrome, Audiobookshelf and Jellyseerr/Overseerr with a now-playing tile across all of them; Vikunja, Firefly III and Ghostfolio.
- Reading and community: a reading tile for Hacker News, Lobsters, Reddit, YouTube channels and Twitch; Mastodon, GoToSocial, Akkoma and Lemmy; GitHub downloads and trends, KDE Store downloads, sport results.
- New rules where services meet, for example a heartbeat that is green while its backup is old, a running image hit by a new CVE, a release on red CI, an update on a machine without a backup.
- Rides from Dawarich tracks with real distances, each classified business, commute or private with its reason, for travel costs and the tax export; customers linked across Kimai, Invoice Ninja, Sure and Paperless, with or without a Verbund; IT docs and compose stacks read from Gitea, with a docs coverage tile.
- Admins set mail, Apprise, the AI key, the check interval, session limits and the log level under Setup → Server, without a restart. The digest mail gets its own settings with a preview, a send-now test and a log.
- A maintenance page shows admins failed fetches, running tasks and recent log records.

#### Improved
- Each connection opens as a record with tabs (overview, access, history). Template logins are held per person or team, and all settings pages share one frame by level: me, team, instance.
- One catalog for templates and tiles you already set up, with search. Tiles are edited in a dialog only, can be picked up anywhere in edit mode, and wide or tall tiles use their room.
- Boards appear as cards with a sketch of their layout; a new order shows at once.
- Charts label both axes, carry a legend and name both values on hover.
- Times of day name their zone (e.g. CEST) when your browser is in a different zone than the server, in tiles, dialogs and forms.
- Every query is paced by its service's rate limit, and private iCal calendars with a login work, with an all-day lane.

#### Fixed
- Pushes that failed (no Apprise server, or a refusal) still marked their hints as sent, so they never came. Now only a delivered push counts.
- Saving in the code view kept no IDs, so "merge" doubled boards and tiles. Undo goes back step by step, and a deleted tile comes back on every board it was on.
- Kimai Lite no longer refreshes away an open form; the Today and Kimai tiles count the running timer.
- TOTP setup shows a QR code and keeps the setup after a wrong code; invites say whether the mail really went out; board exports contain their tiles; error pages keep the app frame and your input.
- OIDC_* variables now win over stored SSO settings, like every other variable; migration 0018 no longer fails on a duplicate shared grant.

#### Note
- Push needs an Apprise API server (`APPRISE_API_URL` or Setup → Server). Without one Andon no longer tries, and the notifications page says so.
- A setting in the environment still wins over the one in the UI and locks its field. Set `TZ` in the container so times follow your zone, not UTC.
- The services added in this release were tested against recorded answers and the demo world, not yet against real instances. Run the connection test and check the first hints.

![One backup overview across all tools (demo data)](https://shrippen.github.io/andon/shots/release-0.6.0-backups-en.webp)

![A connection as a record with tabs (demo data)](https://shrippen.github.io/andon/shots/release-0.6.0-connection-en.webp)

![Boards as cards (demo data)](https://shrippen.github.io/andon/shots/release-0.6.0-boards-en.webp)

![Times name the server's zone when the browser is elsewhere](https://shrippen.github.io/andon/shots/release-0.6.0-timezone-en.webp)


### Deutsch

Andon liest jetzt 80 Dienste statt 47 und prüft sie gegeneinander: Sicherungen gegen Heartbeats, laufende Images gegen neue CVEs, Releases gegen rote CI. Verbindungen, Boards und Einstellungen wurden überarbeitet, und eine QA-Runde über ganze Nutzerwege hat behoben, was im Weg stand.

#### Neu
- Sicherungen: Proxmox Backup Server, Kopia, Duplicati, Backrest und UrBackup. Eine Backup-Übersicht zeigt die letzte Sicherung jedes Gegenstands aus allen Werkzeugen, Probleme zuerst.
- Überwachung und Updates: Healthchecks, Prometheus (Alerts werden Hinweise, PromQL-Werte Kacheln), NVD, Drone mit einer CI-Kachel für GitHub, Gitea und Drone, What's Up Docker und Watchtower.
- Strom, Netz und Zuhause: PeaNUT, apcupsd, OpenDTU, EVCC, Traefik, Caddy, Nginx Proxy Manager, Technitium, FRITZ!Box und ESPHome.
- Medien, Aufgaben und Geld: Tautulli, Jellystat, Navidrome, Audiobookshelf und Jellyseerr/Overseerr mit einer Kachel „Jetzt läuft“ über alle; Vikunja, Firefly III und Ghostfolio.
- Lesen und Community: eine Lese-Kachel für Hacker News, Lobsters, Reddit, YouTube-Kanäle und Twitch; Mastodon, GoToSocial, Akkoma und Lemmy; GitHub-Downloads und Trends, KDE-Store-Downloads, Sportergebnisse.
- Neue Regeln, wo Dienste sich treffen, etwa ein grüner Heartbeat bei alter Sicherung, ein laufendes Image mit neuer CVE, ein Release auf roter CI, ein Update auf einer Maschine ohne Sicherung.
- Fahrten aus Dawarich-Tracks mit echten Strecken, jede als dienstlich, Arbeitsweg oder privat eingeordnet, mit Grund, für Reisekosten und Steuer-Export; Kunden über Kimai, Invoice Ninja, Sure und Paperless verknüpft, mit oder ohne Verbund; IT-Doku und Compose-Stacks aus Gitea gelesen, mit einer Kachel zur Doku-Abdeckung.
- Admins stellen Mail, Apprise, den KI-Schlüssel, den Prüftakt, Sitzungsgrenzen und die Log-Stufe unter Einrichten → Server ein, ohne Neustart. Die E-Mail-Zusammenfassung bekommt eigene Einstellungen mit Vorschau, Sofort-Test und Log.
- Eine Wartungsseite zeigt Admins gescheiterte Abrufe, laufende Aufgaben und die letzten Log-Einträge.

#### Verbessert
- Jede Verbindung öffnet als Akte mit Reitern (Überblick, Zugang, Verlauf). Vorlagen-Zugänge gelten je Person oder Team, und alle Einstellungsseiten teilen einen Rahmen nach Ebene: ich, Team, Instanz.
- Ein Katalog für Vorlagen und schon eingerichtete Kacheln, mit Suche. Kacheln werden nur noch im Dialog bearbeitet, lassen sich im Bearbeitungsmodus überall greifen, und breite oder hohe Kacheln nutzen ihren Platz.
- Boards erscheinen als Karten mit einer Skizze ihres Layouts; eine neue Reihenfolge gilt sofort.
- Diagramme beschriften beide Achsen, haben eine Legende und nennen beim Überfahren beide Werte.
- Uhrzeiten nennen ihre Zone (z. B. CEST), wenn dein Browser in einer anderen Zone ist als der Server, in Kacheln, Dialogen und Formularen.
- Jede Abfrage richtet sich nach dem Ratenlimit ihres Dienstes, und private iCal-Kalender mit Anmeldung funktionieren, mit einer Ganztags-Spur.

#### Behoben
- Gescheiterte Pushes (kein Apprise-Server oder eine Ablehnung) haben ihre Hinweise trotzdem als gesendet markiert, sie kamen also nie. Jetzt zählt nur ein zugestellter Push.
- Speichern in der Code-Ansicht hat keine IDs behalten, „Zusammenführen“ hat Boards und Kacheln verdoppelt. Rückgängig geht Schritt für Schritt zurück, und eine gelöschte Kachel kommt auf jedes Board zurück, auf dem sie lag.
- Kimai Lite frischt ein offenes Formular nicht mehr weg; die Kacheln Heute und Kimai zählen den laufenden Timer mit.
- Die TOTP-Einrichtung zeigt einen QR-Code und behält die Einrichtung nach einem falschen Code; Einladungen sagen, ob die Mail wirklich rausging; Board-Exporte enthalten ihre Kacheln; Fehlerseiten behalten den App-Rahmen und deine Eingabe.
- OIDC_*-Variablen gehen jetzt den gespeicherten SSO-Einstellungen vor, wie jede andere Variable; Migration 0018 scheitert nicht mehr an einer doppelten Freigabe.

#### Beachten
- Push braucht einen Apprise-API-Server (`APPRISE_API_URL` oder Einrichten → Server). Ohne ihn versucht Andon es nicht mehr, und die Seite der Benachrichtigungen sagt das.
- Eine Einstellung aus der Umgebung geht weiter der aus der Oberfläche vor und sperrt ihr Feld. `TZ` im Container setzen, damit Zeiten deiner Zone folgen und nicht UTC.
- Die Dienste dieser Version sind gegen aufgezeichnete Antworten und die Demowelt getestet, noch nicht gegen echte Instanzen. Den Verbindungstest laufen lassen und die ersten Hinweise prüfen.

![Eine Backup-Übersicht über alle Werkzeuge (Demodaten)](https://shrippen.github.io/andon/shots/release-0.6.0-backups-de.webp)

![Eine Verbindung als Akte mit Reitern (Demodaten)](https://shrippen.github.io/andon/shots/release-0.6.0-connection-de.webp)

![Boards als Karten (Demodaten)](https://shrippen.github.io/andon/shots/release-0.6.0-boards-de.webp)

![Uhrzeiten nennen die Zone des Servers, wenn der Browser woanders ist](https://shrippen.github.io/andon/shots/release-0.6.0-timezone-de.webp)


```
Borg, PBS, Kopia, Duplicati, Backrest, UrBackup, PG Back Web, TrueNAS
        → BackupSource (item, last run, result, note)
        → backup overview · cross.heartbeat_backup · cross.update_unbacked
```

All changes / Alle Änderungen: https://github.com/shrippen/andon/compare/v0.5.0...v0.6.0 · Changelog: https://shrippen.github.io/andon/#v0.6.0

Docker: `ghcr.io/shrippen/andon:0.6.0` (also `latest`, `0.6`)

## [0.5.0] — 2026-10-03

### English

Every tile opens a detail dialog, the whole interface now comes from the Kante design system, and the database key is derived the hard way.

#### New
- A detail dialog for every tile: facts, charts with axes and hover values, and the hints that belong to the tile. Link tiles open theirs from the pulse icon.
- Maps in the Authentik dialog (where logins come from) and the Dawarich dialog (where you were).
- Kimai Lite is a full Kimai client: start, stop, edit and split time sheets; unknown tags are created before writing.
- Tandoor recipes, and an about page with version, build and licences.

#### Improved
- Buttons, fields, dialogs, charts, chips and the clock come from Kante, the design shared by all shrippen projects, instead of Andon's own styles.
- Large boards stay fast, also in edit mode. DNS answers are cached and job runs and tile refreshes are spread out, so a home resolver is not flooded.
- The database key comes from Argon2id over MASTER_KEY and a random salt per install, instead of a single SHA-256. Existing databases move over at first start; guessable keys are refused.

#### Fixed
- `rotate-key` skipped OAuth grants and clients, tile secrets and the OIDC secret; it now re-encrypts all of them.

#### Note
- Back up `andon.db.salt` together with the database. Without it the database cannot be opened; Andon's own backups carry it.

![Detail dialog of a tile (demo data)](https://shrippen.github.io/andon/shots/release-0.5.0-detail-en.webp)

### Deutsch

Jede Kachel öffnet einen Detaildialog, die ganze Oberfläche kommt jetzt aus dem Designsystem Kante, und der Datenbankschlüssel wird aufwendig abgeleitet.

#### Neu
- Ein Detaildialog für jede Kachel: Fakten, Diagramme mit Achsen und Werten beim Überfahren, dazu die Hinweise der Kachel. Link-Kacheln öffnen ihn über das Puls-Symbol.
- Karten im Authentik-Dialog (woher Anmeldungen kommen) und im Dawarich-Dialog (wo du warst).
- Kimai Lite ist ein vollständiger Kimai-Client: Zeiten starten, stoppen, bearbeiten und teilen; unbekannte Tags werden vor dem Schreiben angelegt.
- Rezepte aus Tandoor und eine Info-Seite mit Version, Build und Lizenzen.

#### Verbessert
- Schaltflächen, Felder, Dialoge, Diagramme, Chips und die Uhr kommen aus Kante, dem gemeinsamen Design aller shrippen-Projekte, statt aus eigenen Styles.
- Große Boards bleiben schnell, auch im Bearbeitungsmodus. DNS-Antworten werden zwischengespeichert, Prüfläufe und Kachel-Aktualisierungen verteilt, damit ein Heim-Resolver nicht überläuft.
- Der Datenbankschlüssel entsteht per Argon2id aus MASTER_KEY und einem zufälligen Salt je Installation statt aus einem einzigen SHA-256. Bestehende Datenbanken werden beim ersten Start umgestellt; erratbare Schlüssel werden abgelehnt.

#### Behoben
- `rotate-key` hat OAuth-Freigaben und -Clients, Kachel-Geheimnisse und das OIDC-Secret übersprungen; jetzt verschlüsselt es alle neu.

#### Beachten
- `andon.db.salt` zusammen mit der Datenbank sichern. Ohne die Datei lässt sich die Datenbank nicht öffnen; Andons eigene Sicherungen enthalten sie.

![Detaildialog einer Kachel (Demodaten)](https://shrippen.github.io/andon/shots/release-0.5.0-detail-de.webp)

All changes / Alle Änderungen: https://github.com/shrippen/andon/compare/v0.4.0...v0.5.0 · Changelog: https://shrippen.github.io/andon/#v0.5.0

## [0.4.0] — 2026-09-26

### English

The first release under the name Andon.

#### New
- Container images: `latest` from release tags, `edge` from main.
- Tests for every package.

#### Fixed
- Four bugs from the first review, and a polished interface.

### Deutsch

Die erste Version unter dem Namen Andon.

#### Neu
- Container-Images: `latest` aus Release-Tags, `edge` aus main.
- Tests für jedes Paket.

#### Behoben
- Vier Fehler aus der ersten Durchsicht, dazu eine überarbeitete Oberfläche.

Changelog: https://shrippen.github.io/andon/#v0.4.0
