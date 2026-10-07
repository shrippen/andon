# Entwurf: Fähigkeiten je Integration

> Stand: umgesetzt 06.10.2026 (Abschnitt „Umsetzung“ am Ende nennt Abweichungen vom Entwurf). Anlass: ROADMAP „Regeln: noch umzusetzen“ → „Dawarich: Orte“.

## Problem

Jedes Backend kann beim Abgleichen und Schreiben nur einen Teil dessen, was Andon über ein Thema weiß. Beispiel Orte:

| | Dawarich | Kimai „Anfahrten“ | Andon |
|---|---|---|---|
| lesen | Areas (Kreis), Places (Punkt) | Orte | Option `places` der Dawarich-Verbindung |
| anlegen | Area | Ort (nur mit `placesWrite` und `editOwn`) | immer |
| ändern | – | Ort (dieselben Rechte) | immer |
| Arten | keine, nur Geometrie | Zuhause, Arbeit, Kunde, Sonstiges | zusätzlich **Privat** |
| verweist auf | – | `dawarichAreaId` / `dawarichPlaceId` | `area:N`, `place:N`, `kimai:N` |

Heute steht dieses Wissen an keiner Stelle zusammen:

- **Erkennung** als lose Flags im Datensatz: `KimaiDataset.Mileage`, `PlacesWrite`, `MileageEdit`, `HolidayBundle`; `DawarichDataset.TracksState`; `Places == nil` bei altem Dawarich. Hinweise im Verbindungstest gibt es nur bei Kimai (`sources.TestNotes`).
- **Entscheidung, wohin geschrieben wird**, inline in `services/sites` mit zwei verschiedenen Prädikaten: Orte brauchen `PlacesWrite && MileageEdit` (`sites.go`, `pluginWrites`), Fahrten nur `MileageEdit` (`rides.go`, `pluginTrips`).
- **Partner-Verbindung finden** auf vier Arten: Kacheln nehmen die *erste* Verbindung des Dienstes (eigener Bereich zuerst, `widgetlib/display.go`), der Prüflauf die *letzte* je Bereich (`analysis.go`), `sites` die erste Kimai-Verbindung im selben Bereich, Belege eine Auswahl des Nutzers. Bei zwei Verbindungen eines Dienstes widersprechen sich Kachel und Hinweis.
- **Statische Tabellen** verstreut: `hooks.pushServices`, `connect.MethodOf`, `credshape.setupFields`, `rules.BackupSystems`, Peer-Listen in `widgets/costs.go` und `homelabx.go`.

## Begriffe

```
Domäne        ein Thema, das mehrere Dienste kennen: Ort, Fahrt, Kunde,
              Arbeitszeit, Abwesenheit, Rechnung, Zahlung, Beleg
Fähigkeit     Domäne × Operation (lesen, anlegen, ändern, verknüpfen)
              + Varianten (z. B. Ortsarten) + Voraussetzung (Plugin, Recht, Version)
Speicher      wer für ein Feld einer Domäne die Wahrheit hält
Verweis       ein Dienst speichert die ID eines anderen (Kimai-Ort → Dawarich-Area)
Verbund      Verbindungen verschiedener Dienste, die zusammenarbeiten, z. B.
              Dawarich + Kimai + Invoice Ninja + Sure + Paperless; trägt eigene Daten
Partner      die Verbindung eines Dienstes, die für eine andere im Verbund steht
Zuordnung    ein Ding, das in mehreren Mitgliedern eines Verbunds existiert,
             je Mitglied mit dessen ID (Kunde: Kimai 12, Ninja "Kx9", Sure "ACME")
```

## Modell

Zwei Ebenen: was ein **Dienst** grundsätzlich kann (statisch, im Code deklariert), und was eine **Verbindung** gerade kann (zur Laufzeit erkannt: Plugin da, Recht da, Version neu genug).

