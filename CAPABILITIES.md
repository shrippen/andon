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
| Ort: Art „privat“ | – | – | Andon |
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
- **Namensabgleich** (`~~`) ist als schwacher Verweis sichtbar und Kandidat für eine gespeicherte Zuordnung.

## Partner einheitlich finden

Eine Regel für Kacheln, Prüflauf und Services:

1. Ausdrücklich gesetzter Partner der Verbindung (neue Option `peers`, in der Akte wählbar).
2. Sonst die einzige Verbindung des Dienstes im selben Bereich.
3. Sonst keine, mit Hinweis „mehrere Kimai-Verbindungen, Partner wählen“, statt still die erste oder letzte zu nehmen.

Der Zugriff über Bereiche hinweg (Kacheln heute: „jeder erreichbare Bereich“) bleibt als Stufe 2b erhalten, wenn im eigenen Bereich keine ist.

## Schichten

```
web        Akte: Abschnitt Fähigkeiten, Partner wählen
services   sites, billing, receipts …: caps.Store, caps.Peer
sources    erkennen → caps.Set im Datensatz
caps       Typen, Deklarationen je Dienst, Store, reine Funktionen (Blatt)
```

`caps` importiert nichts aus Andon außer `enums`; alle Schichten dürfen es nutzen (wie `progress`).

## Schritte

1. `internal/caps` mit Typen und Deklarationen für Orte und Fahrten (Dawarich, Kimai, Andon). Kimai-Quelle liefert `caps.Set`. `pluginWrites` und `pluginTrips` werden zu `caps.Store`, ohne Verhaltensänderung, Tests je Reihenfolge.
2. Verbindungstest und Akte zeigen Fähigkeiten und fehlende Voraussetzungen, für alle Dienste mit Erkennung.
3. Partner-Regel und Option `peers`; Kacheln, Prüflauf und `sites` nutzen sie.
4. Weitere Domänen: Kunde (Kimai ↔ Ninja, Zuordnung statt Namensabgleich), Beleg (Ninja ↔ Paperless), Abwesenheit (Kimai-Holiday-Bundle).
5. Später, falls nützlich: statische Tabellen (`hooks.pushServices`, `connect.MethodOf`, Backup-Systeme) in dieselben Deklarationen.

## Nicht-Ziele

- Kein allgemeiner Synchronisationsmotor: Abgleiche bleiben eigene Services, nur die Entscheidungen werden einheitlich.
- Kein Löschen über Systeme hinweg (bleibt wie heute).
- Keine Fähigkeiten-API nach außen.

## Offene Fragen

1. **Partner**: ausdrücklich wählen (Option `peers`) oder automatisch mit Hinweis bei mehreren?
2. **Private Orte**: nur in Andon, oder zusätzlich in Dawarich markieren (Area-Name mit Präfix, da Dawarich keine Arten kennt)?
3. **Kunden-Identität Kimai ↔ Invoice Ninja**: Namensabgleich durch eine gespeicherte Zuordnung ersetzen (Schritt 4)?
4. **Umfang Schritt 1**: nur Orte und Fahrten, oder gleich die Kimai-Erkennung komplett (Holiday-Bundle, Vertrag)?
