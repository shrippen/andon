# Entwurf: Fähigkeiten je Integration

> Stand: Entwurf vom 06.10.2026, noch nicht umgesetzt. Anlass: ROADMAP „Regeln: noch umzusetzen“ → „Dawarich: Orte“.

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
Partner       die Verbindung, mit der eine Verbindung zusammenarbeitet
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
- **Namensabgleich** (`~~`) wird durch eine feste Zuordnung über IDs ersetzt (Abschnitt „Kunden Kimai ↔ Invoice Ninja“).

## Partner-Verbindung

Eine Funktion, die zwei Dienste braucht (Fahrten: Dawarich + Kimai; Kundenseite: Kimai + Invoice Ninja), muss wissen, welche Verbindung des anderen Dienstes gemeint ist: ihren **Partner**. Gibt es nur eine, ist er eindeutig und es gibt nichts zu wählen (entschieden 06.10.2026: Wahl nur bei mehreren).

Heute sucht jede Stelle anders: Kacheln die erste, der Prüflauf die letzte, `sites` die erste im Bereich, Belege die Wahl des Nutzers.

Eine Funktion in `services/connections`, die alle nutzen:

```go
// Partner finds the connection of service a caller works with; Ambiguous
// when several fit and none was chosen.
func Partner(q db.Queryer, who *access.Principal, at Asker, service enums.ServiceType) (*model.Connection, PartnerState, error)

// Asker is who needs the partner: a tile, a connection, or a space.
type Asker struct{ SpaceID, ConnID, WidgetID int64 }
```

`widgetlib.peerConnection`, die Datensätze im Prüflauf (`analysis`: heute „letzte gewinnt“), `sites.open`, `billing.pairs`, `clients` und `mailfwd` rufen sie auf.
Die heutige Auswahl in „Belege“ (Ninja + Paperless als Nutzer-Einstellung) und die Feldzuordnung `receipt_*` werden zu einer Verknüpfung Ninja ↔ Paperless mit Einträgen.

### Verknüpfung als eigenes Datenmodell (entschieden 06.10.2026: Option C)

Ein Paar von Verbindungen ist ein eigener Datensatz, die **Verknüpfung**. Sie trägt Daten, die nur in diesem Paar leben, etwa die Kunden-Zuordnung Kimai ↔ Invoice Ninja; keine der beiden Verbindungen besitzt sie allein.

```
connections            links                          link_entries
┌──────────────┐       ┌─────────────────────┐        ┌────────────────────────────────┐
│ 3 Dawarich   │◄──a───│ 1  a=3  b=7         │        │ link 2  customers  12 → "Kx9"  │
│ 7 Kimai      │◄──b───│ 2  a=7  b=9         │◄───────│         confirmed              │
│ 9 Ninja      │◄──b───└─────────────────────┘        │ link 2  customers  15 → –      │
└──────────────┘                                      │         none ("kein Gegenstück")│
                                                      └────────────────────────────────┘
```

Tabellen (Migration):

```sql
CREATE TABLE links (
  id         INTEGER PRIMARY KEY,
  a_conn     INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  b_conn     INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  created_by INTEGER REFERENCES users(id),
  created_at TEXT NOT NULL,
  UNIQUE (a_conn, b_conn), CHECK (a_conn < b_conn)
);
CREATE TABLE link_entries (
  link_id    INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE,
  domain     TEXT NOT NULL,        -- caps.Domain: customers, …
  a_key      TEXT NOT NULL,        -- id in a's service ("12")
  b_key      TEXT NOT NULL DEFAULT '', -- id in b's service ("Kx9"), '' = none
  state      TEXT NOT NULL,        -- suggested, confirmed, none
  updated_at TEXT NOT NULL,
  PRIMARY KEY (link_id, domain, a_key)
);
```

- **Symmetrisch:** `a_conn < b_conn`; welche Seite welcher Dienst ist, sagt die Verbindung, nicht die Spalte.
- **Mehrere Paare** sind möglich (zwei Kimai, je mit eigenem Ninja). Eine Verbindung hat je Partnerdienst höchstens eine Verknüpfung.
- **Löschen** einer Verbindung löscht ihre Verknüpfungen samt Einträgen (`CASCADE`); in den Diensten selbst wird nichts gelöscht.
- **Rechte:** anlegen, ändern, lösen braucht EDIT auf beiden Verbindungen; lesen braucht VIEW auf beiden. Bereichsgrenzen sind erlaubt (persönliches Dawarich, Team-Kimai), solange die Rechte reichen.
- **Export und Sicherung:** Verknüpfungen und Einträge gehören zum Export des Bereichs der Verbindungen; keine Geheimnisse darin.

### Auflösung mit Verknüpfungen

```
gesucht: Partner im Dienst S für <Kachel | Verbindung>
  1. Kachel hat eine Verbindung → deren Verknüpfung zu S
  2. Kachel ohne eigene Verbindung (Monatsabschluss …) → gewählte Verbindung an der Kachel (nur bei mehreren)
  3. keine Verknüpfung, aber genau eine Verbindung von S im Bereich (sonst: erreichbar) → diese, implizit
  sonst: keiner → Hinweis „mehrere S-Verbindungen, verknüpfen“
```