```
            statisch (internal/caps)                 Laufzeit (sources)
  ┌──────────────────────────────────┐     ┌───────────────────────────────┐
  │ Kimai: places create/update      │     │ ping: placesWrite ✓ editOwn ✗ │
  │        needs plugin, placesWrite,│ ──► │ ⇒ places: read                │
  │        editOwn                   │     │   (create/update fehlt:       │
  │        kinds home…other          │     │    „Recht editOwn fehlt“)     │
  │        refs dawarich.area/place  │     └───────────────┬───────────────┘
  └──────────────────────────────────┘                     │ Caps im Datensatz
                                                           ▼
                                   services: store(Domäne, Feld, Verbindungen)
                                   → Kimai, Dawarich oder Andon-Option
```

Skizze der Typen (Blattpaket `internal/caps`, ohne I/O):

```go
type Domain string // places, rides, customers, worktime, absences, invoices, payments, receipts
type Op string     // read, create, update, link

// Need is what a connection must have for a capability, e.g. the
// mileage plugin with the feature placesWrite and the right editOwn.
type Need struct{ Plugin, Feature, Right string }

// Cap is one thing a service can do in a domain.
type Cap struct {
	Domain Domain
	Op     Op
	Kinds  []string // variants it supports, e.g. place kinds; nil = all
	Needs  []Need
	Refs   []Domain // IDs of other services it stores, e.g. dawarich.area
}

// Set is what one connection can do now: the declared caps whose needs
// the source found met, and why the others are missing.
type Set struct {
	Have    []Cap
	Missing []Gap
}

// Gap is a declared capability the connection lacks, and why.
type Gap struct {
	Cap    Cap
	Reason string // catalog key, e.g. "caps.need_right"
}
```

- Die Quelle (`sources/kimai.go`, `loadMileage`) erkennt wie heute, schreibt aber ein `caps.Set` in den Datensatz statt eigener Flags. `PlacesWrite`, `MileageEdit` und `HolidayBundle` entfallen.
- Der Verbindungstest zeigt die Fähigkeiten und, was fehlt („Orte nur lesend: Recht editOwn fehlt“). Das ersetzt `TestNotes` für alle Dienste.
- Die Akte einer Verbindung bekommt einen Abschnitt „Fähigkeiten“.

## Speicher wählen

Eine reine Funktion statt Bedingungen in `sites`:

```go
// Store picks who keeps field f of a domain entry: the first service in
// order whose set can write it, else Andon.
func Store(d Domain, kind string, order []Set) Holder
```

Reihenfolgen, aus dem heutigen Verhalten abgeleitet:

| Domäne, Feld | 1. | 2. | sonst |
|---|---|---|---|
| Ort: Geometrie | Dawarich | – | – |
| Ort: Art und Kunde (nicht privat) | Kimai „Anfahrten“ | – | Andon |
| Ort: Markierung „privat“ | – | – | Andon (der Ort selbst liegt in Dawarich und im Plugin, dort als „Sonstiges“, weil das Plugin einen Typ braucht) |
| Fahrt: Klasse (Auto, Motorrad) | Kimai „Anfahrten“ | – | Andon (`rides`) |
| Fahrt: Klasse (andere) | – | – | Andon |

`pluginWrites` und `pluginTrips` werden zu `caps.Store(...)`. Eine dritte Integration mit Orten (etwa Home Assistant Zonen) fügt nur eine Deklaration und eine Zeile in der Reihenfolge hinzu.

## Abhängigkeiten

Verweise werden deklariert (`Cap.Refs`), nicht im Abgleich versteckt:

```
Dawarich.area ◄── Kimai.place (dawarichAreaId)
Dawarich.place ◄── Kimai.place (dawarichPlaceId)
Kimai.customer ◄── Kimai.place, Kimai.trip (über Projekt)
Kimai.customer ~~ Ninja.client      (heute nur über den Namen)
Ninja.expense  ◄── Paperless.document (Belegfelder)
```

Daraus folgt:
- **Reihenfolge beim Anlegen**: Erst das Ziel des Verweises, dann der Verweisende (heute schon so: Area vor Kimai-Ort). `Sync` liest sie aus den Verweisen.
- **Hinweise bei fehlendem Partner**: Wer einen Verweis deklariert, braucht den Partner. `rules.Needs` (heute nur zweimal genutzt) wird daraus abgeleitet.
- **Namensabgleich** (`~~`) wird durch eine feste Zuordnung über IDs ersetzt (Abschnitt „Kunden“).

## Verbund

Funktionen, die mehrere Dienste brauchen, müssen wissen, welche Verbindungen zusammengehören: Fahrten (Dawarich + Kimai), Kundenseite (Kimai + Invoice Ninja), Monatsabschluss (Kimai + Ninja + Sure + Paperless + Mail). Heute sucht jede Stelle anders: Kacheln die erste, der Prüflauf die letzte, `sites` die erste im Bereich, Belege die Wahl des Nutzers.

Entschieden 06.10.2026: Die Zusammengehörigkeit ist ein eigener Datensatz, der **Verbund**, mit beliebig vielen Mitgliedern, und er trägt Daten, die keinem Mitglied allein gehören (Option C, auf mehr als zwei erweitert). Gewählt wird nur, wenn es mehrere Verbindungen eines Dienstes gibt.

```
                   Verbund "Studio Weber"
        ┌──────────────────────────────────────────────┐
        │ Dawarich #3   Kimai #7   Ninja #9   Sure #11 │   je Dienst höchstens ein Mitglied
        │                              Paperless #14   │
        └───────────────────────┬──────────────────────┘
                                │ Zuordnungen (domain "customers")
        ┌───────────────────────┴──────────────────────────────────────┐
        │ #1  Kimai 12 ✓   Ninja "Kx9" ✓ (Namen)   Sure "ACME GmbH" ?  │
        │ #2  Kimai 15 ✓   Ninja –  (kein Gegenstück)                  │
        └──────────────────────────────────────────────────────────────┘
          ✓ bestätigt   ? vorgeschlagen   – kein Gegenstück
```

### Datenmodell

```sql
CREATE TABLE links (                        -- ein Verbund
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,                 -- "Studio Weber", "Auftraggeber Müller"
  created_by INTEGER REFERENCES users(id),
  created_at TEXT NOT NULL
);
CREATE TABLE link_members (
  link_id    INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE,
  conn_id    INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  service    TEXT NOT NULL,                 -- Kopie von connections.service
  PRIMARY KEY (link_id, conn_id),
  UNIQUE (link_id, service)                 -- je Dienst ein Mitglied: Partner eindeutig
);
CREATE TABLE link_entries (                 -- eine Zuordnung
  id         INTEGER PRIMARY KEY,
  link_id    INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE,
  domain     TEXT NOT NULL,                 -- caps.Domain: customers, …
  updated_at TEXT NOT NULL
);
CREATE TABLE link_keys (                    -- die ID eines Mitglieds in einer Zuordnung
  entry_id   INTEGER NOT NULL REFERENCES link_entries(id) ON DELETE CASCADE,
  link_id    INTEGER NOT NULL,              -- Kopie, für die Eindeutigkeit
  domain     TEXT NOT NULL,                 -- Kopie, für die Eindeutigkeit
  conn_id    INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  key        TEXT NOT NULL DEFAULT '',      -- ID im Dienst, '' = kein Gegenstück
  state      TEXT NOT NULL,                 -- suggested, confirmed, none
  PRIMARY KEY (entry_id, conn_id),
  UNIQUE (link_id, domain, conn_id, key)    -- eine ID steckt in höchstens einer Zuordnung
);
```