- **Implizit bleibt implizit:** Bei genau einer Verbindung wird nichts gespeichert und nichts gefragt. Eine gespeicherte Verknüpfung entsteht erst, wenn du wählst oder das Paar Daten bekommt (die erste bestätigte Kunden-Zuordnung legt sie an).
- Der Prüflauf arbeitet je Bereich mit einem Datensatz je Dienst (`analysis`: heute „letzte gewinnt“). Er wird je Verknüpfung gerechnet: Regeln, die zwei Dienste brauchen, bekommen die Datensätze eines Paars, nicht „irgendeinen Kimai“.

### Oberfläche

- **Akte einer Verbindung → „Verknüpft mit“:** je Partnerdienst die Verknüpfung oder der implizite Partner („einzige“), mit Herkunft; „verknüpfen“ und „lösen“ nur, wenn es mehrere gibt oder eine Verknüpfung besteht.
- **Reiter je Verknüpfungsdaten**, z. B. „Kunden“ (siehe unten), in der Akte beider Verbindungen sichtbar.
- **Kachel-Einstellungen:** Feld je Partnerdienst nur für Kacheln ohne eigene Verbindung und nur bei mehreren.
- **Hinweis** `conn.partner_ambiguous` führt zur Akte.

### Schichten

```
web       Akte: Verknüpft mit, Reiter der Verknüpfungsdaten
services  connections.Partner, links (anlegen, lösen, Einträge; Rechte)
repos     links, link_entries
```

## Kunden Kimai ↔ Invoice Ninja

Entschieden 06.10.2026: feste Zuordnung über IDs, mit Vorschlagsansicht; Invoice Ninja ist die Quelle der Namen.

```
Kimai-Kunde 12 ═══ Ninja-Kunde "Kx9"      link_entries der Verknüpfung Kimai ↔ Ninja,
                                          domain "customers", state confirmed
```

- **Vorschlagsansicht** (Reiter „Kunden“ der Verknüpfung, in beiden Akten): je Kimai-Kunde der Ninja-Kunde mit dem ähnlichsten Namen als Vorschlag; bestätigen, ändern, „kein Gegenstück“. Ungeklärte oben, wie bei Orten.
- **Angleichen (optional):** weicht der Kimai-Name ab, schreibt „Namen übernehmen“ den Ninja-Namen nach Kimai (`PATCH /api/customers/{id}`, neuer Ausgang `outbound/kimai.go`). Nie umgekehrt.
- **Nutzer** der Zuordnung statt Namensabgleich: Kundenseiten (`metrics/clients.go`), Vollkosten-Stundensatz, `geo.travel_unbilled`, Rechnungsentwürfe (`billing`).
- Ohne Zuordnung gilt vorerst weiter der Namensabgleich, als „vorgeschlagen“ markiert.

## Schichten

```
web        Akte: Fähigkeiten, Verknüpft mit, Reiter der Verknüpfungsdaten
services   connections.Partner, links; sites, billing, clients …: caps.Store
sources    erkennen → caps.Set im Datensatz
caps       Typen, Deklarationen je Dienst, Store, reine Funktionen (Blatt)
```

`caps` importiert nichts aus Andon außer `enums`; alle Schichten dürfen es nutzen (wie `progress`).

## Schritte

Entschieden 06.10.2026: klein beginnen; wenn stabil, auf alle Verbindungen ausweiten und als Standard für jede Integration festlegen.

1. **Klein:** `internal/caps` mit Typen und Deklarationen nur für Orte und Fahrten (Dawarich, Kimai „Anfahrten“, Andon). Die Kimai-Quelle liefert dafür ein `caps.Set`; `pluginWrites` und `pluginTrips` werden zu `caps.Store`, ohne Verhaltensänderung, Tests je Reihenfolge.
2. Verbindungstest und Akte zeigen Fähigkeiten und fehlende Voraussetzungen.
3. Verknüpfungen: Tabellen `links`, `link_entries`, `connections.Partner`, „Verknüpft mit“; Kacheln, Prüflauf und Services stellen um.
4. Kunden Kimai ↔ Invoice Ninja: Zuordnung, Vorschlagsansicht, Angleichen.
5. **Groß:** übrige Kimai-Erkennung (Holiday-Bundle, Vertrag), dann jede Integration mit Erkennung; Regel in `agent.md`: neue Integrationen deklarieren ihre Fähigkeiten in `internal/caps`. Später statische Tabellen (`hooks.pushServices`, `connect.MethodOf`, Backup-Systeme) in dieselben Deklarationen.

## Nicht-Ziele

- Kein allgemeiner Synchronisationsmotor: Abgleiche bleiben eigene Services, nur die Entscheidungen werden einheitlich.
- Kein Löschen über Systeme hinweg (bleibt wie heute).
- Keine Fähigkeiten-API nach außen.

## Entscheidungen (06.10.2026)

1. **Partner:** Wahl nur, wenn es mehrere gibt. Gespeichert als eigene Verknüpfung (Option C), die auch Daten des Paars trägt.
2. **Private Orte:** Der Ort liegt in Dawarich und im Plugin (dort „Sonstiges“, das Plugin braucht einen Typ); nur die Markierung „privat“ lebt in Andon. Entspricht dem heutigen Code.
3. **Kunden Kimai ↔ Invoice Ninja:** feste Zuordnung über IDs mit Vorschlagsansicht, optional Namen angleichen; Invoice Ninja ist die Quelle der Namen.
4. **Umfang:** klein beginnen (Orte, Fahrten), wenn stabil groß und als Standard für alle Integrationen.