- **Je Dienst ein Mitglied** pro Verbund: Innerhalb eines Verbunds ist der Partner eindeutig. Zwei Kimai-Instanzen ergeben zwei Verbünde.
- **Eine Verbindung in mehreren Verbünden** ist erlaubt (ein Sure-Konto für zwei Firmen). Dann wird nach dem Verbund gefragt, wo es mehrdeutig ist (siehe Auflösung).
- **Zuordnungen** halten je Mitglied eine ID mit eigenem Status, so lässt sich Ninja bestätigen, während Sure noch Vorschlag ist. Mitglieder, die eine Domäne nicht kennen (Dawarich bei Kunden), haben dort keinen Schlüssel; welche das sind, sagt `internal/caps`.
- **Löschen** einer Verbindung entfernt ihre Mitgliedschaften und Schlüssel (`CASCADE`); eine Zuordnung mit weniger als zwei Schlüsseln wird mitgelöscht. In den Diensten selbst wird nichts gelöscht.
- **Rechte:** Verbund anlegen, Mitglied aufnehmen oder entfernen, Zuordnungen ändern braucht EDIT auf allen beteiligten Verbindungen; lesen VIEW auf allen. Bereichsgrenzen sind erlaubt (persönliches Dawarich, Team-Kimai), solange die Rechte reichen.
- **Export und Sicherung:** Verbünde, Mitglieder und Zuordnungen gehören zum Export; keine Geheimnisse darin.

### Impliziter Verbund

Hat ein Bereich je Dienst höchstens eine Verbindung, bilden diese stillschweigend einen Verbund: nichts wird gespeichert, nichts gefragt. Gespeichert wird ein Verbund erst,
- wenn eine zweite Verbindung desselben Dienstes dazukommt und du zuordnest, oder
- wenn er Daten bekommen soll: Die Seite der Verbünde zeigt ihn als „Ohne Verbund“; „Kunden“ speichert ihn mit den gekoppelten Verbindungen des Bereichs (`caps.Paired`) unter dem Namen des Bereichs und öffnet die Zuordnung (`verbund.Settle`).

### Auflösung

Eine Funktion in `services/connections`, die alle nutzen:

```go
// Partner finds the member of service in the group the asker works in;
// Ambiguous when several groups or connections fit and none was chosen.
func Partner(q db.Queryer, who *access.Principal, at Asker, service enums.ServiceType) (*model.Connection, PartnerState, error)

// Asker is who needs the partner: a tile, a connection, or a space.
type Asker struct{ SpaceID, ConnID, WidgetID, LinkID int64 }
```

```
gesucht: Mitglied im Dienst S für <Kachel | Verbindung>
  1. Kachel mit gewähltem Verbund (Feld nur, wenn mehrere passen) → dessen S-Mitglied
  2. Kachel bzw. Verbindung X in genau einem Verbund mit S-Mitglied → dieses
  3. kein gespeicherter Verbund: impliziter Verbund des Bereichs (genau eine S-Verbindung;
     sonst genau eine erreichbare) → diese
  sonst: keiner → Hinweis „mehrere Verbünde/Verbindungen für S, zuordnen“
```

`widgetlib.peerConnection`, `sites.open`, `billing.pairs`, `clients` und `mailfwd` rufen sie auf. Die heutige Auswahl in „Belege“ (Ninja + Paperless als Nutzer-Einstellung) und die Feldzuordnung `receipt_*` werden zu Mitgliedschaften und Daten eines Verbunds.

**Prüflauf:** Regeln, die mehrere Dienste brauchen, laufen je Verbund und bekommen genau die Datensätze seiner Mitglieder, statt je Bereich „irgendeinen Kimai“ (`analysis`: heute „letzte gewinnt“). Ohne gespeicherten Verbund ist das der implizite des Bereichs, also das heutige Verhalten, solange es je Dienst eine Verbindung gibt.

### Oberfläche

- **Bereich → Verbünde:** Liste der Verbünde mit Mitgliedern; anlegen, umbenennen, Mitglied aufnehmen oder entfernen. Erscheint als eigener Punkt erst, wenn es gespeicherte Verbünde oder doppelte Dienste gibt.
- **Akte einer Verbindung → „Im Verbund“:** die Verbünde mit den übrigen Mitgliedern, implizit oder gespeichert.
- **Reiter je Zuordnungs-Domäne**, z. B. „Kunden“, am Verbund (und von jeder beteiligten Akte aus erreichbar).
- **Kachel-Einstellungen:** Feld „Verbund“ nur, wenn mehrere passen.
- **Hinweis** `conn.partner_ambiguous` führt zum Verbund.

## Kunden (erste Zuordnung)

Entschieden 06.10.2026: feste Zuordnung über IDs, mit Vorschlagsansicht; Invoice Ninja ist die Quelle der Namen.

- **Domäne `customers`**, Mitglieder laut `internal/caps`: Kimai (Kunde), Invoice Ninja (Client), später Sure (Gegenpartei), Paperless (Korrespondent).
- **Namensquelle** je Domäne in `internal/caps` deklariert: `customers` → Invoice Ninja.
- **Vorschlagsansicht** (Reiter „Kunden“ des Verbunds): je Zuordnung eine Zeile, je Mitglied eine Spalte; Vorschlag über den ähnlichsten Namen zur Namensquelle; bestätigen, ändern, „kein Gegenstück“. Ungeklärte oben, wie bei Orten.
- **Angleichen (optional):** „Namen übernehmen“ schreibt den Ninja-Namen in die anderen Mitglieder, soweit deren Fähigkeiten das erlauben (Kimai: `PATCH /api/customers/{id}`, neuer Ausgang in `outbound/kimai.go`). Nie zurück nach Ninja.
- **Nutzer** der Zuordnung statt Namensabgleich: Kundenseiten (`metrics/clients.go`), Vollkosten-Stundensatz, `geo.travel_unbilled`, Rechnungsentwürfe (`billing`).
- Ohne Zuordnung gilt vorerst weiter der Namensabgleich, als „vorgeschlagen“ markiert.

## Schichten

```
web        Akte: Fähigkeiten, Im Verbund; Bereich: Verbünde mit Reitern der Zuordnungen
services   connections.Partner, links (Verbünde, Zuordnungen, Rechte); sites, billing, clients …: caps.Store
repos      links, link_members, link_entries, link_keys
sources    erkennen → caps.Set im Datensatz
caps       Typen, Deklarationen je Dienst, Store, reine Funktionen (Blatt)
```

`caps` importiert nichts aus Andon außer `enums`; alle Schichten dürfen es nutzen (wie `progress`).

## Schritte

Entschieden 06.10.2026: klein beginnen; wenn stabil, auf alle Verbindungen ausweiten und als Standard für jede Integration festlegen.

1. **Klein:** `internal/caps` mit Typen und Deklarationen nur für Orte und Fahrten (Dawarich, Kimai „Anfahrten“, Andon). Die Kimai-Quelle liefert dafür ein `caps.Set`; `pluginWrites` und `pluginTrips` werden zu `caps.Store`, ohne Verhaltensänderung, Tests je Reihenfolge.
2. Verbindungstest und Akte zeigen Fähigkeiten und fehlende Voraussetzungen.
3. Verbünde: Tabellen, impliziter Verbund, `connections.Partner`, „Im Verbund“, Bereich → Verbünde; Kacheln, Prüflauf (je Verbund) und Services stellen um.
4. Kunden als erste Zuordnung (Kimai, Ninja; Sure und Paperless danach): Vorschlagsansicht, Angleichen.
5. **Groß:** übrige Kimai-Erkennung (Holiday-Bundle, Vertrag), dann jede Integration mit Erkennung; Regel in `agent.md`: neue Integrationen deklarieren ihre Fähigkeiten in `internal/caps`. Später statische Tabellen (`hooks.pushServices`, `connect.MethodOf`, Backup-Systeme) in dieselben Deklarationen.

## Nicht-Ziele

- Kein allgemeiner Synchronisationsmotor: Abgleiche bleiben eigene Services, nur die Entscheidungen werden einheitlich.
- Kein Löschen über Systeme hinweg (bleibt wie heute).
- Keine Fähigkeiten-API nach außen.

## Entscheidungen (06.10.2026)

1. **Partner:** Wahl nur, wenn es mehrere gibt. Zusammengehörigkeit als eigener Datensatz (Option C), erweitert auf beliebig viele Mitglieder: der **Verbund**, der auch Zuordnungen trägt.
2. **Private Orte:** Der Ort liegt in Dawarich und im Plugin (dort „Sonstiges“, das Plugin braucht einen Typ); nur die Markierung „privat“ lebt in Andon. Entspricht dem heutigen Code.
3. **Kunden:** feste Zuordnung über IDs mit Vorschlagsansicht, optional Namen angleichen; Invoice Ninja ist die Quelle der Namen.
4. **Umfang:** klein beginnen (Orte, Fahrten), wenn stabil groß und als Standard für alle Integrationen.

## Umsetzung (06.10.2026)

Abweichungen vom Entwurf, und was er offen ließ:

- **Partner** liegt in `services/verbund` (`verbund.Partner`, `verbund.Pairs`, `verbund.Groups`), nicht in `services/connections`, damit die Verbindungen keinen Zugriff nach außen öffnen müssen.
- **„Frei“:** Eine Verbindung, die in einem Verbund mit einer *anderen* Verbindung des fragenden Dienstes steht, ist kein Kandidat (Kimai A gehört zu Dawarich 1, also bekommt Dawarich 2 das freie Kimai B). Ein verlinktes Mitglied, das der Nutzer nicht erreicht, ist kein Partner, und es wird auch kein anderes genommen.
- **Prüflauf:** Verbünde zählen nur mit Mitgliedern aus demselben Bereich; Daten eines anderen (z. B. persönlichen) Bereichs gelangen nie in Hinweise eines Bereichs. Regeln einer Verbindung laufen in jedem Verbund, in dem sie steht; Funde werden nach Fingerprint zusammengeführt. Hinweis `system.partner_ambiguous` je Bereich.
- **Abrechnung, Zahlungen, Kundenseiten** paaren je Kimai bzw. Sure; Formulare nennen die Verbindung (`conn_id`, Kundenseite `?kimai=`). Das Jahrespaket wird je Verbund gebaut (Auswahl im Formular).
- **Belege:** Die eigene Wahl des Nutzers bleibt (Stufe 1); ist nur eine Seite eindeutig, ergänzt der Verbund die andere. Die Feldzuordnung `receipt_*` bleibt vorerst in den Optionen der Verbindung.
- **Bereich → Verbünde** steht immer im Menü (nicht nur bei Bedarf); die Seite nennt mehrdeutige Dienste.
- **Kachel-Feld „Verbund“** erscheint bei jedem Typ, der Partnerdienste liest (Vorgabe oder eine Auswahl seiner Felder), nicht nur ohne eigene Verbindung: es löst auch eine Verbindung, die in zwei Verbünden steht.
- **Kunden:** Verbund → „Kunden“ (bei Kimai und Invoice Ninja im Verbund). Vorschlag = freier Ninja-Kunde mit dem ähnlichsten Namen (Wortüberlappung ohne Rechtsform, ab 0,5); gespeichert wird nur Bestätigtes, auch „kein Gegenstück“. Ein Ninja-Kunde gehört zu höchstens einem Kimai-Kunden; Zuordnungen nehmen nur Kunden, die die Dienste kennen. Ninja-Kunden werden über ihre ID (v5: gehashter Schlüssel) referenziert. „Namen übernehmen“ schreibt den Ninja-Namen nach Kimai (`PATCH /api/customers/{id}`). Stundensätze, Entwürfe, Kundenkarten und Querprüfungen verbinden über `metrics.ClientMap` und fallen ohne Zuordnung auf den gleichen Namen zurück. Sure und Paperless folgen.
- **Groß (Schritt 5):** Kimai meldet Holiday-Bundle (`absences read`, Plugin `holiday`) und Sollarbeitszeit (`worktime read` Art `target`, Einstellung `contract`) als Fähigkeiten; das Flag `HolidayBundle` entfällt. Deklariert sind außerdem Invoice Ninja (Kunden, Rechnungen, Zahlungen, Belege), Sure (Zahlungen lesen), Paperless (Belege; lesen und ändern brauchen die Schnittstelle `custom_fields`) und Postfächer (Belege lesen). Neue Voraussetzungsart `setting`: ein Wert, den der Nutzer im Dienst pflegt.
- **Verbindungstest:** Test-Quellen liefern ein `caps.Set` unter `sources.TestCaps`; der Test nennt jede Lücke in einer Domäne, die die Verbindung sonst bedient (`Set.Partial`: „Orte lesen ja, ändern nein“). Ein ganz fehlendes Plugin ist kein Hinweis. Das ersetzt die Kimai-eigenen `TestNotes`.
- **Regel** in `agent.md`: neue Integrationen deklarieren ihre Fähigkeiten in `internal/caps`; `TestDeclaredServicesReportCaps` prüft, dass jeder deklarierte Dienst sie über seinen Datensatz meldet, `TestCapsKeysExist` die Texte. Offen bleiben die statischen Tabellen (`hooks.pushServices`, `connect.MethodOf`, Backup-Systeme).
- **Gepaart und bereichsweit (Prüfung 06.10.2026):** Nur Dienste mit deklarierten Fähigkeiten paaren (`caps.Paired`): Kimai, Invoice Ninja, Sure, Paperless, Dawarich, Postfach, Kalender (Termine ↔ Kimai) und Wallos (Abos ↔ Sure). Nur sie gelten als mehrdeutig. Alle anderen liest Andon bereichsweit: Mehrere Docker-Hosts, Borg-Server, Proxmox usw. gehen alle in die Querprüfungen und Kacheln ein. Datensätze mit `sources.Merger` werden zu einem vereinigt („die Container des Bereichs“); die übrigen laufen reihum je Verbindung, Funde werden nach Fingerprint zusammengeführt. Vor dieser Prüfung lasen Querprüfungen bei mehreren Hosts keinen davon, die Backup-Kachel meldete „mehrdeutig“, und der Hinweis „mehrere Verbindungen ohne Verbund“ nannte auch Docker.
- **Belege** prüfen die Fähigkeiten des Paperless (Schnittstelle `custom_fields`) aus dem letzten Abruf und sagen bei einem zu alten Paperless, warum es nicht geht.
- **Kunden über alle Mitglieder:** Invoice Ninja führt die Zeilen (Quelle der Namen), Kimai, Sure (Zahler: Händler, sonst Buchungstext ohne Nummern und Daten) und Paperless (Korrespondent) sind Spalten; ohne Ninja führt Kimai. Ein Feld mit „–“ lässt den Namen entscheiden, „Kein Gegenstück“ sagt, dass es keinen gibt. Früher gespeicherte Kimai-Kunden ohne Ninja-Kunden stehen darunter und lassen sich vergessen. Verknüpfte Zahler zählen beim Zahlungsabgleich und bei unerwarteten Eingängen als der Kunde (`metrics.PayerMap`); die Kundenseite verlinkt die Dokumente des Korrespondenten in Paperless (`metrics.DocsMap`).
- **Verweise:** `Cap.Refs` nennt die IDs anderer Dienste, die eine Fähigkeit speichert (Kimai-Orte → Dawarich-Orte, Ninja-Ausgaben ↔ Paperless-Belege). `caps.Order` leitet die Reihenfolge beim Anlegen ab (ein Test hält sie mit `sites.Create`/`Sync` gleich), `caps.Needed` die Abhängigkeiten. Regeln deklarieren, was sie lesen (`rules.Uses`, `internal/rules/uses.go`); ist ein Dienst daraus oder aus seinen Verweisen ausgefallen, bleiben ihre Hinweise stehen (`Spec.Incomplete`). `rules.Needs` bleibt für Dienste ohne Fähigkeiten.
- **Eigenschaften:** Was ein Dienst neben den Domänen kann, steht in `caps.Traits` (`internal/caps/traits.go`): Webhooks (`hooks.Accepts`), Wiederherstellungstest (`rules.BackupSystems`), Anmeldung ohne Token (`connect.MethodOf`, das nur Adresse und Optionen dazu prüft) und OAuth-Client (`connect.NeedsClient`). Die Akte zeigt sie bei den Fähigkeiten.
