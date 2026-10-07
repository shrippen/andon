# Roadmap: Andon, IT- & Freelance-Dashboard

Ein selbst gehostetes, **mehrbenutzerfähiges** Dashboard. Es löst Dashy als **Startseite mit Links, Statusanzeigen und Feeds** ab, führt zugleich Daten aus **Kimai**, **Invoice Ninja**, **Snipe-IT** und **Dawarich** zusammen und leitet daraus **Hinweise, Erinnerungen und Ratschläge** ab. Konfiguriert wird im **eingebauten Editor**, gestaltet über ein **Theme-System**, von dem nur das Theme **Kante** mitgeliefert wird. Auslieferung als **ein Docker-Container** mit eigener Anmeldung.

> Stand: v0.4 · Name **Andon** (seit 2026-09-26, vorher Arbeitstitel `dashboard`): die Signaltafel aus der Fertigung, die zeigt, wo es hakt.
>
> **Veröffentlichung:** Quelltext und Entwicklung auf `git.arianw.de/shrippen/andon` (privat, Image `git.arianw.de/shrippen/andon`). Gitea spiegelt `main` und Tags nach `github.com/shrippen/andon`; dort baut `.github/workflows/ci.yml` das öffentliche Image `ghcr.io/shrippen/andon`, und GitHub Pages zeigt die Landing Page aus `docs/` unter `shrippen.github.io/andon/`.
>
> **Arbeitsort:** Die gesamte Entwicklung findet in diesem Repository (`shrippen/andon`) statt. Das Design-System-Repo `shrippen/shrippen.github.io` ist nur Quelle, es wird von hier aus nicht verändert.

---

## Umsetzungsstand (2026-09-25)

Das Projekt ist vollständig von Python auf **Go** umgestellt (Zielplattform: Raspberry Pi, `net/http` + `html/template` + htmx, `ncruces/go-sqlite3` (WebAssembly) mit Adiantum-Verschlüsselung — kein cgo, kein C-Toolchain nötig). Die frühere Python-Fassung ist nur noch in der Git-Historie vorhanden. Getestet mit gemockten API-Antworten (`httptest`), **nicht gegen echte Instanzen**. Vor dem Produktivbetrieb die Verbindungstests je Dienst ausführen und die Hinweise auf Plausibilität prüfen.

| Bereich | Stand |
|---|---|
| Anmeldung (Passwort, TOTP, Passkeys, OIDC), Teams, Rechte, Freigaben, Overlays | umgesetzt |
| Editor, Bibliothek, Revisionen, YAML-/Dashy-Import, Code-Ansicht (CodeMirror) | umgesetzt |
| Themes (Editor, Import/Export, Schriften, Styleguide, WCAG-AA-Prüfung) | umgesetzt |
| Kimai, Invoice Ninja, Snipe-IT, Dawarich | Adapter, Regeln, Insight-Widgets umgesetzt |
| Homelab-Dienste (Phase 10) | 17 weitere Quellen mit Regeln; Docker offen |
| IT-Doku-Abgleich (Phase 15) | Compose-Stacks und Vault-Frontmatter über Gitea, Regeln `docs.*`, Kacheln, `/api/docs` und Hansei-Webhook umgesetzt; `docs.drift`, Komodo, Homelable offen |
| Prüflauf | Hintergrund-Job holt alle Integrationen (Start + alle `ANALYSIS_MINUTES`); Seiten zeigen nur diesen Stand, live nur der Status-Ping |
| Benachrichtigungen | Apprise, Digest-Mail (SMTP), Wochenrückblick mit optionaler LLM-Zusammenfassung, iCal |
| Trends, Prognosen | Snapshots, Verlauf, Saisonvergleich, Jahresprognose, Liquidität |
| Betrieb | CLI `backup`, `rotate-key`, `import`; Demo-Modus; Icons-Dienst |
| Anmelden statt Token | Home Assistant, Nextcloud, Jellyfin, Gitea, Snipe-IT, Tailscale: gleichwertig neben dem Token; ablaufende Tokens erneuert Andon (`oauth_grants`) |
| Produktivbetrieb | Umstieg von Dashy abgeschlossen (Abschnitt 7.4, 06.10.2026) |

**Abweichungen vom Plan:** Übersetzungen als YAML-Kataloge mit Schlüsseln (unverändert vom Python-Stand übernommen). Das mitgelieferte Theme liegt in `internal/web/templates/` (Builtin, eingebettet). Board-Vorlagen/Revisionen speichern den Board- bzw. Widget-eigenen Zustand, nicht die bereichsübergreifende YAML-Form aus `porting.py`.

**Bekannte Unsicherheiten:** Invoice Ninja v5 liefert IDs als Hash-Strings; Kunden-IDs werden daraus stabil in Zahlen umgerechnet, der Originalschlüssel bleibt für Schreibzugriffe erhalten. Deep-Links in Invoice Ninja (`/#/invoices/<id>/edit`), Snipe-IT meldet keine Version, Dawarich-Felder für Besuche (`area_id`, `place`) werden tolerant gelesen.

---

## 1. Ziele und Nicht-Ziele

**Ziele**

- Ein Blick am Morgen genügt: Was brennt (überfällige Rechnungen, ablaufende Garantien), was ist offen (nicht abgerechnete Stunden), wie läuft das Jahr (Umsatz, Auslastung, Stundensatz).
- Hinweise sind **regelbasiert, erklärbar und konfigurierbar**: Jede Meldung sagt, *warum* sie erscheint, welche Daten dahinterstehen und wohin man klickt, um sie zu erledigen.
- **Querverbindungen** zwischen den Diensten sind der eigentliche Mehrwert (z. B. „Du warst laut Dawarich 6 h bei Kunde X, in Kimai ist nichts gebucht“).
- **Startseite wie bisher:** Die täglich genutzten Dashy-Funktionen (Links, Icons, Status, Suche, RSS, Uhr, Wetter) sind übernommen, sodass Dashy abgeschaltet werden kann.
- **Mehrere Benutzer und Teams:** Benutzer arbeiten völlig getrennt voneinander oder teilen sich in Teams Verbindungen, Widgets und Boards. Rechte lassen sich bis auf einzelne Widgets vergeben, jeder hat sein eigenes Layout.
- **Alles in der Oberfläche einstellbar:** Konfigurationseditor mit Formularen, Drag & Drop und Code-Ansicht, Versionsverlauf und Import/Export.
- **Eigene Themes:** Themes lassen sich in der Oberfläche anlegen und bearbeiten. Mitgeliefert wird nur das Kante-Theme (dunkel und „Leinen“).
- **Eigene Anmeldung** im Dashboard, unabhängig vom Reverse Proxy.
- Nur **lesender** Zugriff auf die Dienste. Das Dashboard verändert nichts in Kimai, Invoice Ninja usw.
- Ein Container, ein Volume.

**Nicht-Ziele (vorerst)**

- Kein Ersatz für die Fach-UIs (Rechnungen schreiben bleibt in Invoice Ninja).
- Keine Mandantentrennung auf Instanzebene (mehrere voneinander unabhängige Organisationen mit eigenen Admins). Teams und getrennte Benutzer innerhalb einer Instanz reichen.
- Keine Theme-Galerie oder Weitergabe von Themes über das Internet; Themes werden als Datei exportiert und importiert.
- Keine Steuerberatung: Steuerhinweise sind Erinnerungen mit konfigurierbaren Werten, keine verbindliche Auskunft.

---

## 2. Grundlage: Dashy erweitern oder eigene App?

| Kriterium | Dashy als Basis | Homepage / Glance | **Eigene App (empfohlen)** |
|---|---|---|---|
| Links/Startseite | sehr gut | sehr gut | wird nachgebaut (nur genutzte Funktionen, Import aus Dashy) |
| Eigene Widgets | Vue-2-Komponenten, Fork + eigener Build nötig | `customapi`/`custom-api`-Widgets, nur Anzeige | frei |
| API-Aufrufe | größtenteils im Browser (CORS-Proxy, Tokens im Frontend) | serverseitig | serverseitig, Tokens verschlüsselt im Container |
| Mehrbenutzer, Teams, Rechte je Widget | nur einfache Benutzer/Gäste | nein | ja |
| Konfigurationseditor | ja, für eine gemeinsame Datei | nein | ja, je Benutzer/Team, mit Verlauf |
| Verlauf/Trends, Regel-Engine, Push/Digest | nein | nein | ja |
| Styling nach Design System, eigene Themes | Custom-CSS-Theme, begrenzt | Custom-CSS, begrenzt | Theme-System auf Basis der Design-Tokens |

**Empfehlung:** Eigene App mit Backend. Hinweise, Mehrbenutzerbetrieb mit Rechten auf Widget-Ebene und ein Editor, der pro Benutzer oder Team speichert, lassen sich auf keinem der Startseiten-Tools sauber aufsetzen.

**Dashy wird abgelöst:** Die wahrscheinlich genutzten Dashy-Funktionen werden migriert (Abschnitt 7), die `conf.yml` wird über einen Import-Assistenten übernommen. Bis zum Umstieg laufen beide parallel; für diese Zeit lassen sich Hinweise und Kennzahlen über `/embed/*` (mit persönlichem Embed-Token) auch in Dashy einbetten.

---

## 3. Architektur

```
┌──────────────────────────────── Docker-Container ─────────────────────────────────┐
│                                                                                   │
│  Anmeldung ─► Sitzung ─► Berechtigungsprüfung (jede Anfrage, jedes Widget-Fragment) │
│                                   │                                               │
│  Web-UI (html/template + HTMX) ◄──┼──────── Boards, Widgets, Themes, Editor        │
│  JSON-API /api/*  ·  /embed/*     │                                               │
│                                   ▼                                               │
│  Scheduler ─► Quellen ─► Cache/Snapshots ─► Kennzahlen ─► Regeln ─► Hinweise       │
│               (je Verbindung    (SQLite,                        (Zustand je        │
│                und Zugangsdaten) /data)                          Benutzer/Team)    │
│                                                                      │            │
│                                                   Benachrichtigungen ◄┘ (je Benutzer)│
└───────────────────────────────────────────────────────────────────────────────────┘
```

**Grundprinzip:** Getrennte Schichten.

- **Quellen** holen Daten, nur auf dem Server, mit Cache und Timeout. Ein RSS-Feed oder eine Statusprüfung ist dabei eine Quelle wie Kimai.
- **Widgets** zeigen nur an und rufen nie selbst etwas ab.
- **Regeln** lesen dieselben Daten und erzeugen Hinweise.
- **Berechtigungen** werden zentral in der Service-Schicht geprüft, nie nur in Vorlagen. Jede Datenbankabfrage ist auf die Bereiche beschränkt, die der Benutzer sehen darf.

**Datenbank statt Konfigurationsdatei:** Weil Editor und Mehrbenutzerbetrieb schreiben, ist die **Datenbank die Quelle der Wahrheit** für Boards, Widgets, Verbindungen, Themes und Rechte. YAML bleibt als Import-/Export-Format und für eine optionale Erstbefüllung (`seed.yml`). Über Umgebungsvariablen kommen nur Betriebswerte (Basis-URL, Datenbank, Hauptschlüssel, SMTP).

**Stack** (Go; bis v0.3 war es Python/FastAPI, siehe Git-Historie)

| Schicht | Wahl | Begründung |
|---|---|---|
| Sprache | Go (aktuelle Stable), ein statisches Binary ohne cgo | läuft auf dem Raspberry Pi, kein C-Toolchain nötig |
| Web | `net/http` + `html/template` + htmx | serverseitig gerendert, kaum JavaScript, passt zum CSS-only Design System |
| Datenbank | `ncruces/go-sqlite3` (SQLite als WebAssembly) mit Adiantum-Verschlüsselung, WAL | ein Volume, einfaches Backup; Migrationen als SQL-Dateien |
| Anmeldung | Argon2id, serverseitige Sitzungen, `pquerna/otp` (TOTP), eigener OIDC-Client (authentik), `go-webauthn` (Passkeys) | eigene Anmeldung, dazu Single Sign-on über authentik |
| Übersetzung | YAML-Kataloge mit Schlüsseln (`internal/i18n/catalogs`) | Oberfläche, Hinweise und E-Mails auf Deutsch und Englisch; Beträge und Daten im Format der Sprache |
| Benachrichtigungen | Apprise (API), SMTP für Digest | ein Baustein für alle Kanäle, je Benutzer eine Liste von Apprise-URLs |
| Geheimnisse | AES-GCM (`internal/crypto`), Hauptschlüssel aus Docker Secret | Zugangsdaten verschlüsselt in der Datenbank |
| HTTP | eigener Client mit Egress-Schutz beim Verbindungsaufbau (`internal/drivers/httpclient`) | Timeouts, geteilte Verbindungen, keine Umgehung über Weiterleitungen |
| Zeitplan | eigener Scheduler (`internal/services/scheduler`) | ein Ticker je Job, Fehler und Panics isoliert |
| Editor | SortableJS und CodeMirror 5 (vorgebaut, im Repo) | Drag & Drop ohne Framework und ohne Build-Kette |
| Diagramme | SVG aus den Templates, Farben aus den Theme-Tokens | keine Build-Kette, kein Diagramm-Paket |
| KI (optional) | `anthropic-sdk-go` | Wochenzusammenfassung, „Was tun?“, Rechnungen in Mails lesen |

**Datenfluss**

1. **Quelle** holt Rohdaten über eine **Verbindung** und normalisiert sie in Pydantic-Modelle (`TimeEntry`, `Invoice`, `Asset`, `License`, `Visit` …). Der Cache-Schlüssel ist *Verbindung + Zugangsdaten*: Eine geteilte Verbindung wird einmal abgerufen, eine Verbindung mit persönlichen Zugangsdaten einmal je Benutzer.
2. **Snapshot-Store** legt pro Lauf einen Zeitstempel-Snapshot ab (für Trends und „seit gestern neu“).
3. **Kennzahlen** werden daraus berechnet (Umsatz YTD, Auslastung, offene Posten …).
4. **Regeln** laufen je Bereich über Kennzahlen und Rohdaten und erzeugen **Hinweise** mit stabilem Fingerprint (`regel-id + objekt-id`). Ein Hinweis bleibt so über Läufe hinweg derselbe, kann quittiert oder bis zu einem Datum pausiert werden und verschwindet von selbst, wenn die Bedingung wegfällt.
5. **Widgets** und **Benachrichtigungen** lesen nur Cache, Hinweise und Kennzahlen, und zwar nur für Benutzer mit Zugriff.

**Hinweis-Modell**

```yaml
id: kimai.unbilled_hours:customer-12
space: personal:alex    # Bereich, zu dem der Hinweis gehört
severity: warn          # info | warn | critical
message: kimai.unbilled_hours          # Übersetzungsschlüssel, kein fertiger Text
params: { hours: 38, customer: "Muster GmbH", days: 30, oldest: 2026-08-12 }
action: { label: open_in_kimai, url: "https://kimai.example/…" }
due: 2026-10-10         # optional, für Fristen
source: [kimai, invoiceninja]
state: open             # open | snoozed | acknowledged | resolved (je Benutzer oder für das Team)
```

Hinweise speichern **Schlüssel und Parameter statt fertiger Sätze**. So erscheint derselbe Hinweis für jeden Benutzer in seiner Sprache („38 h bei Kunde Muster GmbH noch nicht abgerechnet“ / „38 h for Muster GmbH not yet billed“), und Zahlen und Datumsangaben werden passend formatiert.

**Regeln** gibt es in zwei Formen:

- **Einstellbar** im Editor für einfache Schwellwerte (Tage, Beträge, Prozent), je Bereich.
- **Als Python-Klasse** für Regeln über mehrere Dienste hinweg (Abgleich Dawarich ↔ Kimai). Jede Regel hat ID, Standard-Schwellwerte, Beschreibung und einen Unit-Test mit Fixture-Daten.

---

## 4. Mehrbenutzer, Anmeldung und Berechtigungen

### 4.1 Datenmodell

```
Instanz
├── Benutzer ──(Mitglied mit Rolle)──► Team
├── Bereiche (Spaces)
│   ├── persönlich   — genau einer je Benutzer, nur für ihn sichtbar
│   ├── Team         — einer je Team, für alle Mitglieder gemäß Rolle
│   └── Instanz      — global, von Instanz-Admins gepflegt (z. B. gemeinsame Links, Standard-Theme)
│
│   Jeder Bereich enthält:
│   ├── Verbindungen      Dienst-URL + Zugangsdaten (verschlüsselt), z. B. „Kimai Firma“
│   ├── Widget-Bibliothek konfigurierte Widgets (Typ + Einstellungen + Verbindung)
│   ├── Boards            Seiten → Abschnitte → Platzierungen (Verweise auf Widgets)
│   ├── Regel-Einstellungen, Fristen, Ziele
│   └── Themes
│
├── Freigaben   Ressource → Benutzer oder Team, mit Recht
└── Persönliche Overlays   Layout-Anpassungen eines Benutzers an fremden Boards
```

### 4.2 Rollen und Rechte

**Instanz-Rollen**

| Rolle | Darf |
|---|---|
| Instanz-Admin | Benutzer und Teams verwalten, Einladungen, Instanz-Bereich, Instanz-Themes inkl. eigenem CSS und Schriften, Anmeldeeinstellungen, Audit-Log. **Sieht keine persönlichen Bereiche anderer** (kein Einblick in Standortdaten oder Umsätze, kein „Anmelden als“) |
| Benutzer | Eigener persönlicher Bereich, Mitgliedschaften in Teams |

**Team-Rollen**

| Rolle | Darf |
|---|---|
| Owner | Mitglieder und Rollen verwalten, Verbindungen mit Zugangsdaten anlegen, alles im Team-Bereich |
| Editor | Widgets und Boards im Team-Bereich anlegen und ändern, vorhandene Verbindungen nutzen |
| Viewer | Team-Boards ansehen, eigenes Layout-Overlay anlegen |

**Rechte auf einzelne Ressourcen** (Freigaben, zusätzlich zur Rolle; auch in einen anderen Bereich hinein):

| Recht | Bedeutung |
|---|---|
| `view` | Widget/Board sehen, seine Daten anzeigen |
| `use` | Widget auf eigenen Boards platzieren; Verbindung in eigenen Widgets verwenden (ohne die Zugangsdaten je zu sehen) |
| `edit` | Einstellungen ändern |
| `manage` | Freigeben, löschen |

Rechte lassen sich innerhalb eines Teams auch **einschränken**: Ein Team-Widget „Umsatz“ kann z. B. nur für Owner sichtbar sein, obwohl das Board allen Mitgliedern gehört. Wer ein Widget nicht sehen darf, bekommt es gar nicht erst ausgeliefert, auch nicht als leere Kachel.

### 4.3 Wiederverwendung und Layouts

- **Widget-Bibliothek:** Ein Widget wird einmal eingerichtet (z. B. „Offene Rechnungen“) und auf beliebig vielen Boards **platziert**. Platzierungen sind Verweise: Eine Änderung am Widget wirkt überall. Alternativ „Als Kopie übernehmen“ für eine unabhängige Variante.
- **Team-Widgets im persönlichen Board:** Mit `use` lässt sich ein Team-Widget auf das eigene Start-Board legen. Wird das Recht entzogen, zeigt die Platzierung „kein Zugriff mehr“ statt Daten.
- **Persönliche Layouts:** Jeder Benutzer hat eigene Boards und ein eigenes Start-Board. An Team-Boards kann er ein **Overlay** anlegen (Reihenfolge, Größe, eingeklappt, ausgeblendet), ohne das Board für andere zu ändern. „Auf Team-Layout zurücksetzen“ löscht das Overlay.
- **Persönliche Einstellungen:** Theme, hell/dunkel, Sprache, Start-Board, Benachrichtigungskanäle, Ruhezeiten, eigene Suchmaschine.
- **Vorlagen:** Boards lassen sich als Vorlage speichern (ohne Zugangsdaten) und von anderen Benutzern oder Teams übernehmen, z. B. „Freelance-Übersicht“.

### 4.4 Verbindungen: Ebenen, fest und Vorlage

Eine Verbindung gehört zu genau einer Ebene und ist **fest** oder eine **Vorlage**. Wer sie sieht, folgt aus der Ebene; Freigaben für Verbindungen gibt es nicht.

| Ebene | Fest (ein Zugang) | Vorlage (Zugang je Person) |
|---|---|---|
| **Instanz** | Admins legen an, alle nutzen denselben Zugang | Admins legen an; jede Person trägt ihren Zugang ein, oder ein Team-Owner einen für sein Team |
| **Team** | Team-Owner legen an, das Team nutzt denselben Zugang | Team-Owner legen an, jedes Mitglied trägt seinen Zugang ein |
| **Persönlich** | eigene Verbindung, nur für die Besitzerin | – |

Fest heißt **geteilte Daten**: Wer eine Kachel auf der Verbindung sehen darf, sieht, was ihr Token sieht. Eine Vorlage zeigt jedem seine eigenen Daten; wer keinen Zugang eingetragen hat, sieht „Zugang einrichten“. Kacheln im Team-Bereich nutzen für eine Instanz-Vorlage den Team-Zugang, eigene Boards den eigenen.

Ändert ein Admin eine Vorlage (Adresse, Zertifikat, Optionen), pausieren alle Aktivierungen: Die Kacheln zeigen „neu aktivieren“, die Seite *Verbindungen* zeigt Bisher und Jetzt je Feld. Bleibt der Server gleich, reicht „Aktivieren“; ein neuer Server verlangt den Zugang neu, der alte wird nicht dorthin geschickt.

**Dawarich** ist außerhalb des persönlichen Bereichs nur als Vorlage erlaubt; fest muss ein Instanz-Admin für die Instanz einschalten.

### 4.5 Szenarien

| Szenario | Umsetzung |
|---|---|
| **Autark:** mehrere Personen auf einer Instanz, nichts gemeinsam | Jeder nutzt nur seinen persönlichen Bereich mit eigenen Verbindungen. Teams bleiben leer. Der Admin sieht nur Konten, keine Inhalte |
| **Team:** gemeinsame IT-Landschaft | Team-Bereich mit geteilter Snipe-IT-Verbindung, Team-Boards „IT“ und „Links“, Rechte nach Rolle |
| **Gemischt:** Freelancer mit kleinem Team | Persönlicher Bereich für Umsatz, Steuer und Dawarich; Team-Bereich für Links, Snipe-IT und Kimai mit persönlichen Zugangsdaten; Team-Widgets auf dem eigenen Start-Board |

Hinweise gehören zu einem Bereich. Hinweise aus persönlichen Bereichen haben persönlichen Zustand. Bei Team-Hinweisen stellt der Team-Owner ein, ob „quittiert“ für das ganze Team gilt (z. B. „Audit überfällig“) oder für jeden einzeln.

### 4.6 Anmeldung

Das Dashboard hat eine **eigene Anmeldung**. Der Reverse Proxy übernimmt nur TLS, keine Authentifizierung; Header-basierte Proxy-Anmeldung wird bewusst nicht unterstützt.

- **Erstes Konto:** Beim ersten Start gibt der Container einen einmaligen Einrichtungscode im Log aus. Nur damit lässt sich der erste Instanz-Admin anlegen, sodass niemand eine frisch gestartete Instanz übernehmen kann.
- **Konten:** Einladungen per E-Mail durch Admins (Standard), Selbstregistrierung abschaltbar (Standard: aus). Alternativ legt die erste Anmeldung über authentik das Konto an (Abschnitt 4.7).
- **Passwörter:** Argon2id, Mindestlänge 12, keine Kompositionsregeln. Zurücksetzen per E-Mail (zeitlich begrenzter Einmal-Link); ein Admin kann zusätzlich einen neuen Einladungslink erzeugen.
- **Zweiter Faktor:** TOTP mit Wiederherstellungscodes für lokale Konten; für Admins erzwingbar. Bei Anmeldung über authentik übernimmt authentik den zweiten Faktor. Später Passkeys (WebAuthn).
- **Sicherheitsmeldungen per E-Mail:** Anmeldung von neuem Gerät, geändertes Passwort, 2FA an/aus, neues API-Token.
- **Sitzungen:** serverseitig in der Datenbank, Cookie `HttpOnly; Secure; SameSite=Lax`, neue Sitzungs-ID bei Anmeldung, Leerlauf- und absolutes Zeitlimit, Liste aktiver Sitzungen mit „abmelden“.
- **Schutz:** CSRF-Token für Formulare und HTMX-Anfragen, Drosselung je IP und Konto, gleiche Fehlermeldung für falschen Benutzer und falsches Passwort.
- **API- und Embed-Tokens:** Persönliche Tokens mit Ablaufdatum und eingeschränktem Umfang (nur lesen, nur bestimmte Boards), z. B. für das Dashy-iframe während des Umstiegs oder Skripte.
- **Audit-Log:** Anmeldungen (lokal und authentik), fehlgeschlagene Versuche, Änderungen an Rechten, Verbindungen und Zugangsdaten, Board-Änderungen.

### 4.7 Single Sign-on mit authentik (OIDC)

authentik ist ein **zusätzlicher** Anmeldeweg. Konten, Teams und Rechte bleiben im Dashboard; die lokale Anmeldung bleibt als Notzugang erhalten.

**In authentik:** Provider vom Typ *OAuth2/OpenID Provider* (vertraulicher Client) und eine Application, z. B. mit Slug `dashboard`.

| Einstellung | Wert |
|---|---|
| Redirect URI | `https://dashboard.example.lan/auth/oidc/callback` |
| Scopes | `openid`, `profile`, `email` (die Standard-Zuordnung für `profile` liefert auch `groups`) |
| Issuer / Discovery | `https://auth.example.lan/application/o/dashboard/` bzw. `…/.well-known/openid-configuration` |
| Zugriff | Binding an die Gruppe `dashboard-users`, damit nur diese Benutzer sich anmelden können |

**Im Dashboard** (Admin-Einstellungen, alternativ per Env beim ersten Start):

- Issuer-URL, Client-ID, Client-Secret (verschlüsselt gespeichert). Knopf „Verbindung testen“ lädt die Discovery und prüft die Schlüssel.
- Ablauf: Authorization Code Flow mit PKCE, `state` und `nonce`; das ID-Token wird gegen die JWKS von authentik geprüft (Signatur, Issuer, Audience, Ablauf).
- **Kontozuordnung** über `sub` (stabil, ändert sich nicht bei Umbenennung). Bestehende lokale Konten werden verknüpft, indem der Benutzer angemeldet in seinem Profil „Mit authentik verknüpfen“ wählt. Eine automatische Zuordnung über die E-Mail-Adresse nur, wenn authentik `email_verified` liefert und der Admin es erlaubt.
- **Automatisches Anlegen:** Die erste Anmeldung über authentik legt ein Konto samt persönlichem Bereich an (abschaltbar).
- **Gruppen → Rollen und Teams, nur beim Anlegen:** Eine Zuordnungstabelle in den Admin-Einstellungen bestimmt die *Startwerte* des neuen Kontos, z. B. `dashboard-admins` → Instanz-Admin, `team-it` → Team „IT“ als Editor, `team-buero` → Team „Büro“ als Viewer. Danach gehören Rollen und Mitgliedschaften dem Dashboard: Admins und Team-Owner ändern sie von Hand, spätere Anmeldungen überschreiben nichts. Ändern sich die Gruppen in authentik, bleibt das Konto unverändert; ein Admin kann bei Bedarf pro Benutzer „Startwerte aus authentik neu übernehmen“ auslösen (mit Vorschau der Änderungen, im Audit-Log vermerkt).
- **Modus „nur authentik“:** blendet das Passwortfeld aus. Ausgenommen sind als Notzugang markierte lokale Admin-Konten (erreichbar über `/login?local`), damit ein Ausfall von authentik nicht aussperrt.
- **Abmelden:** beendet die Dashboard-Sitzung und leitet optional an den `end_session_endpoint` von authentik weiter.
- **Deaktivierte Benutzer:** Sitzungen über authentik haben eine kürzere absolute Laufzeit (Standard 12 h). Wer in authentik gesperrt wird, kommt danach nicht mehr hinein.

---

## 5. Konfigurationseditor

Alles, was vorher in `config.yml` stand, wird in der Oberfläche eingestellt. Bearbeiten darf, wer im jeweiligen Bereich `edit` hat.

| Teil | Funktion |
|---|---|
| **Board-Editor** | Bearbeitungsmodus direkt auf dem Board: Abschnitte anlegen, Widgets per Drag & Drop anordnen, Spalten und Größen, Widget aus der Bibliothek einfügen oder neu anlegen. Bei fremden Boards ohne `edit` bearbeitet derselbe Modus das persönliche Overlay |
| **Widget-Formulare** | Automatisch aus dem Pydantic-Schema des Widget-Typs erzeugt: Felder, Hilfetexte, Validierung. Auswahl der Verbindung zeigt nur Verbindungen mit `use`. **Vorschau** rendert das Widget mit den ungespeicherten Einstellungen |
| **Verbindungen** | Dienst, URL, Art der Zugangsdaten (geteilt/persönlich), Token-Feld nur schreibbar (gespeicherte Tokens werden nie angezeigt), Knopf „Verbindung testen“ |
| **Regeln und Ziele** | Schwellwerte, Ziele, Fristen und Steuerwerte je Bereich, mit Standardwerten und Erklärung |
| **Code-Ansicht** | Board oder Bereich als YAML bearbeiten; validiert gegen dieselben Schemas, Fehler mit Zeilennummer. Zunächst Textfeld mit serverseitiger Prüfung, später CodeMirror |
| **Verlauf** | Jede Speicherung ist eine Revision (wer, wann, Unterschiede). Wiederherstellen einer älteren Revision. Gleichzeitiges Bearbeiten wird über eine Versionsnummer erkannt („wurde inzwischen von X geändert“) |
| **Import/Export** | YAML je Board oder Bereich (ohne Zugangsdaten, dafür Platzhalter); Dashy-Import als Assistent; Theme-Import/-Export |
| **Erstbefüllung** | Optional `seed.yml` beim ersten Start, um eine Instanz reproduzierbar aufzusetzen (Konfiguration als Code) |
| **Rechte und Freigaben** | Dialog „Freigeben“ an jedem Widget, Board und jeder Verbindung; Übersicht „Wer hat Zugriff?“ |

---

## 6. Themes

### 6.1 Theme-Modell

Ein Theme ist ein **Satz von Design-Tokens** für den dunklen und den hellen Modus, dazu optional Schriften und eigenes CSS. Alle Komponenten verwenden ausschließlich Tokens (`var(--…)`), daher funktioniert jedes Theme mit jedem Widget.

- **Theme-Vertrag:** Die Token-Liste aus `tokens/variables.css` des Design Systems (Hintergründe, Text, Akzent, Semantik, Rollen wie `--field`, `--hl`, Radius, Schriften) ist die versionierte Theme-Schnittstelle. Neue Tokens bekommen Standardwerte, damit ältere Themes weiter funktionieren.
- **Mitgeliefert:** nur **Kante** (dunkel = Standard, hell = „Leinen“). Es ist schreibgeschützt; Änderungen beginnen mit „Duplizieren“.
- **Format** für Import/Export als ZIP:

```
mein-theme.zip
├── theme.json     ← Name, Version, Autor, Theme-Vertrag-Version, Modi
├── tokens.css     ← :root { … } und :root[data-theme="light"] { … }
├── custom.css     ← optional, nur Instanz-Admins
└── fonts/         ← optional, woff2, nur Instanz-Admins
```

### 6.2 Theme-Editor

- Token-Gruppen mit Farbwählern und Zahlenfeldern, dunkel und hell nebeneinander.
- **Live-Vorschau** an einem Beispiel-Board und an der Komponenten-Übersicht (`/styleguide`).
- **Kontrastprüfung** aller Text-/Hintergrund-Paare nach WCAG AA mit Warnungen, bevor gespeichert wird.
- Schriftwahl aus den mitgelieferten und hochgeladenen Schriften.

### 6.3 Wo Themes gelten

- Themes liegen in Bereichen: Instanz-Themes für alle, Team-Themes für Mitglieder, persönliche Themes nur für den Ersteller.
- Auswahl in dieser Reihenfolge: **persönliche Wahl → Team-Standard → Instanz-Standard (Kante)**. Team-Boards können optional ein Theme erzwingen (z. B. für einen Wandbildschirm).
- **Sicherheit:** Normale Benutzer ändern nur Token-Werte, die serverseitig geprüft werden (Farben, Längen, Schriftnamen aus der Liste). Eigenes CSS und Schriften nur für Instanz-Admins. Die Content-Security-Policy (`img-src 'self' data:`, `font-src 'self'`, `connect-src 'self'`) verhindert, dass CSS Daten nach außen lädt.
- Ein Stylelint-Check im Repo verbietet feste Farbwerte außerhalb der Token-Dateien, damit der Theme-Vertrag hält.

---

## 7. Startseite: Migration der Dashy-Funktionen

Das Dashboard übernimmt die Rolle von Dashy als Startseite. Migriert werden die Funktionen, die in einem typischen Homelab-Dashy tatsächlich genutzt werden. Alles andere ist bewusst ausgelassen oder durch etwas Einfacheres ersetzt.

### 7.1 Funktionsumfang

| Dashy-Funktion | Umsetzung im Dashboard | Priorität |
|---|---|---|
| Seiten (`pages`), Abschnitte (`sections`), Einträge (`items`) | Boards → Abschnitte → Widgets | Muss |
| Eintrag: `title`, `description`, `url`, `icon`, `target` (`newtab`, `sametab`) | Widget `link`; `modal` und `workspace` entfallen (öffnen als neuer Tab) | Muss |
| Icons: `favicon`, `si-*` (Simple Icons), `hl-*` (Dashboard Icons), URL, lokale Datei | Server holt das Icon einmal, bereinigt SVGs und legt es unter `/data/icons` ab; Upload im Editor; dazu `sh-*` (selfh.st), `mdi-*` (Material Design), Font Awesome (`fas fa-*`) und Emoji; einfarbige Sätze werden im dunklen Modus invertiert | Muss |
| Statusprüfung (`statusCheck`, `statusCheckUrl`, `statusCheckAcceptCodes`, `statusCheckAllowInsecure`, `statusCheckInterval`) | Quelle `http_status` auf dem Server; Punkt auf der Kachel mit Antwortzeit im Tooltip; Status nie nur über Farbe | Muss |
| Suche/Filter durch Tippen, Tastenkürzel je Eintrag (`hotkey`) | Suchfeld (`/` fokussiert), filtert Kacheln live, `Enter` öffnet den ersten Treffer, Ziffern-Hotkeys | Muss |
| Websuche als Rückfall (`webSearch`, `searchEngine`) | Keine Treffer → Suche an konfigurierte Suchmaschine (je Benutzer einstellbar) | Muss |
| Abschnitt-Anzeige (`collapsed`, `cols`, `itemSize`, `sortBy`) | `collapsed`, `cols`, `size: small|medium|large`, `sort: manual|alphabetical`; eingeklappt/ausgeklappt im persönlichen Overlay gespeichert | Muss |
| Seitenkopf und Fuß (`pageInfo`: Titel, Beschreibung, Nav-Links, Footer) | Einstellungen des Bereichs/der Instanz, gerendert mit `.nav` und `.foot` | Muss |
| Widget `rss-feed` | Widget `rss`: Abruf und Bereinigung auf dem Server, Cache, Anzahl und Intervall einstellbar | Muss |
| Widget `clock` | Widget `clock`: rein im Browser, Zeitzonen, Datum | Muss |
| Widget `weather` / `weather-forecast` | Widget `weather` über Open-Meteo (kein API-Key); OpenWeatherMap optional | Muss |
| Themes, Theme-Wechsler, Custom CSS | Theme-System mit Editor (Abschnitt 6); mitgeliefert nur Kante | Muss |
| Konfigurations-Editor in der UI | Konfigurationseditor (Abschnitt 5), je Bereich, mit Verlauf | Muss |
| Anmeldung, Gast-Sichtbarkeit (`hideForGuests`, `hideForUsers` …) | Eigene Anmeldung und Rechte je Widget (Abschnitt 4) | Muss |
| Widget `iframe` | Widget `iframe`; erlaubte Ziele pflegt ein Instanz-Admin, sie landen in der Content-Security-Policy (`frame-src`) | Soll |
| Widgets `gl-*` (Glances: CPU, RAM, Disk, Load) | Quelle `glances` + Widget `sysinfo` (passt zur IT-Landschaft) | Soll |
| Widget `public-ip` | Widget `public_ip` | Soll |
| Minimal-Ansicht (`/minimal`) | Board-Ansicht `?view=compact`: nur Suche und Kacheln | Soll |
| Als App installieren (PWA) | Web-App-Manifest und Icon | Soll |
| Cloud-Backup der Konfiguration | Export als YAML/ZIP, Datenbank-Backup des Volumes | Nein |
| Keycloak-Anbindung | Single Sign-on über authentik per OIDC (Abschnitt 4.7) | Muss |
| Übrige Dashy-Widgets | Krypto (Phase 12), GitHub-Trends (`github_trending`, GitHub-Suche: neue Repos des Zeitraums nach Sternen; GitHub hat keine Trending-API) und Sport (`sports`, OpenLigaDB: Spieltag, Spiele einer Mannschaft, Tabelle) gibt es; der Import ordnet `github-trending-repos` und `sports-scores` zu (Team-IDs von TheSportsDB nicht übertragbar, der Bericht sagt es). Weitere listet der Import-Assistent | Erledigt (06.10.2026) |

### 7.2 Widget-Modell

Startseite und Auswertung nutzen dasselbe Modell (siehe Architektur): **Quellen** holen Daten auf dem Server, **Widgets** zeigen sie nur an.

| Widget | Quelle | Aktualisierung |
|---|---|---|
| `link` | `http_status` (optional), optionale Infozeile aus einer Dienst-Quelle (`info: kimai.today`) | Status alle 5 min |
| `rss` | `rss` | 30 min |
| `clock` | keine (Browser) | sekündlich im Browser |
| `weather` | `open_meteo` | 30 min |
| `iframe` | keine | – |
| `sysinfo` | `glances` | 1 min |
| `public_ip` | `public_ip` | 1 h |
| `kpi`, `hints`, `table`, `chart` | Dienst-Quellen, Regeln | je Quelle |

Jedes Widget wird als eigenes HTMX-Fragment geladen und aktualisiert; jede Fragment-Anfrage prüft die Rechte erneut. Fällt eine Quelle aus, zeigt nur dieses Widget den Fehler und den letzten Stand mit Alter an.

**Link-Kachel mit Infozeile:** Die Verbindung zwischen Launcher und Auswertung. Die Kachel verlinkt auf den Dienst und zeigt zusätzlich einen Live-Wert und die Zahl offener Hinweise:

```
┌ Kimai ────────── ● online ┐  ┌ Invoice Ninja ─── ● online ┐  ┌ Snipe-IT ──────── ● online ┐
│ 3,5 h heute · Timer läuft │  │ 2 überfällig · 4.180 € off. │  │ 1 Garantie läuft ab        │
│                    ⚠ 1    │  │                      ⚠ 2    │  │                      ⓘ 1   │
└───────────────────────────┘  └─────────────────────────────┘  └────────────────────────────┘
```

### 7.3 Import-Assistent für `conf.yml`

Im Editor (oder per `andon import-dashy conf.yml --space <bereich>`) wird eine Dashy-Konfiguration in einen gewählten Bereich übernommen. Vor dem Speichern zeigt der Assistent eine Vorschau und einen Bericht.

| Dashy | Dashboard |
|---|---|
| `pageInfo` | Kopf-/Fußeinstellungen des Bereichs |
| `appConfig.statusCheck`, `statusCheckInterval` | Standardwerte für `link.status` |
| `appConfig.webSearch` | Suchmaschine des Bereichs |
| `appConfig.theme`, `customColors` | bekannte Themes und eigene Farben werden als Theme des Bereichs angelegt und aktiviert; unbekannte bleiben Kante |
| `customCss`, `layout` | ignoriert (im Bericht vermerkt) |
| `appConfig.auth` (Benutzer, `hideForUsers`, `hideForGuests`) | nicht automatisch; der Bericht listet die Einschränkungen, damit sie als Rechte nachgezogen werden können |
| `sections[].items[]` | Widgets `link` in der Bibliothek + Platzierungen (inkl. Icon, Status, Hotkey, Target) |
| `sections[].widgets[]` (`rss-feed`, `clock`, `weather`, `iframe`, `gl-*`, `public-ip`) | entsprechende Widget-Typen |
| `sections[].displayData` | `collapsed`, `cols`, `size`, `sort` |
| `pages[]` (Unterseiten, eigene YAML-Dateien) | weitere Boards |
| alles andere | Bericht „nicht übernommen“ |

Getestet wird der Import mit einer anonymisierten Kopie der eigenen `conf.yml` als Fixture.

### 7.4 Umstieg

1. Dashboard läuft parallel zu Dashy (anderer Port oder Subdomain).
2. Admin-Konto einrichten, `conf.yml` in den eigenen oder einen Team-Bereich importieren, Bericht durchgehen, fehlende Icons oder Widgets nachziehen.
3. Einige Tage beide nutzen. Kriterium für den Wechsel: Alle täglich genutzten Links, Statusanzeigen und Feeds sind da, die Suche ist mindestens so schnell.
4. Weitere Benutzer einladen, Browser-Startseite umstellen, Dashy-Container stoppen, `conf.yml` archivieren.

### 7.5 Technische Leitplanken

- **Keine Aufrufe aus dem Browser** zu Diensten oder Feeds. Kein CORS-Proxy, keine Tokens im Frontend.
- **Fremde Inhalte bereinigen:** RSS-HTML mit `nh3` (erlaubte Tags: Absätze, Links, Hervorhebungen), Links mit `rel="noopener noreferrer"`. SVG-Icons ohne Skripte und externe Verweise, ausgeliefert als `<img>`.
- **Content-Security-Policy:** Skripte nur aus dem eigenen Container, `frame-src` nur für freigegebene iframe-Ziele.
- **Wenig JavaScript:** Suche, Hotkeys, Uhr und Einklappen in reinem JavaScript (wenige KB); der Editor als eigenes Modul mit SortableJS. Keine Build-Kette, alles andere per HTMX.
- **Zeitlimits:** Statusprüfungen und Feeds mit kurzen Timeouts und Backoff, damit langsame Ziele nichts blockieren.

---

## 8. Die Dienste: Daten und Hinweise

Alle Abrufe laufen read-only mit eigenen API-Tokens über die Verbindungen eines Bereichs (Abschnitt 4.4). Unterstützt wird jeweils die aktuelle Version eines Dienstes. Jede Quelle meldet die gefundene Version im Verbindungstest; ist sie älter als die getestete Mindestversion, erscheint ein Hinweis `system.version_outdated`. Die Fixtures der Tests werden bei jedem größeren Update der Dienste neu aufgenommen. Endpunkte beim Bau gegen die jeweilige Version prüfen (Kimai: `/api/doc`, Dawarich: `/api-docs`).

### 8.1 Kimai (Zeiterfassung)

**Daten:** `GET /api/timesheets` (Filter `begin`, `end`, `exported`), `/api/timesheets/active`, `/api/projects`, `/api/customers`, `/api/activities`. Ziel ist die **jeweils aktuelle Kimai-2-Version**; Authentifizierung nur per Bearer-API-Token (die alte Anmeldung über `X-AUTH-USER`/`X-AUTH-TOKEN` wird nicht unterstützt). Abwesenheiten und Feiertage kommen aus der API des **kimai-holiday-bundle** (`/api/holiday/absences?year=`, `/api/holiday/public-holidays?year=`). Offene Abrechnungsposten im Sinne des **kimai-abrechnung-bundle** (abrechenbar, nicht exportiert, beendet) liefert die Kimai-Kern-API direkt (`/api/timesheets?billable=1&exported=0&state=stopped`).

**Kennzahlen:** Stunden (Woche/Monat/Jahr), davon abrechenbar, Auslastung gegen Soll, Stunden je Kunde/Projekt, Budgetverbrauch je Projekt, nicht exportierte Stunden und deren Alter.

| Regel | Bedingung (Standard) | Stufe |
|---|---|---|
| `kimai.timer_running_long` | Laufender Timer > 10 h | warn |
| `kimai.missing_day` | Werktag ohne Buchung, kein Urlaub/Feiertag (holiday-bundle) | info |
| `kimai.unbilled_hours` | Nicht exportierte, abrechenbare Einträge älter als 30 Tage | warn, ab 60 Tagen critical |
| `kimai.budget_burn` | Projektbudget (Zeit oder Geld) ≥ 80 % / ≥ 100 % verbraucht | warn / critical |
| `kimai.budget_pace` | Budgetverbrauch schneller als Projektlaufzeit (Hochrechnung überschreitet Budget vor Enddatum) | warn |
| `kimai.utilization_low` | Abrechenbare Auslastung im Monat < Ziel (z. B. 70 %) | info |
| `kimai.overtime` | Wochenstunden > Grenze (z. B. 45 h) zwei Wochen in Folge | info |
| `kimai.monthly_close` | Monatsende: Einträge des Vormonats noch nicht exportiert | warn |

### 8.2 Invoice Ninja v5 (Rechnungen, Zahlungen, Ausgaben)

**Daten:** `GET /api/v1/invoices` (u. a. `client_status=unpaid|overdue`), `/payments`, `/clients`, `/quotes`, `/recurring_invoices`, `/expenses`. Header `X-API-TOKEN` und `X-Requested-With: XMLHttpRequest`. Ziel ist die **jeweils aktuelle Invoice-Ninja-v5-Version** (self-hosted).

**Kennzahlen:** Umsatz netto (Monat, YTD, Vorjahr; die Umsatzsteuer ist kein Umsatz), vereinnahmte Umsatzsteuer, Vorsteuer aus Ausgaben, voraussichtliche Zahllast, offene Posten und deren Alter, Zahlungsdauer je Kunde (Days Sales Outstanding), Umsatzanteil je Kunde, Ausgaben, grober Überschuss, effektiver Stundensatz (Umsatz / Kimai-Stunden).

| Regel | Bedingung (Standard) | Stufe |
|---|---|---|
| `in.invoice_overdue` | Rechnung überfällig; ab 14 Tagen Mahnvorschlag | warn → critical |
| `in.slow_payer` | Kunde zahlt im Mittel > 30 Tage | info |
| `in.draft_stale` | Rechnungsentwurf älter als 7 Tage | info |
| `in.quote_open` | Angebot versendet, nach 14 Tagen ohne Reaktion | info (Nachfassen) |
| `in.recurring_ending` | Wiederkehrende Rechnung endet in < 30 Tagen | info |
| `in.revenue_vs_goal` | Umsatz YTD unter anteiligem Jahresziel | info |
| `in.tax_reserve` | Empfohlene Rücklage: Umsatzsteuer-Zahllast des laufenden Zeitraums + x % des Netto-Überschusses für die Einkommensteuer, verglichen mit der erfassten Rücklage | info |
| `in.vat_liability` | Voraussichtliche USt-Zahllast des laufenden Voranmeldungszeitraums (Umsatzsteuer aus Zahlungseingängen bei Ist-Versteuerung bzw. aus Rechnungen bei Soll-Versteuerung, minus Vorsteuer aus Ausgaben) | info, vor der Frist warn |
| `in.missing_vat` | Rechnung an inländischen Kunden ohne Umsatzsteuer, oder an EU-Kunden ohne USt-IdNr. bei 0 % (Reverse Charge prüfen) | warn |
| `in.expense_no_input_vat` | Ausgabe ohne erfasste Vorsteuer, obwohl der Lieferant üblicherweise Umsatzsteuer ausweist | info |
| `in.client_concentration` | Ein Kunde > 50 % des Umsatzes (Klumpenrisiko), > 5/6 über 12 Monate (Hinweis auf Prüfung Rentenversicherungspflicht als arbeitnehmerähnlicher Selbständiger) | info / warn |

### 8.3 Snipe-IT (Assets, Lizenzen)

**Daten:** `GET /api/v1/hardware` (inkl. `warranty_expires`, `asset_eol_date`, Status, Zuweisung), `/hardware/audit/due`, `/hardware/audit/overdue`, `/licenses`, `/consumables`, `/maintenances`. Bearer-Token.

**Kennzahlen:** Anzahl Assets nach Status/Kategorie, Gerätealter, Summe Anschaffungswerte, auslaufende Garantien/Lizenzen der nächsten 90 Tage.

| Regel | Bedingung (Standard) | Stufe |
|---|---|---|
| `snipe.warranty_expiring` | Garantie endet in ≤ 60 / ≤ 14 Tagen | info / warn |
| `snipe.eol_reached` | EOL-Datum erreicht oder Gerät älter als x Jahre | info (Ersatz einplanen) |
| `snipe.license_expiring` | Lizenz/Abo läuft in ≤ 30 Tagen ab | warn |
| `snipe.license_seats` | Keine freien Lizenzplätze mehr | info |
| `snipe.audit_overdue` | Audit überfällig | warn |
| `snipe.consumable_low` | Verbrauchsmaterial unter Mindestbestand | info |
| `snipe.unassigned_deployable` | Einsatzbereites Gerät seit > 90 Tagen ungenutzt | info (verkaufen?) |
| `snipe.expense_missing` | Asset mit Kaufdatum im laufenden Jahr ohne passende Ausgabe in Invoice Ninja (Betrag ± Toleranz, Datum ± 14 Tage) | info |
| `snipe.gwg_hint` | Anschaffung > 800 € netto → Abschreibung statt GWG-Sofortabzug prüfen | info |

### 8.4 Dawarich (Standortverlauf)

**Daten:** `GET /api/v1/points` (`start_at`, `end_at`), `/api/v1/visits`, `/api/v1/areas`, `/api/v1/stats`. Authentifizierung per API-Key. **Sensible Daten:** Es werden nur Aggregate gespeichert (Aufenthalte in definierten Bereichen, Tages-km), keine Rohpunkte.

**Idee:** In Dawarich werden **Areas** für Kundenstandorte, Büro und Zuhause angelegt. Zugeordnet werden sie im Kimai-Plugin Anfahrten (*Fahrten → Orte*: Area übernehmen, Kunde bzw. Typ „Zuhause“ setzen); Andon liest das über `GET /api/mileage/places` der Kimai-Verbindung. Die Option `areas` der Dawarich-Verbindung (Area-Name → `customer_id` bzw. `home`) überschreibt einzelne Areas oder ersetzt das Plugin. Fahrten aus Tracks und Einordnung privat/beruflich: Phase 17.

**Kennzahlen:** Tage beim Kunden je Monat, Fahrtstrecke je Tag, Abwesenheitsdauer von zu Hause, besuchte Länder/Orte (Reisen).

| Regel | Bedingung (Standard) | Stufe |
|---|---|---|
| `geo.visit_without_time` | Aufenthalt in Kunden-Area ≥ 2 h, aber keine Kimai-Buchung für diesen Kunden an dem Tag | warn |
| `geo.time_without_visit` | Kimai-Buchung mit Aktivität „vor Ort“, aber kein Aufenthalt in der Kunden-Area | info (Plausibilität) |
| `geo.travel_costs` | Fahrten zu Kunden im Monat: km × Kilometersatz (Standard 0,30 €/km), nicht als Ausgabe/Rechnungsposten erfasst | info |
| `geo.per_diem` | Abwesenheit > 8 h / 24 h an Kundentagen → Verpflegungsmehraufwand (Sätze konfigurierbar) | info |
| `geo.no_data` | Seit > 24 h keine neuen Punkte (Tracking-App aus?) | warn |

### 8.5 Übergreifend: Fristen und Wochenrückblick

| Regel | Inhalt |
|---|---|
| `tax.vat_return` | Umsatzsteuer-Voranmeldung zum 10. (Monat/Quartal, Dauerfristverlängerung konfigurierbar), mit der voraussichtlichen Zahllast aus `in.vat_liability` |
| `tax.vat_annual` | Umsatzsteuer-Jahreserklärung |
| `tax.prepayment` | ESt-Vorauszahlungen 10.03., 10.06., 10.09., 10.12. mit Betrag aus Konfiguration |
| `tax.annual` | EÜR und Einkommensteuererklärung mit eigener Frist (freiberuflich: keine Gewerbesteuer) |
| `digest.weekly` | Montag: Stunden, Umsatz, offene Posten, neue Hinweise der Woche |
| `digest.monthly` | Monatsanfang: Vormonat abschließen (Export in Kimai → Rechnung in Invoice Ninja → Fahrtkosten aus Dawarich) |

Alle Beträge, Sätze und Fristen werden im Editor je Bereich gepflegt, typischerweise im persönlichen Bereich, und pro Jahr aktualisiert.

---

## 9. Design: Kante (shrippen Design System)

Das Dashboard ist eine **App** im Sinne des Design Systems und nutzt daher die App-Komponenten. Das Design System liefert zugleich das einzige mitgelieferte Theme (Abschnitt 6). Stand: **Kante 1.7**. Regel für die Oberfläche (`agent.md`, „GUI rule“; Text in `kante/AGENT-RULE.md` des Design-System-Repos): Sie wird aus Kante erzeugt, nicht davon inspiriert. Was Kante fehlt, kommt zuerst nach Kante und erst dann hierher; lokal gebaut wird es nicht.

**Einbindung**

- Kante liegt **unverändert als Kopie in diesem Repo**: `internal/web/static/vendor/kante/` mit `components.css`, `base.css`, `shrippen.js`, den Schriften und einer `VERSION`-Datei (Quelle, Branch und Commit). Das Dashboard funktioniert so auch ohne Internet und wandert nicht ungeprüft mit dem CDN mit. Aktualisiert wird bewusst per Skript: `tools/sync-design.sh <Kante-Checkout>` (oder `KANTE_DIR`) kopiert die Dateien und erzeugt `internal/services/themes/builtin/kante/tokens.css` aus `tokens/variables.css`.
- `components.css` wird in `base.html` **vor** `andon.css` geladen; `andon.css` enthält nur Andon-Layout (Board-Raster, Widget-Innenleben) mit Kante-Tokens (`--cut-m`, `--h-m`, `--dur`, Rollen wie `--focus`, `--link`, `--hl`, `--warn`, `--danger`). `base.css` wird nicht geladen: sein Reset und die Sprachregeln sind für Landing Pages.
- `shrippen.js` wird geladen und liefert die Live-Daten-Bewegung (`window.Kante`: `tick`, `fresh`, `stale`, `edit`, `settle`). Sein Sprachumschalter läuft nicht: Andon hat eine eigene i18n, `data-own-lang` auf `<html>` schaltet Kantes Sprachbehandlung ab.
- Schriften (Rajdhani 500/600/700, JetBrains Mono 400/500) werden **lokal** ausgeliefert (WOFF2-Ausschnitte), nicht von Google Fonts (Datenschutz, offline).
- Hell/dunkel über `<html data-theme="light">`; die Wahl wird im Benutzerprofil gespeichert, nicht nur im Browser.
- Sprache: Die Seiten werden auf dem Server in der gewählten Sprache gerendert (nicht beide Sprachen im HTML, kein `.lang`-Umschalter); die Wahl steht im Profil.

**Vorhandene Komponenten wiederverwenden**

| Dashboard-Element | Design-System-Komponente (im Einsatz) |
|---|---|
| Einführungs-Hinweis einer Seite | `.callout` |
| Budget, Auslastung, Umsatzziel | `.progress` mit `data-tier="green|yellow|red"` |
| Status eines Connectors (ok, Warnung, Fehler) | `.pill` mit `data-state` (`applied`, `locked`, `failed`, `reviewing`); die Vorlagenfunktion `pill` übersetzt `ok`/`warn`/`fail` der Dienste |
| Tabellen (offene Rechnungen, Assets) | `.table-wrap` + `.table` |
| Aktionen | `.btn` mit `.btn-accent`, `.btn-outline`, `.btn-danger`, `.btn-quiet`, `.btn-icon`, `.btn-sm`; auf der gelben Leiste `.btn-primary`, `.btn-ghost` |
| Formulare | `.input`, `.select`, `.check` (mit `.check-box`/`.check-dia`), `.switch` |
| Umschalter | `.seg` |
| Menüs (Kopfzeile, Board, Kontext) | `.menu`; Zähler in Menüs und Kopfzeile `.count` |
| Palette, Kürzel-Hilfe, Kachel-Auswahl | `.dialog` auf einem nativen `<dialog>` |
| Rückmeldung nach Aktion | `.toast` mit `.toast-life` |
| Ablaufkarte der Willkommensseite | `.flow` |
| Warten | `.loader` |
| Live-Daten (Kachel aktualisiert, Wert ändert sich, Daten alt, Bearbeiten, Ziehen) | `[data-live-tile]`, `[data-live]`, `.is-stale`, `.is-editing`, `.is-picked`, `.drop-gap` mit `window.Kante` |

Nicht im Einsatz, weil Andon eigene Strukturen hat: `.nav`/`.foot` (Andon: `.app-nav`), `.field` (Beschriftungen stehen ohne Wrapper), `.scrim`, `.range`, `.tabs`, `.tile`/`.feat` (Kacheln entstehen per htmx nach dem Laden und würden von den Einblend-Effekten versteckt bleiben).

**Andon-Bausteine (in diesem Repo, `internal/web/static/andon.css`; fehlen in Kante)**

Keine mehr: `.uptime` kam mit Kante 1.12, `.launch-items` mit Kante 1.16 (06.10.2026). `andon.css` behält nur Board-Layout und Widget-Innenleben (z. B. den Uptime-Streifen auf kleinen Kacheln ausblenden).

Alles andere (Link-Kachel `.launch`, Suche, Uhr, Wetter, Ablagefläche, Diagramm-Legenden) kommt aus Kante.

Die Komponenten nutzen ausschließlich Theme-Tokens (`var(--…)`), keine eigenen Hex-Werte (per Stylelint geprüft). Nur so funktionieren sie mit jedem Theme. Ob sie später ins Design System wandern, ist eine eigene Entscheidung außerhalb dieses Projekts.

**Layout-Skizze: Start (Dashy-Ersatz)**

```
┌─────────────────────────────────────────────────────────────────────┐
│ ◆ dashboard  Start  Übersicht  Freelance  IT  Reisen  ✎  ☼  ◉ alex  │
├─────────────────────────────────────────────────────────────────────┤
│ [ / Suchen oder Websuche …                    ]   09:14 · 17° ☁     │
├─────────────────────────────────────────────────────────────────────┤
│ ▾ Freelance (3)                          │ ▾ News                    │
│ [Kimai ●] [Invoice Ninja ●] [Dawarich ●] │ · Artikel A      heise 1h │
│ ▾ IT (6)                                 │ · Artikel B      lwn   3h │
│ [Snipe-IT ●] [Proxmox ●] [Gitea ●] …     │ · Artikel C      heise 5h │
│ ▸ Medien (4)                             │ ▾ System (Glances)        │
│                                          │ CPU 12 % · RAM 61 %       │
├──────────────────────────────────────────┴──────────────────────────┤
│ Hinweise: ⚠ 3 · ⓘ 5   →  Übersicht                                   │
└─────────────────────────────────────────────────────────────────────┘
```

**Layout-Skizze: Übersicht**

```
┌─────────────────────────────────────────────────────────────────────┐
│ ◆ dashboard  Start  Übersicht  Freelance  IT  Reisen  ✎  ☼  ◉ alex  │
├─────────────────────────────────────────────────────────────────────┤
│ [Umsatz YTD]  [Offene Posten]  [Stunden Monat]  [Auslastung]  [Ø €/h]│
├───────────────────────────────────────┬─────────────────────────────┤
│ Hinweise (3 kritisch · 5 Warnung)     │ Nächste Fristen             │
│ ▌ Rechnung R-2026-041 21 Tage überf.  │ 10.10. USt-VA September      │
│ ▌ 38 h Muster GmbH nicht abgerechnet  │ 14.10. Garantie ThinkPad     │
│ ▌ Laufender Timer seit 11 h           │ 31.10. Lizenz JetBrains      │
├───────────────────────────────────────┼─────────────────────────────┤
│ Umsatz je Monat (Balken, Vorjahr)     │ Projektbudgets (.progress)   │
├───────────────────────────────────────┴─────────────────────────────┤
│ Connector-Status: kimai ● ok · invoiceninja ● ok · snipeit ● ok …   │
└─────────────────────────────────────────────────────────────────────┘
```

**Offen: UI auf Kante-Tokens umstellen**

Die Oberfläche läuft auf den Tokens und Komponenten von **Kante 1.9** (siehe „Einbindung“ oben). Schaltflächen, Felder, Pills, Menüs, Dialoge, Toasts, Tabellen, Fortschritt und die Live-Daten-Bewegung kommen aus dem vendorten Kante; `andon.css` behält nur Board-Layout und Widget-Innenleben. Aus Kante seit 1.5 bis 1.7: Kennzahl (`.kpi`, `.delta`, `.kpi-row`), Chips und Chip-Auswahl, Feed, Hinweis-Karte, Fristen (`.timeline`, `.date-tile`), Bearbeitungsleiste (`.editbar`), Anmeldung (`.login`); seit 1.8/1.9: Kachel-Werkzeugleiste (`.tile-tools`), gespeichertes Einklappen (`details.fold` + `kante:fold`), Sammelleiste (`.bulk-bar`), Freigaben (`.share`), Kontrast und Farbfeld (`.contrast`, `.swatch`, `input[type=color]`), Diagramme (`.chart`, `.spark`, `.heat`, `.legend`, Datenpalette `--d1…--d6`). Seit Kante 1.16 fehlen keine Andon-Bausteine mehr (Tabelle „Andon-Bausteine“). Entwürfe für vier Bildschirme (Start, Übersicht, Editor, Anmeldung): <https://claude.ai/artifact/K1SEJhy4Pm4wyn4zDJ9vLH>.

- [x] `andon.css` in `internal/web/static/` angelegt: nur Tokens (`var(--…)`), keine Hex-Werte außerhalb `themes/`
- [x] `base.html`: `system-ui`-Fallback durch `.app-nav`/`.app-links`/`.app-side` und echte Formularstile ersetzt; jede Seite lädt jetzt ihr aktives Theme (`Deps.Page` setzt `ThemeURL`, vorher nur die Board-Seite)
- [x] `.launch`: Link-Kachel mit Icon-Quadrat (Monogramm als Rückfall), Status-Punkt, Infozeile und Hinweis-Zähler
- [x] `.kpi` / `.kpi-row`: Kennzahl-Kacheln (`widgets_insight.html`, bereits vor diesem Abschnitt vorhanden, jetzt mit den echten Tokens statt Fallback-Werten)
- [x] `.hint`: Hinweis-Karte mit Stufe, Quelle, aufklappbarem „Warum?“, Aktion — bestehende Struktur, jetzt mit Tokens gestylt
- [x] `.pill` mit `data-state`: Connector-/Link-Status
- [x] `.progress` mit `data-tier`: Budget- und Auslastungsbalken
- [x] `.editbar`: Bearbeitungsmodus direkt auf dem Board (`?edit`), Kacheln per Drag & Drop (SortableJS); eigenes Layout über `?layout`
- [x] `.login`: eigene Anmeldeseite (`login.html`, `totp`, `setup`)
- [x] Kontrastprüfung dunkel/hell (WCAG AA) für die neuen Komponenten *(Test `TestComponentContrastAA`; Statustexte über abgeleitete Tokens `--ok-text`/`--warn-text`/`--danger-text`, auch für eigene Themes)*

---

### 9.1 Startseite im D-Stil

Aufbau (Entwurf D, umgesetzt 2026-09-26): Kopfleiste mit Boards, Hinweis-Zähler, Suche und den Menüs „Einrichten“ und Nutzer. Darunter frei platzierbare Bereiche: Begrüßung, „Häufig“, Datenkacheln, Linkgruppen in fließenden Spalten (Spanne 7), rechts die Lage (Hinweise nach Schwere). Jeder Bereich und jede Kachel lässt sich im Bearbeiten-Modus verschieben und über „Mein Layout“ je Nutzer ausblenden.

Links: klein = kompakte Zeilen, mittel = Zeile mit Beschreibung, groß = Icon-Kachel. Ein Link ohne Info-Verbindung zählt die Hinweise der Verbindung auf demselben Host; Klick auf den Zähler zeigt sie.

### 9.2 Katalog der Datenkacheln

Status: **da** = gab es schon, **neu** = mit dem D-Stil gebaut (2026-09-26).

| Bereich | Kachel | Darstellung | Quelle | Status |
|---|---|---|---|---|
| Start | Begrüßung (Gruß, Uhr, Wetter, seit gestern) | Text, Vorhersage-Balken, Liste | Open-Meteo, Zeitleiste, Hinweise | neu |
| Start | Lage | Hinweise nach Schwere, Aktion, Später | Hinweise | neu (Umbau) |
| Start | Uhr, Wetter, Feiertage, Kalender, RSS, Notiz, Liste, Bild, Eingebettete Seite | Text | diverse | da |
| Start | Öffentliche IP | Wert | ipify | da |
| Zeit | Kimai Lite | Timer, Tagesbalken, Heute/Woche, Favoriten, Zuletzt; Einträge bearbeiten, teilen, löschen; Tags, abrechenbar | Kimai (live) | neu (Umbau) |
| Zeit | Stunden-Heatmap | Jahresraster | Kimai | da |
| Zeit | Woche je Tag gegen Ziel | Balken mit Ziellinie | Kimai | neu |
| Zeit | Projekte je Tag / Tätigkeitsverteilung (wie Plasmai-Statistik) | gestapelte Balken, Ring | Kimai | neu |
| Geld | Offene Rechnungen nach Alter | gestapelte Leiste | Invoice Ninja | neu |
| Geld | Kennzahl (Umsatz, offen, überfällig, Stundensatz …) | Wert, Delta | Kimai, Invoice Ninja, Sure | da |
| Geld | Liquiditätsverlauf | Linie mit Ereignissen | Invoice Ninja, Sure | da |
| Geld | Umsatz je Monat | Balken | Invoice Ninja | da (Diagramm) |
| Geld | Nicht abgerechnete Stunden nach Alter | gestapelte Leiste | Kimai | neu |
| Geld | Budget-Fortschritt | Fortschrittsbalken | Kimai | da |
| Homelab | Verbindungen | gesund/gesamt, 14-Tage-Streifen | Abrufstatistik | neu |
| Homelab | Last (CPU, RAM, Load) | Balken | Glances | da (jetzt Balken) |
| Homelab | Systemwerte (CPU, RAM, Swap, Platten) | Fortschrittsbalken | Glances | da (jetzt Balken) |
| Homelab | Backups | Statusstreifen, Liste | Borg, PG Back Web | da (jetzt Streifen) |
| Homelab | Speicherprognose | Füllstand, „voll in“ | Kennzahl-Verlauf der Speicherquellen | da |
| Homelab | Update-Zentrale, Update-Fenster | Liste | diverse | da |
| Homelab | Monitore | Status-Pills | Uptime Kuma | da |
| Homelab | Link-Erreichbarkeit 30 Tage | Streifen | Linkstatus | da (in Link-Kacheln) |
| Homelab | Plattengesundheit | Ampel je Platte | Scrutiny | neu |
| Homelab | Container/Stacks | Zähler, Liste | Komodo | neu |
| Homelab | Pools | Füllstand je Pool | TrueNAS | neu |
| Homelab | Werbeblocker heute | Anteil geblockt, Balken je Stunde | Pi-hole, AdGuard | neu |
| Homelab | VPN-Tunnel | Status, Ausgangsland | Gluetun | neu |
| Homelab | Tailnet | Geräte, ablaufende Schlüssel | Tailscale | da |
| Homelab | Gateway | WAN, Geräte, Updates | OPNsense, pfSense, UniFi | neu |
| Homelab | Zertifikate und Domains | Tage bis Ablauf als Balken | Zertifikate, RDAP | neu |
| Homelab | Homelab-Kosten | Summe, Aufteilung | Einstellungen, Tibber | da |
| Netz | Internet-Geschwindigkeit | Balken gegen Vertrag | Speedtest Tracker | da (jetzt Balken) |
| Netz | Geschwindigkeit 7 Tage | Balken je Tag | Speedtest Tracker | neu |
| Haushalt | Home Assistant | Werte, Schalter | Home Assistant | da |
| Haushalt | Energie | Preiskurve, Kosten | Tibber, Home Assistant | da |
| Haushalt | Vorräte, Einkaufsliste | Liste | Grocy | da |
| Haushalt | Wetterwarnungen | Liste | DWD | da |
| Medien | Mediaserver (Streams, Bibliothek) | Zähler | Jellyfin, Plex | da |
| Medien | Demnächst | Liste | Sonarr, Radarr | da |
| Medien | Downloads | Fortschritt, Speicher frei | SABnzbd | neu |
| Dokumente | Paperless-Posteingang | Zähler, ältestes | Paperless | neu |
| Dokumente | Rechnungen aus Mail | Liste | IMAP | neu |
| Wissen | Ungelesen je Feed | Balken | FreshRSS | neu |
| Wissen | Linkwarden-Abgleich | Zähler | Linkwarden | neu |
| Code | GitHub-Repos | PRs, Issues, CI | GitHub | da |
| Code | Gitea | Offene Reviews | Gitea | neu |
| Standort | Pendeln heute, Fahrtenbuch | Strecke, km | Dawarich | neu |
| Assets | Garantien und Prüfungen | Fristen | Snipe-IT | da (Fristen) |
| Sicherheit | Anmeldungen | Liste, Länder | authentik | neu |
| Sicherheit | Tresore ohne 2FA | Zähler | Vaultwarden | neu |
| Übergreifend | Woche in Zahlen | Zeilen | alle | da |
| Übergreifend | Hinweise als Widget mit Filter | Liste | Hinweise | da |
| Übergreifend | Eigene Integration / Eigene API | Kennzahl, Tabelle | JSON-API | da |

## 10. Phasen

Jede Phase endet mit einem lauffähigen, getaggten Image. Anmeldung und Bereichsmodell kommen bewusst ganz an den Anfang: Mehrbenutzerfähigkeit nachträglich einzubauen hieße, jede Abfrage und jedes Widget noch einmal anzufassen.

### Phase 0: Fundament (v0.1)

- [x] Repo-Struktur, `pyproject.toml`, Ruff, Stylelint, Pytest, pre-commit *(Stylecheck-Skript statt Stylelint, kein pre-commit)*
- [x] FastAPI-Grundgerüst, Jinja-Layout mit Kante (`components.css`), lokale Schriften *(seither Go: `net/http`, `html/template`; Schriften unter `static/vendor/kante/fonts`, CSP wie in Python)*
- [x] Übersetzung von Anfang an: alle Texte über gettext (DE/EN), Formatierung mit Babel, Sprache aus Profil bzw. `Accept-Language` beim ersten Besuch; CI prüft, dass keine Übersetzung fehlt *(YAML-Kataloge mit Schlüsseln statt gettext)*
- [x] Datenbank mit SQLAlchemy + Alembic (SQLite im WAL-Modus)
- [x] Datenmodell: Benutzer, Teams, Bereiche, Verbindungen, Widgets, Boards, Platzierungen, Freigaben, Revisionen
- [x] Anmeldung: Einrichtungscode, lokale Konten (Argon2id), Sitzungen, CSRF, Drosselung, Abmelden
- [x] Zentrale Berechtigungsprüfung in der Service-Schicht, Tests als Rechte-Matrix (Rolle × Recht × Ressource)
- [x] Verschlüsselung der Zugangsdaten (AES-GCM, Hauptschlüssel aus Docker Secret)
- [x] Quellen-Schnittstelle (`fetch()`, `healthcheck()`, Cache je Verbindung + Zugangsdaten), Widget-Schnittstelle (Schema + Vorlage + HTMX-Fragment), Scheduler
- [x] Hinweis-Engine: Fingerprint, Zustände je Benutzer/Team, Snooze/Ack
- [x] Dockerfile (multi-stage, non-root, `HEALTHCHECK`), `docker-compose.example.yml`
- [x] GitHub Actions: Tests, Image-Build `linux/amd64` + `linux/arm64`, Push nach GHCR
- [x] Demo-Modus mit Fixture-Daten und Demo-Benutzern (Entwicklung, Screenshots) *(Go: `ANDON_DEMO=true`, `services/seed`, `sources/demo.go`)*

### Phase 1: Startseite, Editor und Dashy-Migration (v0.2)

- [x] Widget `link` mit Icons (`favicon`, `si-*`, `hl-*`, URL, Upload, Monogramm) und Icon-Cache *(Go: `services/icons`, `sources/icons.go`, `/icons/{key}`)*
- [x] Quelle `http_status` und Statuspunkt auf den Kacheln
- [x] Boards, einklappbare Abschnitte, `cols`, Kachelgrößen, Sortierung
- [x] Suche mit Filter, `Enter`, Hotkeys, Websuche als Rückfall
- [x] Widgets `rss`, `clock`, `weather` (Open-Meteo)
- [x] Widgets `iframe`, `sysinfo` (Glances), `public_ip`; kompakte Ansicht; PWA-Manifest
- [x] Konfigurationseditor v1: Board-Editor mit Drag & Drop, Widget-Formulare aus Schema mit Vorschau, Widget-Bibliothek, Verbindungen mit „testen“, Revisionen *(Go: Felder je Typ in `widgets/fields.go`, Vorschau per htmx)*
- [x] Import/Export YAML, Dashy-Import-Assistent mit Bericht, getestet an der eigenen `conf.yml` *(Go: `services/porting`, `/import`, `/spaces/{id}/code`, CLI `andon import`)*
- [x] Persönliche Einstellungen: Start-Board, hell/dunkel, Sprache, Suchmaschine
- [x] Neue Komponenten `.launch`, `.launch-grid`, `.section-fold`, `.search`, `.feed`, `.clock`, `.weather`, Editor-Komponenten
- [x] Parallelbetrieb, dann Umstieg nach Checkliste (Abschnitt 7.4) *(abgeschlossen 06.10.2026)*

**Ergebnis:** Dashy ist abgeschaltet, das Dashboard ist die Browser-Startseite, alles wird in der Oberfläche gepflegt.

### Phase 2: Mehrbenutzer und Teams (v0.3)

- [x] E-Mail-Versand (SMTP) mit Vorlagen im Design System; Einladungen, Selbstregistrierung (abschaltbar), Passwort-Reset per E-Mail, Sicherheitsmeldungen
- [x] Single Sign-on mit authentik (Abschnitt 4.7): Kontoverknüpfung, automatisches Anlegen mit Startwerten aus authentik-Gruppen (danach manuell pflegbar), Modus „nur authentik“ mit Notzugang *(Go: `services/oidc`, `sources/oidc.go`, PKCE + ID-Token-Prüfung mit go-jose; Einstellungen unter `/admin/settings`)*
- [x] TOTP mit Wiederherstellungscodes, für Admins erzwingbar; Sitzungsliste *(Go: `/me/security`)*
- [x] Teams mit Rollen Owner/Editor/Viewer, Team-Bereiche *(Go: `/teams`)*
- [x] Freigaben `view`/`use`/`edit`/`manage` an Widgets, Boards und Verbindungen; Dialog „Wer hat Zugriff?“ *(Go: `/shares/{kind}/{id}`, bisher nur von der Verbindungsliste verlinkt)*
- [x] Team-Widgets auf persönlichen Boards, persönliche Overlays an Team-Boards, Vorlagen *(Vorlagen über YAML-Export/-Import, keine Vorlagengalerie)*
- [x] Verbindungen mit persönlichen Zugangsdaten
- [x] Persönliche API- und Embed-Tokens *(Go: `/me/security`; `/api/summary`, `/api/hints`, `/embed/hints`, `/embed/b/{id}`)*
- [x] Audit-Log; Admin-Ansicht für Benutzer und Teams (ohne Einblick in persönliche Bereiche) *(Go: `/admin/users`, `/admin/audit`, `/admin/settings`)*

### Phase 3: Themes (v0.4)

- [x] Theme-Vertrag (Token-Liste, Version, Standardwerte) und Laden der Themes je Bereich
- [x] Kante (früher „shrippen“) als einziges mitgeliefertes, schreibgeschütztes Theme (dunkel + Leinen)
- [x] Theme-Editor mit Live-Vorschau, dunkel/hell nebeneinander, Kontrastprüfung WCAG AA *(Go: `/themes/{id}`)*
- [x] Import/Export als ZIP; eigenes CSS und Schriften nur für Instanz-Admins *(Go: Schrift-Upload je Theme, Dateiname = Familie-Gewicht; Schriften reisen im ZIP mit)*
- [x] Auswahlreihenfolge persönlich → Team → Instanz, optional erzwungenes Theme je Board
- [x] Seite `/styleguide` mit allen Komponenten im aktuellen Theme

### Phase 4: Freelance-Kern: Kimai + Invoice Ninja (v0.5, MVP der Auswertung)

- [x] Kimai-Quelle und Kennzahlen, Regeln aus 8.1
- [x] Invoice-Ninja-Quelle und Kennzahlen, Regeln aus 8.2
- [x] Abgleich Kimai ↔ Invoice Ninja: nicht abgerechnete Stunden je Kunde, effektiver Stundensatz (KPI `effective_rate`, Tabelle `effective_rates`)
- [x] Boards „Übersicht“ und „Freelance“ als Vorlagen mit `.kpi`, `.hint`, `.progress`, Tabelle offener Posten
- [x] Regel-Einstellungen im Editor; Deep-Links von jedem Hinweis in die Fach-UI
- [x] Infozeilen und Hinweis-Zähler auf den Link-Kacheln von Kimai und Invoice Ninja

**Ergebnis:** Das Dashboard ersetzt den täglichen Blick in beide Tools.

### Phase 5: IT-Landschaft: Snipe-IT (v0.6)

- [x] Snipe-IT-Quelle, Regeln aus 8.3
- [x] Board-Vorlage „IT“: Assets nach Status, Garantie-/Lizenz-Zeitleiste, Audits
- [x] Abgleich Snipe-IT ↔ Invoice-Ninja-Ausgaben (`snipe.expense_missing`)

### Phase 6: Standort: Dawarich (v0.7)

- [x] Dawarich-Quelle, nur Aggregate speichern, standardmäßig nur persönliche Verbindung
- [x] Zuordnung Area → Kimai-Kunde im Editor
- [x] Regeln aus 8.4: Besuch ohne Buchung, Fahrtkosten, Verpflegungspauschalen
- [x] Board-Vorlage „Reisen“: Kundentage, km je Monat, Vorschlag für Fahrtkosten-Position

### Phase 7: Erinnerungen und Benachrichtigungen (v0.8)

- [x] Fristen-Kalender (Abschnitt 8.5), Board „Fristen“, iCal-Feed je Benutzer (mit Token) *(Go: `/calendar.ics?token=…`, `services/calendar`)*
- [x] Benachrichtigungen über Apprise: jeder Benutzer hinterlegt eigene Apprise-URLs (verschlüsselt gespeichert, mit Testknopf) und wählt Mindeststufe und Ruhezeiten; E-Mail-Benachrichtigungen nutzen den vorhandenen SMTP-Server *(Go ruft eine externe Apprise-API statt sie einzubinden)*
- [x] Texte der Benachrichtigungen in der Sprache des Empfängers
- [x] Digest als HTML-E-Mail im Design System (mit Textversion), in der Sprache des Empfängers
- [x] Morgen-Digest und Wochenrückblick; Ruhezeiten; keine Doppelmeldungen (Fingerprint)
- [x] Monatsabschluss-Checkliste (Kimai-Export → Rechnung → Fahrtkosten)

### Phase 8: Trends und Prognosen (v0.9)

- [x] Verlaufsdiagramme aus Snapshots (Umsatz, Stunden, offene Posten)
- [x] Hochrechnung Jahresumsatz, Umsatzsteuer-Zahllast und Steuerrücklage
- [x] Vergleich Vorjahr, saisonale Muster, Liquiditätsvorschau (offene Posten + wiederkehrende Rechnungen − feste Ausgaben) (Diagramm `seasonal`, KPI `liquidity_30`, Fixkosten in den Bereichseinstellungen)

### Phase 9: Ausbau (v1.0)

- [x] Passkeys (WebAuthn) für lokale Konten (Sicherheit → Passkeys, Anmeldung ohne Passwort; ersetzt TOTP)
- [x] CodeMirror in der Code-Ansicht (CodeMirror 5 vendored, YAML/CSS, Theme-Tokens)
- [x] Weitere Dashy-Widgets nach Bedarf (Liste aus dem Import-Bericht) *(Bild, Wechselkurse, Hacker News als RSS, Uptime-Kuma-Monitore; uptime-kuma/proxmox-lists mit Hinweis auf Verbindung)*
- [x] Weitere Quellen über dieselbe Schnittstelle: z. B. Uptime Kuma (Dienste down), Proxmox/Docker (Updates, Speicher), Backup-Status, Paperless-ngx (unbearbeitete Belege), Zertifikatsablauf *(Uptime Kuma, Proxmox VE mit Updates/Speicher/vzdump-Backups, Paperless-ngx, TLS-Zertifikate; Docker nicht umgesetzt)*
- [x] Optionale Wochenzusammenfassung in Fließtext per LLM (abschaltbar je Benutzer, nur Aggregate, keine Standortdaten) *(Claude über `ANTHROPIC_API_KEY`/Secret `anthropic_api_key`; nur Hinweis-Anzahlen je Regel, `geo.*` ausgenommen)*

### Phase 10: Homelab-Dienste

Jede Quelle liefert einen gecachten Datensatz (`<dienst>.data`), Regeln, eine Infozeile auf der Link-Kachel und Demodaten. Der Quell-Cache hält Ergebnisse jetzt für die TTL der Quelle im Speicher (vorher jeder Aufruf live).

- [x] Prüflauf als Hintergrund-Job: holt alle Integrationen (beim Start und alle `ANALYSIS_MINUTES`, Standard 5) und leitet Hinweise ab; Seiten lesen nur diesen Stand, live ist nur der Status-Ping der Link-Kacheln. Fehlt ein Stand (neue Verbindung), wird er einmal im Hintergrund geholt. Admin → Instanz zeigt den letzten Lauf und startet ihn auf Wunsch sofort
- [x] Datenmodus je Widget: automatisch (Vorgabe des Typs) / live beim Anzeigen / aus dem Prüflauf. Live als Vorgabe bei Home Assistant, Uptime-Kuma-Monitoren und Systemwerten; Daten anderer Verbindungen im selben Widget (z. B. Kimai beim Stundensatz) bleiben beim Prüflauf
- [x] Leichte Live-Quelle für Kimai (laufender Timer, Stunden heute), damit „live“ dort nicht den ganzen Datensatz lädt *(`kimai.live`)*

- [x] FreshRSS (Google-Reader-API): Leserückstand mit den größten Quellen, verstummte Feeds
- [x] Gitea: wartende Reviews, fällige Issues, ruhende PRs, fehlgeschlagene Actions, veraltete Spiegel
- [x] E-Mail (IMAP, nur lesend): Eingangsrechnungen erkennen (Betreff/Anhangsname, Betrag aus dem Text) und mit Invoice-Ninja-Ausgaben abgleichen (Betrag ± 1 ct im Datumsfenster, sonst Lieferantenname ~ Absender)
- [x] Sure: Kontostand/Vermögen, ausgebliebene wiederkehrende Zahlungen, niedriger Kontostand, Bank-Sync, ungewöhnliche Ausgaben, ohne Kategorie; mit Invoice Ninja: Zahlungseingang zu offener Rechnung, Geschäftsausgabe nicht erfasst; Liquidität nutzt Sures Fixkosten
- [x] Paperless-ngx: zusätzlich Rechnungsdokumente gegen Invoice-Ninja-Ausgaben
- [x] Immich: Speicher, fehlgeschlagene Jobs, Updates
- [x] Linkwarden: Lesezeichen einer Sammlung ohne Kachel, Kacheln ohne Lesezeichen
- [x] Home Assistant: Alarmsensoren, Batterien, nicht erreichbare Entitäten (ein Hinweis), Updates; Widget mit Schaltern (nur gelistete Entitäten, nur mit Recht „nutzen“ an der Verbindung)
- [x] Uptime Kuma: zusätzlich Kacheln ohne Monitor
- [x] Umami: Besuchereinbruch gegenüber Vorwoche, keine Aufrufe mehr
- [x] Scrutiny: SMART-Fehler, Temperatur, schweigender Collector
- [x] Borg Backup Server: Clients offline/Fehler, fehlgeschlagene Jobs, Alter des letzten Backups, Speicher, Updates
- [x] PG Back Web (keine Lese-API): signierte Webhook-URL je Verbindung; fehlgeschlagene/veraltete Backups, nicht erreichbare Datenbanken/Ziele, ausbleibende Webhooks
- [ ] Obsidian – geplant in Phase 15 (Abgleich Doku ↔ Compose, Ansicht in Homelable)
- [x] Docker *(über einen Socket-Proxy, der nur Container zeigt; Regeln `docker.unhealthy`, `docker.crashed`, Kachel „Container“, Container ohne Kachel)*

### Phase 11: Dashy-Abgleich und weitere Integrationen

Auswahl vom 25.09.2026 (Checkliste „Dashy-Abgleich“). Abgelehnt: Overlay, Arbeitsbereich- und Minimal-Ansicht, Unterseiten per URL, Bangs, Suchmaschinen-Liste, URL direkt öffnen, Ausblenden-aber-findbar, Suchziel, Prüfintervall je Link, Öffnen-Ziele beim Import melden. Offen gelassen: Schreib-API, Cloud-Sicherung, weitere Sprachen, erzeugte Icons, Healthchecks, ntfy, Synology, Linkding, Drone CI, CVE-Feed, Sport.

- [x] Import/Startseite: Tags an Links (Suche), Seitentitel/Beschreibung/Navigationslinks/Fußzeile anzeigen, mehrere Links in einer Kachel (Sub-Items), Pfeiltasten durch die Treffer *(Seitentexte in den Bereichseinstellungen, `spaces/page.go`)*
- [x] Rechtsklick-Menü je Kachel (neuer Tab, selber Tab, Adresse kopieren; Umschalt + Rechtsklick öffnet das Browser-Menü)
- [x] Layout: Abschnitte über mehrere Zeilen, eigene Farbe je Abschnitt und Link (aus Theme-Farben) *(Abschnitte zusätzlich in Vierteln der Breite; Farbe nur als Linie, Text bleibt kontrastgeprüft)*
- [x] Status-Checks mit eigenen HTTP-Headern *(verschlüsselt in der Widget-Konfiguration, nie im Formular oder Export)*
- [x] Icons: Material Design Icons, selfh.st, Emoji, Font Awesome *(Emoji als Text, ohne Download; Shortcodes wie `:rocket:` nicht)*
- [x] Themes: Auswahl der Dashy-Themes nachbauen; Dashy-Farben beim Import als Theme übernehmen *(11 Vorlagen in `themes/presets.go`: Callisto, Nord, Dracula, One Dark, Material hell/dunkel, High Contrast hell/dunkel, Oblivion, Cyberpunk, Vaporware; Dashy-eigene Paletten angenähert; beide Modi mit derselben Palette)*
- [x] Widgets ohne eigenen Dienst: Kalender (iCal), Custom API, eigene Liste, Feiertage, xkcd, NASA-Bild des Tages, Witze, Krypto, Aktien, Flüge, Nahverkehr *(Quellen in `sources/fun.go`, `media.go`, `ical.go`, `travel.go`; API-Schlüssel, Header und private Kalender-Adressen verschlüsselt in der Widget-Konfiguration, nie im Formular oder Export; Dashy-Widgets werden beim Import zugeordnet)*
- [x] Widgets/Quellen fürs Homelab: Pi-hole/AdGuard, Nextcloud, Sabnzbd, Gluetun/Mullvad, Glances im Detail, Domain-Ablauf, Blacklist-Check *(`sources/netops.go`, `rules/netops.go`; Gluetun erkennt Lecks am Vergleich der Ausgangs-IP mit der eigenen und prüft das erwartete Land – eine Mullvad-eigene Abfrage entfällt, weil das Dashboard selbst nicht im Tunnel läuft; Widget `glances_chart` für den Verlauf; Domain-Ablauf über RDAP, .de ohne Datum)*
- [x] Integrationen: TrueNAS, Komodo, Pangolin, authentik (Nutzungsstatistik) *(Go: `sources/infra.go`, `rules/infra.go`; TrueNAS über JSON-RPC per WebSocket mit REST-Fallback; Tabelle `app_usage` für Anmeldungen je Anwendung)*

### Phase 12: Ausbau

Auswahl vom 25.09.2026 (Checkliste „Dashboard-Ausbau“). Nicht gewählt: Hell/dunkel nach Uhrzeit, öffentliche Statusseite, Proxmox Backup Server, Frigate, Fragen an die eigenen Daten, Prometheus-Metriken.

**Hinweise und Analyse**
- [x] Update-Zentrale: alle verfügbaren Updates in einem Widget *(Widget `updates`: Hinweise der Update-Regeln, `rules/topics.go`)*
- [x] Backup-Übersicht: Dienst × letzte Sicherung (Borg, PG Back Web, TrueNAS-Snapshots), Dienste ohne Backup melden *(Widget `backups`; Regel `backups.gap` vergleicht Namen von Komodo-Stacks und TrueNAS-Apps mit den Backup-Einträgen)*
- [x] Ausfälle bündeln: ein Hinweis mit Ursache statt vieler Folgehinweise *(≥ 2 Fehler auf einem Host – Verbindungen und Kuma-Monitore – werden zu `system.outage`; der Analyse-Lauf ruft erst alles ab und wertet dann aus)*
- [x] Dienste ohne Kachel finden (Komodo, Pangolin, Kuma)
- [x] Zertifikate automatisch aus Link-Kacheln und Pangolin-Ressourcen prüfen *(Option `auto: true` der Zertifikats-Verbindung)*
- [x] Verlauf je Hinweis, flatternde Hinweise dämpfen *(3× wieder aufgetreten in 7 Tagen = flattert, keine Wiederholungs-Pushes)*
- [x] Eigene Regeln ohne Code (Schwellwert auf Kennzahl oder API-Feld) *(Bereichseinstellungen; neue Verbindung „Eigene API (JSON)“ für beliebige JSON-Endpunkte)*
- [x] Notiz beim Quittieren
- [x] Hinweis zuweisen (offen/in Arbeit/erledigt)

**Selbstständigkeit**
- [x] Rechnungsentwurf aus unabgerechneten Kimai-Stunden in Invoice Ninja (Vorschau, Bestätigung) *(Seite „Abrechnung“; Kunde ↔ Invoice-Ninja-Kunde über den Namen; Zeiten wahlweise als exportiert markiert)*
- [x] Kimai-Timer starten/stoppen (mit der leichten Kimai-Quelle aus Phase 10) *(Widget `kimai_timer`; Schreibzugriffe nur über `outbound`, mit Nutzungsrecht auf die Verbindung)*
- [x] Liquiditätsverlauf 90 Tage *(Widget `cashflow`: Sure-Kontostand, offene Rechnungen nach Zahlungsgewohnheit des Kunden, feste Kosten, USt und Vorauszahlungen)*
- [x] Zahlungsmoral je Kunde *(Tabelle `payment_morale`, Regel `in.payment_worse`)*
- [x] Verträge und Kündigungsfristen aus Paperless *(Tag „Vertrag“; Felder „Vertragsende“/„Kündigungsfrist“ oder Text; automatische Verlängerung wird fortgeschrieben; Regel `paperless.contract_notice` mit Fälligkeit)*
- [x] Rechnungsmail an Paperless übergeben *(Seite „Abrechnung“; Anhänge werden erst dann per IMAP geladen)*
- [x] Jahrespaket für die Steuer (CSV/ZIP) *(laufendes und Vorjahr, so weit die Datensätze reichen)*
- [x] Stunden-Heatmap

**Startseite und Bedienung**
- [x] Befehlspalette (Strg+K) *(Boards, Link-Kacheln aller sichtbaren Boards, Seiten; Aktionen wie Timer/Schalter bleiben auf den Kacheln)*
- [x] Link per URL hinzufügen (Titel und Icon automatisch)
- [x] Häufig genutzte Links *(ab 3 Klicks, je Benutzer)*
- [x] Tote Links melden *(Hintergrundprüfung alle 10 Minuten, Regel `links.dead` ab 7 Tagen ohne Antwort)*
- [x] Rückgängig im Editor *(stellt die vorige Board-Version wieder her)*
- [x] Mehrere Kacheln gleichzeitig bearbeiten *(Auswahl im Editor: verschieben, Farbe, entfernen)*
- [x] Board duplizieren und Vorlagen *(Vorlagen Homelab, Selbstständig, Familie; eingebettetes YAML)*
- [x] Tastenkürzel-Übersicht („?“)
- [x] Verfügbarkeit auf der Kachel (30 Tage, Antwortzeit)

**Anzeigen und Geräte**
- [x] Wandanzeige (Vollbild, Boards wechseln, nachts gedimmt) *(`?kiosk&every=60&dim=22-7`)*
- [x] Offline-Ansicht (letzter Stand) *(Service Worker, Netz zuerst; Abmelden löscht die Kopien)*
- [x] Eigenes Handy-Layout je Board *(je Abschnitt: oben anzeigen oder ausblenden)*

**Verbindungen**
- [x] Verbindungs-Assistent *(Dienst wählen → Adresse/Token mit Hilfe → Test und passende Widgets)*
- [x] Zustand je Verbindung (letzter Erfolg, Fehlerquote, Antwortzeit) *(7 Tage, Tabelle `conn_stats`)*
- [x] Token-Hygiene (Alter, Ablauf) *(Regel `system.token_age`)*
- [x] Eigene Integration per YAML *(JSON-API-Verbindung: fields mit Schwellen, list; Widget „Eigene Integration“, Regel `jsonapi.threshold`)*
- [x] Abruf-Budget für begrenzte APIs *(Abrufe je Tag, danach letzter Stand)*

**Integrationen**
- [x] Tailscale / Headscale *(Geräte, Schlüsselablauf, lange offline)*
- [x] OPNsense / pfSense / UniFi *(Dienst „gateway“, kind wählt das System; WAN, Geräte, Updates)*
- [x] Jellyfin / Plex *(Streams, Bibliothek, Update)*
- [x] Sonarr / Radarr *(Zustand, hängende Downloads, Demnächst-Widget)*
- [x] Vaultwarden *(Nutzer ohne Zwei-Faktor)*
- [x] Speedtest Tracker *(gegen gebuchte Geschwindigkeit)*
- [x] Grocy *(Abgelaufenes, Einkaufsliste, Hausarbeit)*
- [x] DWD-Unwetterwarnungen *(über Bright Sky)*
- [x] GitHub *(PRs, Issues, CI, Releases)*
- [x] Energie und Kosten (Home Assistant, Tibber) *(Preiskurve, günstigste Stunden, Kosten; Leistung aus HA)*

**KI und Betrieb**
- [x] „Was tun?“ je Hinweis *(auf Klick; nur Titel und Begründung gehen raus, nie Standorthinweise; Antwort je Sprache gespeichert)*
- [x] Rechnungen in Mails per KI lesen *(auf Klick: PDF/Bild-Anhänge → Absender, Nummer, Fälligkeit, Betrag; Titel für Paperless)*
- [x] Automatische Sicherung des Dashboards mit Test-Wiederherstellung *(täglich VACUUM INTO, Probe-Öffnen: Integrität, Migrationen, Zeilen, Zugangsdaten entschlüsselbar; 7 Kopien)*
- [x] Hinweise abonnieren (je Person nach Quelle und Stufe) *(Quellen je Benachrichtigungskanal)*
- [x] Ruhezeiten für Benachrichtigungen *(Ruhezeit gab es; neu: Kritisches kommt trotzdem, wahlweise stumm; Wiederholung offener kritischer Hinweise)*

**Obsidian – Vorschlag.** Obsidian hat keinen Server; der Vault ist ein Ordner mit Markdown. Drei Wege, ihn zu lesen:

| Weg | Voraussetzung | Bewertung |
|---|---|---|
| Vault per Obsidian-Git-Plugin in ein Gitea-Repo | Plugin, privates Repo | **Empfohlen.** Gitea-Verbindung existiert schon; Lesen über die Contents-API, versioniert, kein offener Port am Rechner. |
| Vault per Syncthing auf den Server, schreibgeschützt ins Dashboard gemountet | Syncthing | Einfach, wenn Syncthing schon läuft; kein API-Token nötig. |
| Plugin „Local REST API“ | Obsidian-Desktop läuft | Nur solange der Rechner an ist – für Hinweise ungeeignet. |

**Entschieden (01.10.2026):** Der Vault liegt bereits im privaten Repo `ObsidianPrivat` auf Gitea (der Fast-Note-Sync-Server committet selbst, kein Push-Mirror; das bleibt so, im Repo liegt der ganze Vault). Andon liest von dort; erster Nutzen ist der Abgleich mit den Compose-Repos, siehe Phase 15.

Weitere mögliche Auswertungen: offene Aufgaben `- [ ]` mit Fälligkeit (Tasks-Plugin `📅 2026-10-01`) als Hinweise und in den Fristen; Notizen mit `wiedervorlage:` im Frontmatter; fehlende Tagesnotiz; wachsender Eingangsordner; Kundennotizen, deren letzte Änderung lange zurückliegt, während in Kimai für diesen Kunden gebucht wird.

### Phase 13: Datenkreuzungen

Analysen, die erst aus mehreren Diensten zusammen entstehen. Grundlage ist ein Kennzahl-Verlauf (täglicher Wert je Kennzahl), ein Versionsregister und eine Ereignis-Zeitleiste. Standortdaten werden nur lokal verrechnet.

**Grundlage**
- [x] Kennzahl-Verlauf, Versionswechsel und Ereignisse speichern *(Tabellen `samples`, `versions`, `events`; 400 Tage Verlauf)*

**Geld und Zeit**
- [x] Vollkosten-Stundensatz je Kunde (Kimai, Invoice Ninja, Dawarich) *(Tabelle `full_rates`, Regel `in.rate_below`; Fahrzeit aus Kilometern bei 50 km/h)*
- [x] Gebundenes Geld in unfakturierter Arbeit (Alter × Satz) *(Tabelle `unbilled_aging`; Hinweis nach Alter gab es schon: `kimai.unbilled_hours`)*
- [x] Zahlungseingänge automatisch zuordnen und buchen (Sure → Invoice Ninja) *(Rechnungsnummer im Buchungstext vor Betrag; „Buchen“ auf /billing; Regel `cross.payment_unmatched`)*
- [x] Belege, die fehlen (Sure gegen Invoice Ninja, Paperless, Mail) *(`cross.expense_unrecorded` prüft jetzt auch Paperless und Rechnungsmails; Tabelle `missing_receipts`)*
- [x] Abhängigkeit von einem Kunden (Umsatz- und Stundenanteil, 5/6-Grenze) *(`in.client_concentration` um den Stundenanteil aus Kimai ergänzt)*
- [x] Frei verfügbares Geld (Konto minus Steuern und Fixkosten) *(KPI `safe_to_spend`, Regel `sure.spendable_negative`)*
- [x] Abo-Radar mit Nutzung (Sure, Paperless, authentik, Klicks) *(Tabelle `subscriptions`, Regel `sure.subscription_unused`; Abgleich über das erste Wort des Namens)*
- [x] Projektbudget-Prognose (Erschöpfungsdatum gegen Projektende) *(`kimai.budget_pace` nennt jetzt das Datum; auch Zeitbudgets; Tabelle `budget_forecast`)*
- [x] Termine ohne Zeitbuchung (Kalender, Kimai, Feiertage, Abwesenheit) *(neue Verbindung „Kalender“; Regeln `calendar.unbooked`, `kimai.booked_free_day`)*
- [x] Projektmarge mit allen Kosten *(Tabelle `project_margins`, Regel `kimai.margin_low`; Kostensatz `costs.hourly_cost` oder Fixkosten ÷ 168 h; Snipe-IT fehlt mangels Kundenbezug)*
- [x] Auftragsloch früh sehen (Vorjahr gegen jetzt) *(Regel `in.order_gap`)*
- [x] Fahrtenbuch-Auszug für die Steuer *(`fahrten.csv` im Jahrespaket)*
- [x] Arbeitslast und Erholung *(Regel `kimai.workload`; späte Commits fehlen: Gitea/GitHub liefern keine Commit-Zeiten)*

**Homelab: Ursache und Wirkung**
- [x] Ereignis-Zeitleiste mit Vorgeschichte *(Seite /timeline; im Hinweis „Kurz davor“: Ereignisse der 2 Stunden vor dem Auftreten)*
- [x] Langsamer seit dem Update *(Regel `system.slower_since_update`: Kuma-Antwortzeit 7 Tage vor gegen nach dem Versionswechsel)*
- [x] Speicher- und Plattenprognose *(Regel `system.storage_forecast`, Widget „Speicherprognose“; Pools, Proxmox-Speicher, Borg, Immich; Garantie aus Snipe-IT fehlt noch: Platten sind dort nicht eindeutig zuzuordnen)*
- [x] Was seit dem letzten Backup neu ist *(Regel `backups.unsaved`: Fotos, Dokumente, Dateien seit der jüngsten Sicherung)*
- [x] Gutes Fenster für Updates *(Widget „Update-Fenster“)*
- [x] Angriffsfläche je öffentlichem Dienst *(Regel `pangolin.exposure`, Tabelle `exposure`; Pangolin-Anmeldung, Zertifikat, offene Updates)*
- [x] Verdächtige Anmeldungen (authentik, Standort) *(Regel `authentik.login_anomaly`: neues Land oder > 500 km vom Dawarich-Besuch)*
- [x] Auffällige Geräte im Netz (DNS-Filter, Router, Tailscale) *(Regeln `dns.device_spike`, `dns.new_device` aus Pi-hole/AdGuard; Abgleich mit Router- und Tailscale-Namen fehlt: dort gibt es keine IP-Adressen)*
- [x] Nachweis für den Internetanbieter (§ 57 TKG) *(Seite /reports/isp mit CSV, Regel `speedtest.contract`)*
- [x] Vorbereitung auf Unwetter *(Regel `dwd.storm_prep` mit USV-Stand aus Home Assistant)*
- [x] Downloads ohne VPN *(Regel `gluetun.downloads_exposed`)*
- [x] Domain-Kette auf einen Blick *(Tabelle `domain_chain`; `domains.expiring` nennt, was an der Domain hängt)*

**Kosten und Nutzen**
- [x] Dienste, die niemand nutzt *(Regel `system.unused_service`: Komodo-Stacks, Proxmox-Gäste, TrueNAS-Apps gegen Kachel-Klicks und authentik; ohne Nutzungssignal keine Aussage)*
- [x] Stromkosten je Dienst *(Widget „Homelab-Kosten“: Leistung aus Home Assistant × Preis, verteilt nach CPU der Proxmox-Gäste)*
- [x] Wartung in günstige Stunden legen *(Regel `energy.shift_jobs`: letzte Laufzeit von Borg, TrueNAS, Proxmox gegen den Tibber-Tagesverlauf)*
- [x] Gesamtkosten des Homelabs *(Widget „Homelab-Kosten“: Strom, Abschreibung aus Snipe-IT, Domains, Hosting-Abos aus Sure, Cloud-Vergleich; Einstellungen im Bereich)*
- [x] Betrieblicher Anteil der IT-Kosten *(Anteil der Stacks und Repos mit Kunden- oder Projektnamen; `it-kosten.csv` im Jahrespaket)*
- [x] Ersetzen oder weiterbetreiben *(Regel `snipe.replace_worth`: Snipe-IT-Gerät ↔ Leistungssensor per Name)*
- [x] Energieverbrauch bereinigt ums Wetter *(Regel `energy.weather_adjusted`: Heizgradtage aus Open-Meteo, Optionen lat/lon an der Tibber-Verbindung)*
- [x] Wochenrückblick mit Zusammenhängen *(im wöchentlichen Digest und als Widget „Woche in Zahlen“; lokal berechnet)*
- [x] Schwellen, die nur nerven *(Hinweise-Seite: Regeln, deren Hinweise in 90 Tagen zu ≥ 80 % weggeklickt wurden)*
- [x] Was ein Hinweis kostet *(Betrag am Hinweis, Sortierung nach Geldwert)*


### Phase 14: Ausbauliste (28.09.2026)

Drei Durchgänge durch den Code; umgesetzt, jeweils mit Test.

**Sicherheit und Betrieb**
- [x] Egress-Schutz beim Verbindungsaufbau (Weiterleitungen, DNS-Rebinding); offener Modus sperrt Loopback und Link-Local
- [x] Server-Timeouts, HSTS, COOP, CSP ohne `unsafe-inline` (`data-style` über das CSSOM), iframe-Ursprünge geprüft
- [x] Secrets als Dateideskriptor statt Umgebungsvariable; Webhook-URLs widerrufbar und gedrosselt; `?refresh` gedrosselt
- [x] Release-Build ohne Demo-Modus (`-tags release`, `scripts/release-check.sh`); Galerie mit neutralem `sample.json`
- [x] CI: staticcheck, govulncheck, Race-Test, gepinnte Actions, SBOM und cosign (GitHub); `latest` nur für Releases
- [x] Prozesszustand auf Admin → Instanz; `andon healthcheck`; Alpine 3.24

**Leistung**
- [x] Geteilte HTTP-Transports, Sitzung ohne Schreibsperre, Lesepfade mit `WithRead`, Cache begrenzt, gleichzeitige Abrufe gebündelt, Polling pausiert in verborgenen Tabs, statische Dateien gzip

**Hinweise**
- [x] Neue Regeln: `glances.*`, `system.clock_skew`, `wallos.renewal_soon`, `github.review_waiting`/`stale_pr`, `snipe.checkin_overdue`, `domains.mail_auth`, `backups.restore_untested`, `docker.*`
- [x] Wartungsfenster, Eskalation nach Tagen, Anleitung je Regel, Ausfälle über Proxmox-Knoten und -Gäste, Suche/Tasten/Erledigt-Liste, Pausieren bis Montag/Monatsanfang

**Kacheln und Seiten**
- [x] Ablauf-Zeitstrahl, Geldfluss, Hinweis-Verlauf, Umami, Immich, Container; KPI-Details
- [x] Kundenseiten (`/clients`), Host-Seiten (`/hosts`)
- Nicht umgesetzt: `system.version_drift` (die Versionstabelle hält eine Version je Dienst und Bereich, nicht je Host), „Wichtigster Hinweis groß“ (deckt die Wandampel ab), „Seit gestern“-Seite (deckt die Begrüßung ab)

### Phase 15: IT-Doku-Abgleich und Homelable (begonnen 06.10.2026)

Ziel: Die IT-Doku in Obsidian aktuell halten. Andon erkennt, wo Doku und Compose-Dateien auseinanderlaufen, und gibt die Befunde an Hansei weiter; nur Hansei schreibt in den Vault, und nur nach Freigabe. Zusätzlich zeichnet Andon die Doku als Grafik in eine Homelable-Instanz auf Regis.

| | liest | schreibt |
|---|---|---|
| Hansei | Vault, Andon-Befunde (`/api/docs`) | Obsidian (nach Freigabe im Diff); seinen Stand an Andon (Webhook) |
| Andon | Vault-Frontmatter, Compose-Repos, Komodo | Hinweise; Homelable (abgeleitete Ansicht) |
| Homelable | nur, was Andon schickt | nichts |

**Stand 07.10.2026:** Andon-Seite bis auf `docs.drift`, Komodo und Homelable fertig (PRs #63–#71; Demowelt shrippen.github.io #25–#28, Hansei #8–#10).
- Live geprüft gegen git.arianw.de: 118 Stacks (Regis 79, Eredin 28, Plötze 11), 275 Notizen in `IT/Dienste`, `IT/Geräte`, `IT/Orte`, `IT/Netzwerk`, `IT/Allgemeines`. Kein Vault-Eintrag hat bisher ein `Compose`-Feld, daher melden die Regeln alle 118 Stacks als undokumentiert; das ist die Arbeitsliste fürs Nachtragen.
- Produktiv noch nicht eingerichtet: an der Gitea-Verbindung die Optionen `docs_repo: shrippen/ObsidianPrivat` und `docs_paths: [IT/Dienste, IT/Geräte, IT/Orte, IT/Netzwerk, IT/Allgemeines]`; eine Verbindung Hansei (Webhook-Adresse steht auf ihrer Seite); ein Lese-Token für Hansei.
- Schnittstelle zu Hansei:
  - `GET /api/docs?token=…` (Lese-Token) → `{"complete": bool, "findings": [{"id", "rule", "host", "stack", "note", "path", "link", "note_url", "compose", "services": [{"name", "image", "ports"}]}]}`. `complete: false` heißt: Stacks oder Notizen nicht vollständig gelesen, ein fehlender Befund ist dann keine Erledigung.
  - IDs: `docs.missing:<host>/<stack>`, `docs.orphan:<Notizpfad>`, `docs.deprecated_live:<host>/<stack>`.
  - `POST <Webhook-Adresse der Verbindung Hansei>` mit `{"state": {"review", "feedback", "done", "conformity" (0..1), "claimed": [IDs]}}` ersetzt den letzten Stand (höchstens 60 Aufrufe je Minute, Körper bis 128 KiB). Nach jeder Änderung und beim Start senden.
- Offen auf Hansei-Seite: Phase 05 in `hansei/ROADMAP.md` (Feld `Compose` im Vault, Befunde abholen, Stand senden).

**Ausgangslage (Abgleich vom 01.10.2026, nur über Namen):** Regis 79 Stacks, Eredin 28, Plötze 11 in `docker-compose-{regis,eredin,ploetze}`; 225 Notizen in `IT/Dienste` (92 davon deprecated), nur 18 verlinken ihre `compose.yaml`. Rund 27 Stacks ließen sich keiner Notiz zuordnen; ein Teil sind nur abweichende Namen (`kometa` = Plex-Meta-Manager, `seerr` = Jellyseer, `digikam_db`), ein Teil echte Lücken (`beets-flask`, `journiv`, `stirling-pdf`, `sure`, `hievents`, `lauti`, `andon`), `tdarr` und `tubesync` sind nur als deprecated-Notiz da. Deshalb Zuordnung über ein festes Feld, nicht über Namen.

**Voraussetzungen (Vault, über Hansei – siehe `hansei/ROADMAP.md`)**
- Frontmatter-Feld `Compose`: URL zur `compose.yaml` in Gitea, als Liste erlaubt. Schlüssel für den Abgleich und zugleich der Link, den `IT/Design.md` ohnehin verlangt
- Infrastruktur-Stacks (`komodo_periphery*`, `newt-*`, `glances-*`, `tailscale-*`, `caddy-*` …) stehen im `Compose`-Feld der Gerätenotiz. Netze (Tailscale, Pangolin/newt …) haben eine eigene Netz-Notiz (z. B. `IT/Allgemeines/Tailscale.md`, `IT/Netzwerk/`), die ihre Stacks ebenfalls unter `Compose` führt; die genaue Kennzeichnung (Tag oder Feld) wird in `IT/Design.md` festgelegt
- `Compose` ist Pflichtfeld für `IT/Dienste/{Regis,Eredin,Plötze}` (Hansei-Prüfregel)

**Quellen**
- [x] Compose-Repos: `docker-compose-*` über die vorhandene Gitea-Verbindung (`git/trees`), Host aus dem Repo-Namen (`ploetze` → Plötze); je Stack Dienste, Images, Ports, Labels. `environment`-Werte werden beim Lesen verworfen *(`GiteaDataset.Stacks`, `internal/sources/compose.go`; Dateien je Blob-SHA nur einmal gelesen)*
- [x] Obsidian: Repo `ObsidianPrivat`, nur die Teilbäume `IT/Dienste`, `IT/Geräte`, `IT/Orte` und die Netz-Notizen, nur Frontmatter (`Compose`, `Gerät`, `deprecated`, `URL`, Ports, `Backup via`, `SSO …`, `abhängig von`, `letzte Prüfung`, `Orte`). Nie den ganzen Baum laden: Gitea kürzt ihn bei rund 3000 Einträgen (`truncated`), und Andon sieht so keine Pfade anderer Ordner *(`GiteaDataset.Notes`, `internal/sources/itdocs.go`; Optionen `docs_repo`, `docs_paths` an der Gitea-Verbindung; `Ort` wie `Orte` gelesen)*
- [x] Hansei-Stand für das Widget „Batches warten“ *(statt Statusnotiz im Vault: Verbindung Hansei, Hansei schickt seinen ganzen Stand per Webhook `{"state": …}`; Andon behält den letzten)*
- [ ] Später: Komodo-Stand dazu (Stack im Repo, aber nicht deployt und umgekehrt)

**Regeln und Widgets**
- [x] `docs.missing`: Stack ohne aktive Notiz (bzw. ohne Eintrag in einer Geräte- oder Netz-Notiz) *(ein Hinweis je Host; Zuordnung über `Compose`-Link auf Datei oder Stack-Ordner, `metrics.CheckDocs`)*
- [x] `docs.orphan`: aktive Notiz, deren `Compose`-Link auf keinen Stack zeigt
- [x] `docs.deprecated_live`: Notiz deprecated, Stack liegt noch im Repo
- [ ] `docs.drift` (später): URL, Ports oder Image im Frontmatter weichen von der Compose-Datei ab
- [x] Widget „Doku-Abdeckung“ je Host (X von Y Stacks dokumentiert, Liste der Lücken) *(`docs_coverage`; Popup mit Aufgaben je Lücke und Links)*
- [x] Widget „Batches warten“ *(`hansei_batches`)*

**Übergabe an Hansei**
- [x] API-Endpunkt für die `docs.*`-Befunde (Token wie bisher): Regel, Host, Stack, Notizpfad, Compose-Auszug ohne Secrets. Hansei holt sie ab und macht daraus Batches; nach Freigabe und Sync verschwindet der Hinweis beim nächsten Prüflauf von selbst *(`GET /api/docs`, Token mit Leserecht; je Stack bzw. Notiz, `complete` = alles gelesen; Auszug ohne Labels)*
- [x] Rückmeldung von Hansei: `claimed` im Stand nennt die Befund-IDs (`id` aus `/api/docs`), an denen ein Batch arbeitet; deren Hinweise warten, `docs.missing` zählt sie nur noch mit. Erledigt ist ein Befund erst, wenn der Vault ihn nicht mehr zeigt

**Homelable als Ansicht**
- [ ] Homelable (github.com/Pouzor/homelable, MIT) als Stack `docker-compose-regis/homelable`, Version gepinnt (Renovate), nur intern (LAN/Tailscale, keine Pangolin-Resource: Swagger unter `/docs` ist ohne Anmeldung). Lokaler Login, weil der OIDC-Modus keinen Skriptzugang hat (Issue #291). Nicht genutzt: eigene Dokumentation (zweites Wiki neben Obsidian), Netzwerk-Scanner, Live View, Docs View
- [ ] Ausgang `outbound/homelable`: Andon baut aus Quellen und Regeln ein Graph-Modell und gleicht es über die REST-API ab. Eigener Canvas „IT-Doku (aus Obsidian)“; von Hand gezeichnete Canvases bleiben unberührt. JWT läuft nach 24 h ab → bei `401` neu anmelden, Passwort verschlüsselt wie andere Zugangsdaten
- [ ] Abbildung: Geräte als Host-Knoten in ihrer Zone (`Orte`), Dienste im Host verschachtelt (`Gerät`), `abhängig von` als Verbindungen; Eigenschaften URL, Ports, Backup, SSO, `letzte Prüfung`, `obsidian://`-Link zur Notiz
- [ ] Infrastruktur-Stacks: Knoten im Host **und** zusätzlich ein eigenes Netz (z. B. Tailscale, Pangolin), verbunden mit allen Hosts, auf denen ein zugehöriger Stack läuft
- [ ] `docs.*`-Befunde sichtbar: Stacks ohne Notiz als blasse Knoten „Doku fehlt“, veraltete `letzte Prüfung` markiert
- [ ] Live-Status in Homelable an (`check_method` aus URL/Ports), **ohne Benachrichtigungen**; alarmiert wird weiter nur über Andon und Uptime Kuma
- [ ] Abgleich idempotent: Andon speichert je Notizpfad bzw. Stack die Homelable-ID, sendet nur Änderungen und nie Positionen (eigenes Layout bleibt), entfernt Knoten, deren Notiz fehlt oder deprecated ist
- [ ] Demo: Homelable-Verbindung als `demo://` mit Studio-Weber-Daten (nur im Demo-Build)

### Phase 16: Icons für Kachelgruppen und Abschnitte (notiert 05.10.2026)

- [x] Ein Icon je Kachelgruppe (Thema: Überblick, Arbeit & Geld, Auswertung, Homelab, Sicherheit, Zuhause, Links), zuerst in Kante, dann in der Kachel-Bibliothek vor dem Gruppennamen und in der Themen-Navigation
- [x] Abschnitte dürfen vor ihrem Namen ein Icon haben, mit derselben Icon-Logik wie Kacheln (Favicon, `si-*`, `hl-*`, `sh-*`, `mdi-*`, Font Awesome, URL, Upload, Emoji); wählbar in den Abschnittseinstellungen, auch in der Code-Ansicht und im Export

### Phase 17: Fahrten aus Dawarich, privat und beruflich (umgesetzt 06.10.2026)

Ziel: jede Fahrt aus den Dawarich-Tracks mit echter Strecke, eingeordnet als beruflich, Pendeln oder privat, mit sichtbarem Grund; Auswertungen für beide Seiten. Ersetzt die Schätzung aus Aufenthalten (Luftlinie × 1,3, eine Fahrt je Kundentag). Das Kimai-Plugin Anfahrten ist nur ein zusätzlicher Datenpunkt; Andon muss ohne es und bei lückenhafter Nutzung funktionieren.

```
Dawarich tracks ──► Fahrten (Zeit, km, Verkehrsmittel, Start/Ziel)
                        │
Orte ───────────────────┤  Start/Ziel → Ort (Kunde, Zuhause, Arbeit, sonst)
 (Andon ⇄ Anfahrten     │
  ⇄ Dawarich-Areas)     ▼
Kimai-Zeiten ──────► Einordnung ──► beruflich / Pendeln / privat + Grund
Anfahrten-Fahrten ──►   │
                        ▼
              Kennzahlen, Regeln, Kacheln, Export
```

**Einordnung** (erste zutreffende Regel gilt)

| # | Bedingung | Klasse | Grund |
|---|---|---|---|
| 0 | von Hand eingeordnet (ohne Plugin oder kein Auto) | diese | `manual` |
| 1 | Fahrt im Plugin im selben Zeitfenster | deren Art | `plugin` |
| 2 | mindestens 50 % der Fahrzeit liegen in gebuchter Kimai-Zeit (jede Buchung, auch interne Projekte) | beruflich, Kunde aus der Buchung (intern: ohne Kunde) | `kimai` |
| 3 | Start oder Ziel ist ein Kundenort | beruflich, Kunde aus dem Ort | `kunde` |
| 4 | Zuhause ↔ Arbeitsort (nur wenn ein eigener Arbeitsort eingestellt ist) | Pendeln | `pendel` |
| 5 | sonst | privat | `rest` |

Kettenregel: eine Fahrt zwischen zwei beruflichen Fahrten desselben Tages ist beruflich (Zuhause → Kunde A → Kunde B → Zuhause). Grund `kunde` ohne Kimai-Buchung am Tag = „beruflich, unbestätigt“ und Hinweis `geo.visit_without_time`.

**Einstellung Startpunkt** (Bereichseinstellungen „Fahrten“): Standard „Zuhause ist Betriebsstätte“ (Freiberufler; jede berufliche Fahrt beginnt zu Hause, kein Pendeln); alternativ „eigener Arbeitsort“ (Ort vom Typ Arbeit; Zuhause ↔ Arbeit = Pendeln mit Entfernungspauschale). Bestimmt auch, ab wann die Verpflegungspauschale zählt.

**Datenhaltung:** so wenig wie möglich in der Datenbank. Gerechnet wird direkt auf den Dawarich-Daten über den Quellen-Cache; gespeichert werden nur die Zuordnung der Orte (ohne Plugin) und die Einstellungen. Fahrten je Monat als kompakter Datensatz (Zeiten, km, Verkehrsmittel, Start-/Zielort); vergangene Monate mit langer Gültigkeit, der laufende kurz. Beim ersten Abruf wird das Jahr nachgeladen, danach nur neue Tracks.

**1. Quelle Tracks**
- [x] Tracks in `dawarich.data`: `/api/v1/tracks` (Seiten, Zeitfenster ab Jahresbeginn), Segmente aus `/tracks/{id}` (die Liste liefert keine). Abgeschlossene Tracks bleiben im Speicher (je Id, Revision, Ende), je Abruf höchstens 300 neue, der Rest beim nächsten (`TracksPartial`)
- [x] `metrics`: Tracks → Fahrten. Folge gefahrener Segmente = Fahrt, Halt ab Stoppdauer oder Fußweg trennt; Fuß-, Rad- und ÖPNV-Wege bleiben als eigene Fahrten mit Verkehrsmittel (für private Auswertungen)
- [x] Ohne Tracks (alte Dawarich-Version, noch nicht berechnet): bisherige Schätzung, als „geschätzt“ markiert
- [x] Demowelt: Tracks für Studio Weber (`shrippen.github.io/demo/world/`, dann `sync-demo.py`)

**2. Orte und Abgleich**
- [x] Start/Ziel → Ort über Dawarich-Areas und -Places, Orte des Plugins und die Andon-Zuordnung; Schlüssel ist die Area-ID, nicht der Name (Option `areas` migrieren)
- [x] Reiter „Orte“ in der Akte der Dawarich-Verbindung (MANAGE): je Area/Place Typ (Kunde, Zuhause, Arbeit, privat) und Kunde aus dem Kimai-Peer, Herkunft sichtbar; Orte mit Besuchen ohne Zuordnung oben; Vorschlag bei ähnlichem Kundennamen. Neuer Ort aus einem häufigen unbekannten Ziel einer Fahrt (Koordinaten aus der Fahrt). Kante-Bausteine. Namensvorschlag aus Dawarichs eigenem Geocoder (`/api/v1/places/nearby`, Quelle `dawarich.nearby`, je Zeile nachgeladen); Andon fragt keinen Geocoder selbst
- [x] Abgleich in beide Richtungen: ein in Andon angelegter oder geänderter Ort wird als Area in Dawarich (`POST /api/v1/areas`) und als Ort im Plugin angelegt bzw. geändert (Kunde, Typ, `dawarichAreaId`). Area ohne Plugin-Ort → Ort im Plugin anlegen. Kein Löschen über Systeme hinweg. Mit Plugin ist es der Speicher der Zuordnung (Andon hält keine Kopie), ohne Plugin die Dawarich-Verbindung
- [x] Schreiben ist neu für diese Dienste: eigener Ausgang `outbound/places.go` (Schicht wie Apprise), Token mit Schreibrecht; schlägt Schreiben fehl, zeigt der Reiter den Fehler. Verbindungstest der Kimai-Verbindung meldet ein nur lesbares oder zu altes Plugin (`ping`: `editOwn`, `placesWrite`); ohne Schreibrecht bleiben Orte und Einordnungen in Andon. Dawarich-API-Keys kennen keine Rechtestufen, dort gibt es nichts zu prüfen
- [x] Plugin Anfahrten: `POST/PATCH /api/mileage/places` (Name, Typ, Kunde, Koordinaten, Radius, `dawarichAreaId`), Feature im `ping`. Endpunkte von Dawarich vor dem Bau gegen `/api-docs` prüfen

**3. Einordnung**
- [x] Reine Funktion nach der Tabelle oben, Test je Regel und für die Kettenregel; Plugin-Fahrten aus `/api/mileage/trips` (ohne Plugin entfällt Regel 1)

**4. Bestehendes umstellen**
- [x] Fahrtkosten, `fahrten.csv` (Jahrespaket), Vollkosten-Stundensatz: echte km und Fahrzeit, nur berufliche Fahrten; Kilometergeld nur für Auto und Motorrad
- [x] `geo.per_diem`: Abwesenheit vom Startpunkt (erste Abfahrt bis letzte Ankunft an beruflichen Tagen) statt Zeit beim Kunden; `full_day` für mehrtägige Reisen (heute ungenutzt)
- [x] `geo.time_without_visit`: Buchung „vor Ort“ ohne berufliche Fahrt zum Kunden

**5. Auswertungen**
- [x] Beruflich: km, Fahrzeit, Kosten je Kunde und Monat; Fahrzeit als Anteil der gebuchten Zeit; Pendeltage und Entfernungspauschale (nur mit Arbeitsort); Fahrtkosten, die in keiner Rechnung stehen (Invoice Ninja)
- [x] Privat: km je Monat und Verkehrsmittel, häufigste Ziele, Fahrten am Wochenende und im Urlaub (Holiday-Bundle), Vergleich zum Vorjahr (Gesamt-km aus Dawarichs Statistik, Tracks reichen bis Jahresbeginn)
- [x] Beides: Anteil privat/beruflich, Heatmap Wochentag × Stunde, Fahrzeit je Woche, Privatanteil eines betrieblichen Fahrzeugs (> 50 %: 1-%-Regel nicht zulässig), km gegen Tankkosten aus Sure (Verbrauch, € je km)
- [x] Datenqualität: Ort mit Besuchen ohne Zuordnung, viele „unbestätigt“, Tracks nicht berechnet, berufliche Autofahrten, die im Plugin fehlen (`geo.plugin_missing`); „unbestätigt“ über `geo.visit_without_time`
- [x] Kacheln: Reise-Kachel mit Balken privat/beruflich, Dialog mit km je Monat und Klasse, Heatmap, Stunden je Woche, Kunden, Zielen, Verkehrsmitteln; Tabellen `trips` (Klasse, Grund), `trip_customers`, `destinations`. Detail je Fahrt zeigt die Strecke aus `dawarich.route` (`PickQueries`: Abfrage für den gewählten Eintrag)

**6. Später**
- [x] Klasse einer Fahrt in Andon von Hand ändern (für Nutzer ohne Plugin); mit Plugin dort als Fahrt anlegen *(Formular im Fahrt-Detail, `sites.SetClass`: Auto/Motorrad mit Plugin als Fahrt dort bzw. deren Art geändert, sonst Option `rides` der Dawarich-Verbindung, Regel 0 `manual`; „automatisch“ löscht nur die Option)*

### Phase 18: Kacheltypen aus der Recherche (begonnen 07.10.2026)

Ziel: die Kacheltypen aus [`research/tile-types.md`](research/tile-types.md), je Dienst mit Quelle, Demodaten (Studio Weber), Kachel oder Beitrag zu einer Sammelkachel, Regeln und **Queranalysen**. Ein Schritt = ein PR (dazu die Demowelt in `shrippen.github.io`). Sammelkacheln (Backups, Updates, Medien) lesen neue Dienste über eine gemeinsame Schnittstelle, nicht über Sonderfälle je Dienst.

**Schritt 1: Gemeinsame Sicherungs-Schnittstelle**
- [x] `sources.BackupSource` (Datensatz nennt seine Sicherungen: Werkzeug, Gegenstand, letzte Sicherung, Ergebnis); Borg, PG Back Web, TrueNAS stellen um; Backup-Kachel, `backups.*`, `LastBackup` (Update-Fenster) lesen jede Quelle mit der Schnittstelle

**Schritt 2: Healthchecks** (healthchecks.io, API v3)
- [x] Quelle, Kachel „Herzschläge“ (Status je Check, letzter Ping), Regel `heartbeat.down` *(Verlauf je Check noch offen)*
- [x] Quer: `cross.heartbeat_backup` (Check mit Namen einer Sicherung meldet sich nicht, die Sicherung ist aber frisch, oder umgekehrt)
- [x] Checks auf der Host-Seite *(über ein Tag mit dem Hostnamen: Tag `nas` → nas.lan; Checks kennen keinen Host)*

**Schritt 3: Prometheus** (und Alertmanager-kompatible Alerts)
- [x] Quelle: feuernde Alerts (`/api/v1/alerts`) als Hinweise `prometheus.alert` mit Stufe aus dem Label `severity`; Kachel „Prometheus-Wert“ (PromQL als Zahl oder Verlauf)
- [x] Quer: Alerts je Host (Label `instance`) auf der Host-Seite; Alert und Uptime-Kuma-Ausfall desselben Hosts als ein Hinweis *(kritische Alerts sind ein Signal für `system.outage`, der einzelne Alert wird dann unterdrückt)*

**Schritt 4: CVE** (NVD-API, Schlüssel optional)
- [x] Quelle: neue CVEs mit CVSS und betroffenen Versionen; Kachel „Sicherheitslücken“ *(Verbindung `nvd`: alle hohen und kritischen CVEs der letzten 30 Tage, ohne Suchwortliste; der Abgleich mit den Images läuft quer, `metrics.ImageCVEs`)*
- [x] Quer: `cross.image_cve` (laufendes Image, Tag im betroffenen Bereich: betroffen; sonst prüfen), verschärft, wenn Pangolin den Dienst öffentlich macht (`exposure`); Treffer auf der Host-Seite

**Schritt 5: CI** (Drone; GitHub Actions und Gitea Actions der vorhandenen Verbindungen)
- [x] Quelle Drone (`/api/user/repos?latest=true`, Builds je Repo); Kachel „CI-Läufe“ über Drone, GitHub und Gitea (`sources.CISource`) *(GitHub und Gitea nennen nur den letzten Lauf; ein Verlauf bräuchte je Repo eine weitere Abfrage)*
- [x] Regel `drone.failing` (Standardzweig rot seit N Stunden; GitHub und Gitea haben schon `github.ci_failed`, `gitea.actions_failed`)
- [x] Quer: `cross.release_red_ci` (Release auf GitHub, der letzte Build davor war rot, aus jedem CI-Anbieter)
- [x] Quer: `cross.ci_red_deployed` *(die Komodo-Quelle liest Deploys (Stack, Zeit, Commit, wer) und das Git-Repo je Stack in den Datensatz; Repo zum Stack: Option `ci_repos`, sonst Komodos Repo, sonst ein CI-Repo mit dem Namen des Stacks. Rot heißt: der Build des deployten Commits, ohne Commit der letzte Build davor, war fehlgeschlagen. Drone und GitHub nennen dafür den Commit; Gitea nennt nur rote Läufe ohne Zeit und zählt nicht mit)*

**Schritt 6: Sicherung** — Proxmox Backup Server, Kopia, Duplicati, Backrest, UrBackup
- [x] Quellen mit `BackupSource`; PBS zusätzlich Belegung je Datastore und Verify-Jobs; Regeln `pbs.verify_failed`, `pbs.datastore_full` *(UrBackup-Anmeldung und Duplicati-Token als Treiber; Fehlertext des Werkzeugs als `BackupJob.Note`)*
- [x] Quer: VM ohne Sicherung *(statt eigener Regel zählt `proxmox.backup_old` frische PBS-Sicherungen mit)*; `cross.pbs_orphan` (PBS-Gruppen von Gästen, die Proxmox nicht mehr hat)
- [x] Datastore-Füllstand gegen TrueNAS-Pool *(PBS nennt den Pool nicht: Option `pools: {archiv: tank}` der PBS-Verbindung. `cross.pbs_pool` warnt, wenn der Pool fast voll ist (85 %), der Datastore aber nicht, und meldet, wenn der Pool 15 Punkte voller ist, als der Datastore meint. Eine eigene PBS-Kachel gibt es nicht; der Dialog der Backup-Kachel zeigt die Datastores mit Belegung, Pool und dessen Belegung)*

**Schritt 7: Updates** — What's Up Docker, Watchtower, Releases
- [x] WUD (Container mit neuer Version), Watchtower (Metriken: geprüft, aktualisiert, fehlgeschlagen), Releases beobachteter Repos über GitHub; alles als Update-Hinweise in `updates` und `update_window` *(Gitea liest keine Releases)*
- [x] Quer: `cross.update_unbacked` (Container aktualisiert ohne frische Sicherung), `cross.release_newer` (neues Release gegen laufenden Image-Tag)
- [x] Nachtrag zu Schritt 6: `backups.job` meldet fehlgeschlagene oder veraltete Sicherungen der Werkzeuge ohne eigene Regeln; neue Backup-Regeln im Thema „Backups“

**Schritt 8: Strom** — PeaNUT (NUT), apcupsd, OpenDTU, EVCC
- [x] USV: Ladung, Restlaufzeit, Last; Solar: Leistung und Ertrag; EVCC: Laden, Netz, 30-Tage-Werte *(eigene Kachel „Strom“ statt Beiträgen zu `energy`; apcupsd über sein NIS-Protokoll)*
- [x] Regeln `ups.on_battery`, `ups.runtime_low`, `ups.replace_battery`, `opendtu.offline`
- [x] Quer: Stromausfall *(USV auf Batterie ist die Ursache in `system.outage`, Einzelhinweise der Monitore und Alerts fallen weg)*, `cross.charge_expensive` (EVCC × Tibber)
- [x] Quer: `cross.ups_load` (Laufzeit gegen Zahl der Hosts), `cross.charge_business` (EVCC × Fahrten) *(`cross.ups_load`: Option `hosts` der USV-Verbindung (Liste oder je USV); warnt, wenn die Laufzeit jetzt oder die kürzeste der letzten 7 Tage unter Hosts × 5 min liegt. Der Verlauf hält dafür je Tag die kürzeste Laufzeit (`Readings.Low`, Tabelle `samples`). `cross.charge_business`: die EVCC-Quelle liest die Ladevorgänge (`api/sessions`, 62 Tage); die Energie eines Vorgangs fährt der Wagen bis zum nächsten, seine Kosten teilen sich nach den Auto-km der Dawarich-Fahrten dazwischen; gemeldet in den ersten Tagen des Monats für den Vormonat. EVCC und Dawarich müssen im selben Bereich liegen; die Demo hat dafür Maras Wallbox im persönlichen Bereich)*

**Schritt 9: Netz** — Traefik, Caddy, Nginx Proxy Manager, Headscale, Technitium, FRITZ!Box, UniFi
- [x] Routen (Traefik, Caddy, NPM) mit Zertifikat und Ziel, Kachel „Routen“; Technitium in der DNS-Kachel; FRITZ!Box über TR-064: Verbindung, Neuverbindung, DSL-Rate; UniFi liest zusätzlich die Clients *(Headscale las die Tailscale-Verbindung schon)*
- [x] Quer: Route auf gestoppten oder fehlenden Container *(in `routes.down`)*, `cross.route_undocumented`, `cross.line_vs_speed` (Sync-Rate gegen Speedtest und Gebuchtes), `cross.device_uninventoried` (Client von UniFi/OpenWrt ohne Snipe-IT-Asset)
- [x] FRITZ!Box-Abbrüche in den ISP-Bericht *(der Datensatz nennt, seit wann die Leitung steht (Uptime, auf die Minute); jeder Prüflauf hält das als Zustand „WAN“ im Verlauf, „–“ solange sie unten ist. Ein neuer Beginn ist eine Neuverbindung; die Ausfallzeit steht nur fest, wenn ein Prüflauf die Leitung unten sah (mindestens ab dann), sonst „kurz“. `/reports/isp` listet sie mit Zeit und Ausfall, auch für Bereiche ohne Speedtest Tracker, die CSV ebenso. Die Demo trennt jede Nacht um 04:02 und bringt Neuverbindungen der letzten Wochen im Verlauf mit)*

**Schritt 10: Medien** — Tautulli, Jellystat, Seerr, Audiobookshelf, Navidrome
- [x] Kachel „Jetzt läuft“ über `sources.StreamSource` (Jellyfin/Plex, Tautulli, Navidrome, Audiobookshelf), Jellystat-Statistik (30 Tage), Seerr: offene und hängende Anfragen *(Jellystat und Audiobookshelf ohne echte Instanz gebaut, Felder tolerant gelesen)*
- [x] Quer: Streams aller Quellen im Update-Fenster und in den Belegungsstunden; `seerr.stuck`, `cross.requests_arr` (hängende Anfragen, während Sonarr/Radarr Probleme melden)

**Schritt 11: Aufgaben** — Vikunja
- [x] Kachel „Aufgaben“ (fällig, überfällig, je Projekt), Regeln `vikunja.overdue`, `vikunja.due` *(mit Fälligkeitsdatum, so in Zusammenfassung und iCal)*
- [x] Quer: `cross.task_unbooked` (Aufgabe eines Kimai-Projekts erledigt, keine Zeit gebucht; Projekt über Vikunja-Projekt oder Label)

**Schritt 12: Finanzen** — Firefly III, Ghostfolio
- [x] Firefly III in der Domäne Zahlungen (wie Sure, `caps`) *(liefert Sures Form; `metrics.BankOf` gibt Regeln und Kennzahlen Sure oder Firefly; Sure-Regeln laufen als `firefly.*`)*; Ghostfolio: Kachel „Depot“ mit Verlauf und Positionen, `ghostfolio.drawdown`
- [x] Quer: Zahlungsabgleich mit Invoice Ninja auch über Firefly III; `cross.depot_reserve` (fehlende Rücklage, die das Depot decken könnte)
- [ ] Kacheln mit Sure als Partner (Liquidität, Kosten) lesen Firefly noch nicht; Depot nicht in der Liquidität *(Depot ist kein Bargeld)*

**Schritt 13: Lesen** — Hacker News, Lobsters, Reddit, YouTube-Kanäle, Twitch
- [x] Kachel „Lesen“ (Punkte, Kommentare, Alter), YouTube über die Kanal-Feeds, Twitch mit App-Zugang *(Verbindung `news`: Hacker News über die Algolia-API, Lobsters, `r/<sub>`, `youtube:<Kanal-ID>`, ohne Anmeldung; Verbindung `twitch` mit Client-ID und Secret, App-Token im Speicher; die Seiten wechseln sich in der Kachel ab, Kanäle live stehen oben)*
- [x] Quer: `cross.project_mentioned` (ein Beitrag nennt ein eigenes Repo oder einen KDE-Store-Eintrag) *(Repos nur über den Link, und nur die des GitHub-Besitzers (Option `owner`), beobachtete nicht; Store-Einträge über den Link oder den Namen im Titel)*

**Schritt 14: ESPHome, Fediverse** (Mastodon-API: Mastodon, GoToSocial, Akkoma)
- [x] ESPHome: Geräte, online, Firmware gegen Dashboard-Version; Fediverse: Folgende im Verlauf, Benachrichtigungen, Instanz-Version *(Kacheln „ESPHome“ und „Fediverse“; Regeln `esphome.offline`, `esphome.update`, `fediverse.mentions`; ungelesen nach dem Marker der Instanz; das Home-Assistant-Add-on ohne freigegebenen Port bleibt außen vor)*
- [x] Quer: ESPHome-Gerät offline gegen Home-Assistant-Entität, `cross.release_unannounced` (Release auf GitHub oder im KDE Store ohne Beitrag mit Link) *(`cross.esphome_ha`: Knoten online, alle seine Entitäten in Home Assistant nicht verfügbar, oder umgekehrt; Entitäten über den Knotennamen. Ein Release gilt als angekündigt, wenn ein eigener Beitrag es verlinkt oder nennt)*
- [x] Nachtrag: Lemmy (API v3, 0.19) *(Kachel „Lemmy“: ungelesene Antworten und Erwähnungen, beliebte Beiträge der abonnierten Communities, eigene Beiträge; Regel `lemmy.replies`; Anmeldung mit Benutzer und Passwort, Sitzung im Speicher, ohne Zwei-Faktor. Lemmy-Beiträge zählen in `cross.project_mentioned`, eigene in `cross.release_unannounced`)*

### Regeln: noch umzusetzen (notiert 06.10.2026)

Beide Regeln gelten für alle eigenen Projekte; in `agent.md` übernommen und angewendet (06.10.2026).

- [x] Visualisierungen (Heatmaps, Graphen, Diagramme) haben immer Achsenbeschriftungen oder eine Legende, sonst sind sie nutzlos. Bestehende Kacheln und Diagramme prüfen und nachrüsten; Regel in `agent.md` aufnehmen *(Regel in `agent.md` und `eigene/CLAUDE.md` steht; geprüft 06.10.2026)*
  - [x] Popups: `detail_graph` ohne Werte-Achse, Legende nur bei mehreren Reihen; Link-Prüfungen und `strips` ohne Zustandslegende; Antwortzeit (ms) ohne Achsen; Heatmap ohne Stufenlegende (`.heat-legend`); Zeitstrahl-Band ohne Werte-Achse; nirgends Hover *(Kante 1.15: Werte-Achse, Zeilen-Achse, Hover für Linien und Balken; Säulen mit runder Obergrenze; `Graph.Unit`)*
  - Kacheln: `widgets/chart` ohne Werte-Achse; Energie ohne Achsen; Mini-Balken (Glances, Speedtest, GitHub) ohne Werte-Achse; Sparklines ohne alles *(bleibt so: Regel gilt nur für Popups und Seiten)*
  - In Ordnung: Tagesbalken (Prozent, `title`), `hbars`, Wochen- und Tagesleiste
  - [x] Kante zuerst: Werte-Achse für gestreckte SVGs (`preserveAspectRatio="none"` verzerrt SVG-Text, also HTML-Achse daneben); Hover-Anzeige für Balken und `path`-Linien (heute nur `polyline` in `.chart-wrap[data-readout]`) *(Kante 1.15, Read-out jetzt delegiert, wirkt auch in nachgeladenen Dialogen)*
  - Geklärt 06.10.2026: Die Regel gilt nur für Popups und Seiten; Kacheln bleiben ohne Achsen und Legenden, damit sie auf einen Blick lesbar sind
- [x] Integrationen werden möglichst gegen eine reale Instanz getestet *(`internal/testkit/live`, `make live`; erster Test: Orte-Abgleich `places_live_test.go`)*:
  - [x] `.local-test/` in `.gitignore`
  - Verzeichnis außerhalb von Git (`.local-test/`): lokale Andon-Instanz; die Tests nehmen die Verbindungen aus `data/andon.db` (persönliche mit dem Login ihres ersten Inhabers) (Schlüssel `secrets/master_key`)
  - Schreiben nur mit neuen Testeinträgen (Name `andon-test …`); vorhandene Einträge nie schreibend anfassen (`live.Change` bricht ab)
  - Jeder schreibende Vorgang wird in `.local-test/writes.log` protokolliert (Zeit, Dienst, Aktion, Art, ID, Test)
  - In Cloud-Umgebungen und CI genügen Tests gegen Nachbauten; ohne Instanz wird der Live-Test übersprungen
  - [x] Live-Tests für die übrigen schreibenden Ausgänge (Kimai-Zeiten, Invoice Ninja, Paperless) *(`kimai_live_test.go` (Zeiten, Tags, Timer), `ninja_live_test.go` (Kunde, Entwurf, Zahlung, Ausgabe; hinterlässt Nummernlücken), `paperless_live_test.go` (Upload, Zusatzfeld); räumen am Ende auf. Erster Lauf 06.10.2026 grün. Laufen nur mit `ANDON_LIVE=1` (`make live`), nie in `make check`)*
    - Invoice Ninja schreibend nur auf ausdrückliche Aufforderung (`ANDON_LIVE_NINJA=1`), um so wenige Nummernlücken wie möglich zu erzeugen
  - [x] Alle Integrationen lesend gegen die lokale Instanz: `sources_live_test.go` holt Verbindungstest und Datensatz jeder Verbindung *(06.10.2026: 34 von 37 grün)*
    - [x] Gitea und Tailscale: HTTP 401. Ursache in Andon: `svcdata.Secret` gab bei OAuth-Anmeldungen den Platzhalter `grant:v1` statt des Tokens aus; Timer, Licht, Abrechnung, Mail-Weiterleitung und Kachel-Aktionen auf solchen Verbindungen scheiterten *(behoben)*
    - [x] Snipe-IT: `/api/v1/hardware` antwortet mit HTTP 500. Ursache im Snipe-IT-Log: Tabelle `asset_external_sources` fehlt, 16 Migrationen von v8.8.0 stehen aus (beim Start am 01.10. war die Datenbank noch nicht erreichbar). Auf regis.lan: Datenbank sichern, dann `docker exec snipeit_app php artisan migrate --force` *(Migration ausgeführt, live grün 06.10.2026)*
    - [x] KDE Store „shrippen“: Option `user` fehlte, der Datensatz blieb stumm leer. Jetzt Fehler „Nichts zu lesen“ ohne `user`/`ids`; lokal `user: shrippen` gesetzt
    - [x] Domains „arianw.de“: rdap.org kennt `.de` nicht (fehlt in IANAs RDAP-Liste), daher 404. `.de` fragt jetzt DENIC direkt; ein Ablaufdatum veröffentlicht DENIC nicht
  - [x] Weitere schreibende Ausgänge: Kimai-Export-Flag, Kunde umbenennen, Fahrten (`kimai_more_live_test.go`), Tandoor-Einkaufsliste (`tandoor_live_test.go`); `TestPlacesLive` löscht seine Dawarich-Area
    - [x] Kimai-Orte per API löschen: Anfahrten-Plugin `DELETE /api/mileage/places/{id}` (kimai-anfahrt #7); `TestPlacesLive` räumt den Ort auf, Rest Ort 44 gelöscht
    - Ohne Schreibtest: `HassToggle` und `DNSPause` (schalten bestehende Geräte bzw. den Filter, keine eigenen Testeinträge möglich), Grocy (keine Verbindung), Apprise, Mail, LLM

- [x] Einstellungen: möglichst viele bisher nur per Umgebungsvariable setzbare Einstellungen zusätzlich in den Servereinstellungen der Oberfläche anbieten; sind beide gesetzt, gewinnt die Umgebungsvariable (in der Oberfläche als „durch Umgebung gesetzt“ gesperrt anzeigen) *(Admin → Einstellungen → Server: SMTP, Apprise, Anthropic-Schlüssel, Prüflauf-Intervall, Sitzungsdauern, Log-Stufe; Geheimnisse verschlüsselt, wirken ohne Neustart. Nur Umgebung: `BASE_URL`, `MASTER_KEY`, Pfade, `TRUSTED_PROXIES`, `SCHEDULER_ENABLED`, Demo/Dev. `LOG_LEVEL` wurde vorher gar nicht ausgewertet)*
  - [x] OIDC: heute gewinnt die gespeicherte Konfiguration über `OIDC_*`; auf „Umgebung gewinnt, Feld gesperrt“ umstellen *(je Feld; Speichern lässt gespeicherte Werte gesperrter Felder unberührt)*
- [x] Wartungsseite: laufende Aufgaben, zuletzt abgeschlossene Aufgaben, Probleme, Logs *(Admin → Betrieb: Jobs und Fortschritt langer Arbeiten (`internal/progress`), Verlauf der letzten 50 Läufe, Verbindungen mit Fehlern heute, die letzten 300 Logeinträge (`internal/logbuf`))*
- [x] „Über Andon“: Dev-Builds zeigen keine Version. Auch Dev-Builds bekommen automatisch eine Versionsnummer nach dem Schema `Version/Branch/Build`, z. B. `0.5.0/main/#25` *(`scripts/version.sh`; Build = Commits seit dem letzten Tag, beginnt nach jedem Release neu)*
- [x] Regel (in `agent.md` aufnehmen): Visualisierungen in Popups bekommen, wo möglich, Tooltips beim Hover (ergänzt die Regel zu Achsen und Legenden) *(Umsetzung beim Nachrüsten oben)*
- [x] Dawarich: Orte *(Fähigkeiten je Integration, Verbünde und Kunden-Zuordnung: [`CAPABILITIES.md`](CAPABILITIES.md), Abschnitt „Umsetzung“)*
  - Kimai „Anfahrten“ ist nur für Geschäftliches zuständig und kennt Kunde, Zuhause, Arbeitsplatz, Sonstiges, aber keine privaten Orte (so gewollt)
  - Andon kennt zusätzlich private Orte; vorerst reicht die Kategorie „Privat“
  - Struktur: jedes Backend unterstützt nur eine Teilmenge der Funktionen beim Abgleichen und Schreiben (Fähigkeiten je Integration)
  - Prüfen, ob sich das abstrahieren lässt, damit weitere Integrationen es nutzen: welche Fähigkeiten eine Integration hat, wie sie sich mit anderen überlappt (Dawarich und Kimai Anfahrten kennen beide „Orte“, unterschiedlich und voneinander abhängig) und wie Integrationen voneinander abhängen *(Entwurf: [`CAPABILITIES.md`](CAPABILITIES.md), offene Fragen dort)*
- [x] Dawarich: Der Hinweis „Dawarich-Tracks werden noch gelesen; ältere Fahrten fehlen vorerst.“ bekommt eine Fortschrittsanzeige und einen Link zur Wartungsseite *(Fahrten-Dialog: „300 / 1200 · Tracks gelesen“, Link nur für Admins; dieselbe Aufgabe unter Admin → Betrieb)*

---

### QA-Runde 07.10.2026

Fünf Bereiche als User Journeys im Browser durchgespielt (Erster Start, Alltag, Geld und Zeit, Boards bauen, Verwaltung), gegen die Demo und eine leere Instanz. Die Journeys stehen fest in [`QA.md`](QA.md) für die nächsten Runden. Rechte-Lecks: keine gefunden (fremde Boards, Verbindungen, Admin-Seiten direkt aufgerufen und per POST: abgelehnt).

**Behoben (mit Test)**
- [x] Teilen ging an den Falschen: Benutzer- und Team-IDs überschneiden sich, Art und Name kamen aus zwei Feldern („Team Produktion“ ging an Lena). Ein Feld `team:1`
- [x] Verlauf und Freigaben überlebten das Löschen; SQLite vergibt die ID neu, ein neues Board zeigte fremden Verlauf („Wiederherstellen“ überschrieb es), ein neues Team oder Benutzer hätte alte Freigaben geerbt. Trigger löschen mit, Waisen entfernt (Migration 0023)
- [x] Abrechnung bot keinen Entwurf an („Kein Invoice-Ninja-Kunde mit diesem Namen“): der Entwurf nahm den Hash-Schlüssel des Kunden, der ohne Hash leer ist; jetzt `Ref()`
- [x] Adresse einer Verbindung mit gemeinsamem Token ließ sich nicht ändern (Formular verlangte den Zugang, hatte aber kein Feld); Feld „Neue Adresse? Zugang“
- [x] Timer stoppen/starten/wechseln: Ablehnung durch Kimai war ein stummer 500; jetzt Meldung auf der Kachel
- [x] Schnell-Link „not a url“: 500 und leeres Feld; jetzt Meldung
- [x] Einladung an „not-an-email“ angenommen; zwei offene Einladungen an dieselbe Adresse; jetzt geprüft, die neue ersetzt die alte
- [x] TOTP gesperrt nach fünf Fehlversuchen meldete „Ungültiger Code“ auch für den richtigen; jetzt „Zu viele Versuche“
- [x] Hinweise-Dialog zählte nur die gelisteten Hinweise als „offen“ (8 statt 163)
- [x] Hinweissuche traf Schaltflächentexte („pausieren“ fand alle 161)
- [x] Wandanzeige zeigte das Einführungsbanner, das dort nicht zu schließen ist
- [x] „1 Rechnungen“: Singular über `<key>_singular` im Katalog (für jeden Text mit `{count}` nutzbar)
- [x] Englische Texte: „Don''t verify“ mit sichtbarem Doppel-Apostroph, „fine again since“ ohne Zeit, „connections place“

**Offen: Fehler**
- [x] Hinweise: „Erledigt“ quittiert (ack), „Erledigt (7 Tage)“ zeigt nur gelöste; kein „Wieder öffnen“ (Route `/hints/{id}/reopen` gibt es). Jede Aktion springt an den Seitenanfang *(erledigte Hinweise kommen in „Erledigt (7 Tage)“ mit „Wieder öffnen“; nach einer Aktion geht es zum nächsten Hinweis)*
- [x] Rückgängig wechselt nur zwischen den zwei neuesten Fassungen hin und her (`boards/extras.go`); Kachel auf zwei Boards löschen: keine Anzahl, Rückgängig wirkungslos *(Rückgängig geht Schritt für Schritt zurück; Löschen nennt die Zahl der Boards und legt je Board eine Fassung mit Kopie der Kachel an, Rückgängig holt sie zurück)*
- [x] Code-Ansicht: „Ersetzen“ ohne Änderung nummeriert Boards und Kacheln neu (Lesezeichen zeigen auf anderes); „Zusammenführen“ mit dem vollen Text verdoppelt alles; beide Modi unerklärt (`porting.go`) *(Import gleicht Boards nach Slug und Kacheln nach Schlüssel ab und ändert sie an Ort und Stelle; „Ersetzen“ entfernt nur, was fehlt)*
- [x] Board-Export enthält keine Kacheln, Import in einen anderen Bereich verliert sie *(Export enthält die Kacheln des Boards)*
- [x] Uhr: 24 h gewählt, Board zeigt 12 h in Englisch (`andon.js`, `hour12` nur bei 12 h gesetzt) *(`hourCycle: h23`, im Browser geprüft)*
- [x] Board-Namen ohne Längengrenze (300 Zeichen machen jede Seite 4400 px breit); doppelte Namen ohne Unterscheidung *(höchstens 80 Zeichen; ein neuer Name, den es im Bereich schon gibt, bekommt eine Nummer, Umbenennen darauf wird abgelehnt)*
- [x] Fehlerseiten ohne App-Rahmen: 404, 405 (Neuladen nach POST: Einladung, Wiederherstellungscodes), 409 (veraltete Fassung, Eingabe weg), 403 „forbidden“ *(Seitenaufrufe bekommen eine Fehlerseite im App-Rahmen mit „Zurück“, das die Eingabe behält; GET auf POST-Seiten leitet zur Seite des Formulars. 409 füllt das Formular nicht neu, „Zurück“ hat die Eingabe)*
- [x] Theme: ungültige Farbe zeigt `theme.bad_value:--fg0`, Wert verworfen *(Meldung nennt das Token und was erlaubt ist, die Eingaben bleiben)*
- [x] Galerie-Vorschau Kalender: „ERROR dns:“ statt Beispieldaten *(Beispieltermine aus der Demo-Welt)*
- [x] TOTP: kein QR-Code (nur Geheimnis und URI) unter „Code scannen“; ein falscher Bestätigungscode verwirft die Einrichtung (neu scannen) *(QR-Code; ein falscher Code zeigt dasselbe Geheimnis wieder)*
- [x] Neue Verbindung im persönlichen Bereich: Vorgabe „Vorlage: jeder meldet sich an“, Token-Feld versteckt, obwohl der Hinweis „persönlich = fester Zugang“ sagt *(im persönlichen Bereich fest vorbelegt, sonst Vorlage; folgt dem Wechsel des Bereichs)*
- [x] Teams: der letzte Owner lässt sich entfernen (auch man selbst) *(abgelehnt: „Ein Team braucht mindestens einen Owner“)*
- [x] `/admin/users/9999/reapply` → 500 statt 404 *(404)*
- [x] Kimai Lite: Hinzufügen-Formular verschwindet beim Auffrischen der Kachel (alle 59 s) mit dem Getippten; „Teilen“ im Tagesformular am Desktop von der Nachbarkachel verdeckt *(Auffrischen wartet, solange ein Formular offen ist oder etwas getippt wurde; Aktionen brechen um; im Browser geprüft)*
- [x] „Heute“ dreimal verschieden: Kimai-Kachel „0,0 h · Timer läuft“, Heute 04:12, Kimai Lite 5:12 *(Kimai-Kachel zählt den laufenden Timer; die Demo rechnet Kimai Lite aus Blöcken und Timer. Die Heute-Kachel zeigt den Beginn des Timers, keine Summe. Bleibt: die Demo erzählt zwei Timer, 11 h im Datensatz für `kimai.timer_running_long` und 47 min in Kimai Lite)*
- [x] Offene Rechnungen: Kachel „21 Tage überfällig“, Dialog „Überfällig –“ *(Ganzzahlen werden gelesen, `asF`)*
- [x] „An Paperless“: 400 „Kein persönlicher Zugang hinterlegt“ ohne Verbindung oder Weg dorthin, Neuladen sendet erneut *(Meldung nennt die Verbindung und verlinkt den Reiter Zugang; Antwort als Weiterleitung, Neuladen sendet nichts; Demo-Verbindungen sagen es)*
- [x] Fahrten: „Ort anlegen“ zeigt roh „dns: dawarich“; neue Ziele sind mit „Zuhause“ vorbelegt *(übersetzte Meldung mit Dienst; Demo sagt es; neue Orte starten „nicht zugeordnet“)*
- [x] Monatsabschluss-Link „Geschäftskonten benennen“ landete auf dem Reiter Startseite statt Regeln
- [x] Kalender-Feed: doppelte Termine (zwei Hinweise, dieselbe Frist), keine Beschreibung und kein Link zurück; jetzt ein Termin je Tag und Titel mit allen Gründen (`DESCRIPTION`) und Link (`URL`)
- [x] Audit: rohe Schlüssel `audit_action.settings.map`/`.server`, Speichern ohne Änderung schreibt Einträge „– → false“; Reset-Link nennt die Benutzer-ID statt der Adresse *(Texte für die Schlüssel; unveränderte Werte schreiben keine Einträge; der Reset-Link nennt die Adresse)*
- [x] Ungültiges CIDR zeigt den rohen Go-Fehler; Einladung meldet „gesendet“, obwohl SMTP nicht erreichbar ist *(Meldung mit Beispiel; die Seite sagt, ob die Einladung per Mail rausging, scheiterte oder kein SMTP da ist)*
- [x] `/teams/1` scrollt auf dem Handy seitlich *(Mitglieder-Auswahl begrenzt, 360 px geprüft)*

**Offen: Reibung und Unlogisches** (Entscheidung nötig)
- [x] Von der Verbindung zur Kachel auf dem Board: vier Schritte in drei Dialogen; Bibliothek ohne „auf Board legen“; leeres Board ohne Hinweis *(entschieden C: Bibliothek „Auf Board legen“ mit Board-Wahl; leeres Board sagt es, bietet die Galerie und je Verbindung ohne Kachel ihre Startkachel; nach grünem Test die passenden Vorlagen des Dienstes (`widgets.ForService`: eigene Typen, dann Querkacheln wie „Backup-Übersicht“), ein Klick legt sie aufs gewählte Board)*
- [x] 85 Dienste ohne Suche bei „Neue Verbindung“; Fehler der Verbindungstests roh und englisch („connection refused“, „egress denied“) ohne Hinweis auf Admin → Netzwerk; nach neuem Token kein automatischer Test *(entschieden A: Suche und Gruppen wie in der Galerie; bekannte Ursachen (abgelehnt, Netzwerk-Regel, 401, 403, TLS, DNS, Zeitüberschreitung) übersetzt mit Link, rohe Meldung darunter; neuer Token, neue Zugangsdaten oder eigener Zugang zu einer Vorlage testen sofort)*
- [x] Formulare verlieren Eingaben bei Fehlern (Setup, Einladung, Ruhezeiten); keine Erfolgsmeldung nach Setup, Passwort-Reset, Passwortwechsel *(entschieden A: ein Mechanismus für alle Formulare. Antwortet eine Seite auf ein abgelehntes Formular, füllt `web/forms.go` dessen Felder mit dem Getippten, nie Passwörter, Tokens, Schlüssel, Codes oder Felder mit `data-secret`; Erfolg nach einer Weiterleitung sagt ein einmaliger Toast (Flash) auf der nächsten Seite: Setup, Passwort-Reset, Passwortwechsel, Profil, Benachrichtigungen, Bereichs- und Admin-Einstellungen, Verbünde)*
- [x] „Passwort vergessen?“ ohne SMTP angeboten; Registrierung verrät vergebene Adressen (Reset verbirgt es) *(entschieden A: ohne SMTP heißt der Link „Admin um neues Passwort bitten“, die Seite erklärt den Reset-Link der Admins. Registrierung antwortet für neue und vergebene Adressen gleich (Weiterleitung zur Anmeldung mit Hinweis), legt nichts an und meldet nicht automatisch an; mit SMTP bekommt die vergebene Adresse die Mail „Du hast schon ein Konto“)*
- [x] Eigene Rolle als Auswahl mit Speichern angeboten; Betrachter sehen Instanz-Einstellungen und Test/Bearbeiten an allen Verbindungen mit aktiven Knöpfen; Ablehnung als englisches „access denied“ *(entschieden A, verbergen: Template-Funktion `can` prüft mit `access.Need`; Testen nur mit Verwaltungsrecht oder eigenem Zugang zur Vorlage, Bearbeiten und „Verbünde verwalten“ nur mit Recht; Instanz-Einstellungen nur für Admins (`spaces.OpenSettings`), Team-Editoren sehen die Team-Einstellungen ohne Speichern; eigene Rolle als Text. Ablehnung: übersetzte Fehlerseite im App-Rahmen mit Grund und Weg zurück, auch beim weichen Seitenwechsel (htmx-Boost); die Prüfungen der Services bleiben)*
- [x] Hinweisseite: rund 140 Gruppen mit je einem Hinweis, je eine Sammelleiste; 60 Dienst-Chips vor der Liste (Handy: erster Hinweis bei 1550 px) *(entschieden C nach Entwürfen: Filter-Seitenleiste mit Stufen und Diensten samt Zählern (Kante 1.19 `.filter-layout`), am Handy hinter „Filter (n)“; Umschalter „Gruppiert / Einzeln“ je Nutzer, Gruppen erst ab zwei Hinweisen, einzelne als Zeilen; erster Hinweis am Handy bei 385 px)*
- [x] Wandanzeige: viermal so hoch wie der Bildschirm, kein Durchlauf, kein Weg hinaus *(entschieden: kein Scrollen, sondern Sätze aus Kacheln, die genau einen Bildschirm füllen, alle 20 s der nächste mit einem Übergang aus Kante 1.20 (Überblenden, Kante-Schnitt, Kachel für Kachel, Fallblatt, Rollladen, Scanlinie, Abwechselnd) und wählbarer Beschleunigung; Zeit, Übergang und Beschleunigung in den Board-Einstellungen mit Vorschau. Esc, jede Taste oder Klick in eine Ecke führt zurück, Hinweis darauf beim Start. Kacheln der schmalen Seitenspalte sind an der Wand vollwertige Kacheln im Raster der Hauptspalte. Regel: braucht ein Board mehrere Sätze, deckt jeder mindestens ein Drittel des vollsten Satzes; ein zu leerer Satz nimmt Kacheln vom vorigen, dann vom nächsten, sonst verschmilzt er mit einem Nachbarn, notfalls auf 70 % verkleinert)*
- [x] Zeitzone: Kimai Lite folgt der Server-Zeit (`TZ`), andere Anzeigen dem Browser; ohne `TZ` (Entwicklung) UTC *(entschieden: Kimai Lite bleibt in der Server-Zeit und nennt die Zone, wenn der Browser in einer anderen ist)*
- [x] Hosts: alle 66 mit „Monitore 0 / Probleme –“, Hinweise zählen nicht mit *(entschieden A: Hinweise gehören zum Host ihres Gegenstands (Host, Knoten, Gast, Gerät), sonst zu dem ihrer Verbindung, und zählen als Probleme; Hosts ohne Monitor und Hinweis unter „N ohne Befund“ eingeklappt)*
- [x] Abrechnung: offener Entwurf beim selben Kunden nicht erwähnt (doppelter Entwurf möglich); Hinweis „nicht abgerechnet“ führt zu Kimai statt zu `/billing#drafts` *(entschieden A: „Offener Entwurf R-… vom …“ mit Link vor „Entwurf anlegen“, Anlegen bleibt; der Hinweis führt zu Abrechnung → Entwürfe)*
- [x] Belege: Zähler der Reiter weichen ab (6 gegen 3); Erfolg „Verknüpft: 0“ neben der Demo-Ablehnung; Reiter „Belege zuerst“ geht nach „Anlegen“ verloren; Ausgaben aus dem Export fehlen in der Suche *(entschieden A, als Fehler behoben: „6 Belege ohne Ausgabe“ zählte alle Belege, der Reiter nur die markierten; Zähler, Jahreschips und Listen kommen jetzt aus denselben Filtern. Erfolg nur ab einer Verknüpfung. Der Reiter steht in der URL aller Suchen und Formulare. Export und Suche lesen dieselben Ausgaben, ohne gelöschte; die Demo hatte zwei verschiedene Listen)*
- [x] Zeiträume ohne Angabe: Umsatz je Kunde (12 Monate) gegen Kundenseite (Jahr); Vorjahresvergleich über unvollständige Historie; Abos mischen Jahres- und Monatsbeträge; Kilometerbetrag passt nicht zur genannten Formel *(entschieden B: Kunden- und Abrechnungsseite mit „12 Monate / Jahr / Vorjahr“ (`?period=`, Standard 12 Monate), jede Kennzahl nennt Zeitraum oder „Stand heute“; Vorjahresvergleich nur bei voller Historie, sonst „Daten ab …“; Abo-Zeilen als Monatsbetrag; Kilometergeld nennt die bezahlten km (Auto, Motorrad), Rad zählt beruflich, aber ohne Pauschale)*
- [x] Sprache: Bereich „Instanz“ und „Verbünde“ im englischen UI; Sprachumschalter nur im Benutzermenü; rohe Regel-IDs in Hinweislisten der Dialoge; Kennzahl-Dialoge heißen alle „Kennzahl“ *(englisch „Instance“ und „Bundle“; Dialoge nennen den Titel der Regel; Kennzahl-Dialoge heißen wie ihre Kennzahl; Umschalter DE/EN im Fuß jeder Seite, auch auf der Anmeldung: abgemeldet im Cookie, angemeldet im Profil. Tests: kein Deutsch in `en.yml`, Text in beiden Sprachen)*
- [x] Handy: Navigation drei Zeilen (≈ 215–290 px) vor dem Inhalt; Beträge in Abrechnungs- und Kundentabellen außerhalb des Bildschirms *(entschieden nach Entwürfen: Schublade aus A (Kante 1.19 `.nav-drawer`, Leiste 54 px), Tabellen als Karten aus B (`table.cards-sm`, Betrag oben rechts))*
- [x] Inline-Style-CSP-Meldungen in der Konsole nach htmx-Tausch (vermutlich `attributesToSettle` mit `style`) *(bestätigt: Kontextmenü am Board, danach Seitenwechsel; htmx setzt nur noch class, width, height. Ein Test durchläuft die Demo-Welt und scheitert an jedem `style=`; Playwright: 1 Meldung vorher, 0 nachher)*
- [x] Kante: Suchfeld des Boards ohne sichtbaren Fokusring *(Kante 1.18: `.input`/`.select` mit `--focus`-Ring bei `:focus-visible`)*

**Rest der Reibung (nach dem Durchgang)**
- [x] Formulare mit `?error=`-Weiterleitung verloren noch Eingaben: Verbund-Kunden (Zelle), Verbund „Kunden“ (Anlegen), Mitglieder, Löschen; Jahrespaket; neue Ausgabe aus einem Beleg; Feldzuordnung der Belege *(antworten mit der Seite samt Fehler und Eingabe über `web/forms.go`; Get-Formulare (Jahrespaket) ebenso. Belege: die Formulare im Reiter antworten an Ort und Stelle (htmx). Bleiben als Weiterleitung, weil nichts getippt wird und Neuladen nicht erneut senden soll: Belege verknüpfen, lösen, ignorieren, Mail an Paperless, Anmelde-Abläufe (OAuth))*
- [x] Veraltete Fassung (409) in Board-Formularen füllte das Formular nicht neu *(Abschnitt anlegen: Board im Bearbeiten-Modus mit 409, Titel und Meldung; Abschnitt bearbeiten und Schnell-Link (htmx): der Abschnitt im neuen Stand, sein Formular offen mit dem Getippten, Meldung „neuer Stand mit deiner Eingabe“; Kachel bearbeiten ebenso. Die Fassung im Formular ist die neue, erneutes Speichern geht. Offen: Board-Einstellungen (Arbeit an der Wandanzeige läuft dort) und eine neue Kachel, deren Platzierung veraltet ist (Kachel ist angelegt, Fehlerseite))*
- [x] Demo erzählte zwei Timer (11 h im Kimai-Datensatz für `kimai.timer_running_long`, 47 min in Kimai Lite) *(ein Timer: der Datensatz übernimmt Timer und Blöcke von Kimai Lite, „heute“ ist in Kimai-Kachel, Heute und Kimai Lite gleich. Der Hinweis „Timer läuft lange“ fehlt dafür in der Demo: ein Timer über Nacht widerspricht den Blöcken des Tages. `running_hours`, `running_id`, `running_customer` sind in der Demo-Welt ungenutzt (Quelle: shrippen.github.io `demo/world/business.json`))*
- [x] „Ø Vorjahre“ (Saison-Diagramm) mittelte auch Jahre ohne Daten (Monate vor der ersten Rechnung zählten als 0, je Monat andere Jahre) *(jeder Monat mittelt dieselben Jahre, nur solche mit vollständiger Historie; reicht sie nicht drei Jahre zurück: „Daten ab …“ in Legende und Dialog, keine Veränderung)*
- [x] Sure-Abos mit Abstand unter 25 oder über 370 Tagen standen mit ihrem Rohbetrag in der Abo-Kachel (wöchentlich 10 € als 10 € im Monat) *(jeder Abstand wird zum Monatsbetrag: Tage ×365/12, Wochen ×52/12, Monate geteilt durch ihre Zahl; monatlich, vierteljährlich und jährlich jetzt genau statt über 30 Tage)*
- [x] Hinweise „Gruppiert“: Hinweise in einer Regel-Gruppe waren noch volle Karten, einzelne kompakte Zeilen *(alle als Zeilen (`.hint-card.is-row`) unter Kopf und Sammelleiste der Gruppe; Grund, Öffnen, Pausieren, Notiz und Details hinter „⋯“. Eingebettet (`/api/…/hints`) nur mit Öffnen-Link, ohne Formulare)*
- [x] `/clients` am Handy: Umsatz oben rechts auf der Karte ohne Namen *(Kante 1.21: die Kennzahl einer Tabellenkarte (`data-card="key"`) zeigt ihr `data-label` klein über dem Wert; gilt auch für die Beträge der Abrechnung)*

### Notiert 07.10.2026 (noch nicht begonnen)

- [x] KDE-Store-Kachel testen; in den Einstellungen erklären, was in welches Feld gehört *(live gegen api.kde-look.org geprüft, Benutzer shrippen: 3 Einträge. Eigene Felder „Benutzer“ und „Einzelne Einträge“ mit Erklärung, Links werden zu Nummern; store.kde.org als URL wird erklärt statt HTTP 410; doppelt gelesene Einträge behalten die höhere Zahl, die Einzelabfrage hinkt nach)*
- [x] Klären, warum es die Umgebungsvariable `APPRISE_API_URL` gibt, obwohl jeder Nutzer eigene Benachrichtigungs-Einstellungen hat *(sie nennt den Apprise-API-Server, der versendet, wie `SMTP_URL` für Mail; die Kanäle der Nutzer sind nur Ziele. Auch unter Einrichten → Server setzbar)*
- [x] Fehler: Ohne `APPRISE_API_URL` scheitert jeder Push, die Hinweise gelten trotzdem als gesendet (`dispatchUser` setzt `TouchSent` unabhängig vom Ergebnis) und kommen nie mehr. Außerdem sagt die Seite der Benachrichtigungen nicht, dass der Server fehlt *(gesendet gilt nur, was angekommen ist; ohne Server versucht Andon nichts, die Seite warnt und verlinkt Admins zu Einrichten → Server; Erklärung am Feld)*
- [x] Testmöglichkeit für jeden Benachrichtigungsweg (Mail, Apprise) *(Apprise: „Testen“ je Kanal, gab es schon; Mail: „Jetzt an mich senden“ schickt die Zusammenfassung sofort)*
- [x] Einstellungen für E-Mails: wann, welcher Inhalt, Vorschau, Log *(eigene Karte „E-Mail-Zusammenfassung“: Zeit und Wochentag; ab Stufe, Steuerfristen, leere Mails; Vorschau ohne KI; die letzten 10 Sendungen mit Ergebnis. Versand wartet jetzt auf die SMTP-Antwort, Fehler stehen im Log)*
- [x] Untersuchen, wie das Verknüpfen von Kunden grundsätzlich gedacht ist, je einmal mit und ohne Verbund (Anlass: nur eine Verbindung je Typ, auf der Kundenseite ist dazu nichts zu sehen) *(gedacht laut `CAPABILITIES.md`: mit Verbund unter Bereich → Verbünde → „Kunden“; ohne Verbund bildet der Bereich einen impliziten, und die erste bestätigte Zuordnung soll ihn anlegen. Umgesetzt ist nur der Fall mit Verbund: die Kundenseite `/verbund/{id}/customers` braucht einen gespeicherten Verbund, die Seite der Verbünde listet nur gespeicherte; bis dahin gilt der Namensabgleich)*
- [x] Kunden ohne Verbund zuordnen: impliziten Verbund auf der Seite der Verbünde zeigen, mit „Kunden“; erste Bestätigung legt ihn an *(gespeichert wird er schon beim Klick auf „Kunden“, nicht erst bei der ersten Bestätigung: Die Zuordnungsseite braucht einen gespeicherten Verbund)*
- [x] Kundenseite (`/clients`): Stand der Zuordnung zeigen (bestätigt oder nur gleicher Name) und zur Zuordnung verlinken *(auf der Seite eines Kunden, Link nur mit Bearbeiten-Recht)*
- [x] Diagramme stärker beschriften: Bei Balkendiagrammen hat die Y-Achse je Linie eine Beschriftung, die X-Achse nur Anfang und Ende; das ist schwer lesbar. Der Hover nennt beide Achsen *(`Graph.Labels`: X-Wert je Punkt; fünf Marken statt zwei, Hover „Tag · Wert“ an Balken und Linien. Antwortzeit-Diagramm der Link-Details ebenso, fünf Tage)*
- [x] Designdokument: alle Konzepte, die Andon kennt (z. B. Kunden, Orte), und was Andon mit ihnen tut bzw. wofür es sie verwendet *([`CONCEPTS.md`](CONCEPTS.md))*
- [x] Recherche Kacheltypen *([`research/tile-types.md`](research/tile-types.md); zuerst empfohlen: Proxmox Backup Server mit `cross.vm_unbacked`, `cross.code_unbooked` (Wakapi × Kimai), `cross.image_cve`)*:
  - Dashys mögliche Kacheln durchgehen und auflisten, welche in Andon fehlen
  - Andere Dashboards und Andon-ähnliche Programme ansehen, daraus Vorschläge für weitere Kacheltypen
  - Aus den Kacheltypen Vorschläge für Verbindungen, Analysen und Queranalysen ableiten

### Noch nicht in echt getestet (notiert 07.10.2026)

Der letzte Live-Lauf (`make live`, 06.10.2026, 34 von 37 grün) kannte nur die Dienste bis #58. Alles danach ist nur gegen Nachbauten (`httptest`) und die Demowelt getestet. Abhaken, wenn `make live` gegen eine echte Instanz grün ist und die Hinweise plausibel sind; Funde wie beim Lauf vom 06.10. darunter notieren.

**Neue Dienste ohne Live-Lauf** (je Dienst eine Verbindung in der lokalen Instanz, sofern der Dienst läuft)
- [ ] Sicherung: Proxmox Backup Server (Token-Kopf `PBSAPIToken=`, Verify-Jobs, Belegung je Datastore), Kopia, Duplicati (Token-Anmeldung), Backrest, UrBackup (Anmeldung)
- [ ] Überwachung: Healthchecks (API v3), Prometheus (Alerts, PromQL-Kachel), NVD (mit und ohne Schlüssel; ohne nur 5 Abfragen in 30 s), Drone
- [ ] Updates: What's Up Docker, Watchtower (Metriken-Endpunkt mit Token)
- [ ] Strom: PeaNUT, apcupsd (NIS-Protokoll, Port 3551), OpenDTU, EVCC
- [ ] Netz: Traefik, Caddy (Admin-API), Nginx Proxy Manager, Technitium, FRITZ!Box (TR-064 mit Digest-Anmeldung)
- [ ] Medien: Tautulli, Jellystat, Navidrome (Subsonic-Anmeldung), Audiobookshelf, Jellyseerr/Overseerr
- [ ] Aufgaben und Finanzen: Vikunja, Firefly III, Ghostfolio
- [ ] Lesen: Hacker News (Algolia-API), Lobsters, Reddit (sperrt Abrufe ohne Anmeldung zunehmend; prüfen, ob `hot.json` mit Andons User-Agent antwortet), YouTube-Kanal-Feeds; Twitch (App-Token, `helix/streams`)
- [ ] ESPHome: Basic Auth am Dashboard mit Passwort ist eine Annahme, ebenso die Felder von `/devices` und `/ping`; Home-Assistant-Add-on nur mit freigegebenem Port
- [ ] Fediverse: je einmal Mastodon, GoToSocial und Akkoma; Marker (`/api/v1/markers`) bei GoToSocial und Akkoma, Software aus der Versionszeile
- [ ] Lemmy (#99): Anmeldung, neue Anmeldung nach 401, ältere Instanz (Zeiten ohne Zone), Konto mit Zwei-Faktor gibt eine verständliche Meldung
- [ ] Hansei: Webhook-Stand und `/api/docs` gegen die echte Hansei-Instanz (dort Phase 05 offen); produktiv noch nicht eingerichtet (siehe Phase 15)

**Geänderte Quellen bekannter Dienste** (Live-Lauf vom 06.10. lief vor der Änderung)
- [ ] UniFi: Clients (für `cross.device_uninventoried`)
- [ ] GitHub: Option `owner` (eigene gegen beobachtete Repos), Releases beobachteter Repos, CI des Standardzweigs; Trends über die Such-API ohne Token
- [ ] Gitea: Actions-Status für die CI-Kachel
- [ ] Jellyfin/Plex: laufende Streams für „Jetzt läuft“, Update-Fenster und Belegungsstunden
- [ ] Borg, PG Back Web, TrueNAS über die gemeinsame Sicherungs-Schnittstelle (`sources.BackupSource`)
- [ ] Sure: Zahlungsabgleich über `metrics.BankOf` (Sure oder Firefly)
- [ ] Sport (OpenLigaDB)
- [ ] Kunden-Zuordnung über Sure (Zahler) und Paperless (Korrespondenten): Zuordnen, Lösen, „Namen angleichen“ (schreibt in die Dienste) mit echten Daten

**Querregeln mit echten Daten** (nur mit der Demowelt geprüft: stimmen die Treffer, gibt es Fehlalarme?)
- [ ] `cross.heartbeat_backup`, `cross.image_cve` (Image-Tag gegen CPE-Bereiche), `cross.release_red_ci`, `cross.pbs_orphan`, `cross.update_unbacked`, `cross.release_newer`
- [ ] `cross.charge_expensive`, `cross.route_undocumented`, `cross.line_vs_speed`, `cross.device_uninventoried`, `cross.requests_arr`
- [ ] `cross.task_unbooked` (Projekt über Vikunja-Projekt oder Label), `cross.depot_reserve`
- [ ] `cross.project_mentioned`, `cross.esphome_ha` (Entitäten über den Knotennamen), `cross.release_unannounced`
- [ ] `system.outage` mit USV auf Batterie und kritischen Prometheus-Alerts

**Ohne Schreibtest** (unverändert seit dem Lauf vom 06.10.): `HassToggle`, `DNSPause`, Grocy, Apprise, Mail, LLM

**Webseite**
- [ ] Changelog auf shrippen.github.io/andon nach dem Merge ansehen (#100; Kante 1.17 muss vorher unter `/v1` liegen)

## 11. Betrieb und Sicherheit

- **Anmeldung:** Eigene Anmeldung (Abschnitt 4.6). Der Reverse Proxy terminiert nur TLS; das Dashboard setzt `Secure`-Cookies und erwartet HTTPS (`BASE_URL`).
- **Zugangsdaten der Dienste:** Werden im Editor eingegeben und mit AES-GCM verschlüsselt in der Datenbank gespeichert. Der Hauptschlüssel kommt aus einem Docker Secret; ohne ihn sind Datenbank-Backups für Tokens wertlos. Schlüsselwechsel per Befehl `andon rotate-key`. Empfohlen: pro Dienst ein eigener Benutzer mit Leserechten.
- **Trennung:** Jede Abfrage ist auf erlaubte Bereiche beschränkt; Tests prüfen für jede Route, dass fremde Bereiche nicht erreichbar sind. Instanz-Admins verwalten Konten, sehen aber keine persönlichen Inhalte.
- **Netz:** Ausgehende Verbindungen nur zu den konfigurierten Diensten, Feeds, Statuszielen und Icon-Quellen. Keine externen CDNs zur Laufzeit; Icons werden einmal geholt und lokal zwischengespeichert. Weil Benutzer selbst URLs für Statusprüfungen, Feeds und Verbindungen eintragen, kann ein Instanz-Admin festlegen, welche Netze und Hosts erreichbar sein dürfen (Positivliste). Das verhindert, dass eingeladene Benutzer das Dashboard als Scanner für das interne Netz missbrauchen.
- **Daten:** Datenbank und Icons unter `/data`, Aufbewahrung konfigurierbar (z. B. Snapshots 24 Monate, Dawarich-Aggregate 12 Monate, Audit-Log 12 Monate). Löschen eines Benutzers löscht seinen persönlichen Bereich vollständig.
- **Backup:** `andon backup` erzeugt ein konsistentes Abbild (SQLite-Backup-API) plus Icons und Themes.
- **Robustheit:** Ein ausgefallener Dienst lässt das Dashboard nicht ausfallen. Das Widget zeigt den letzten Stand mit Alter und `.pill[data-state="failed"]`, dazu ein Hinweis `system.connector_down` im Bereich der Verbindung.
- **Beobachtbarkeit:** `/healthz`, strukturierte Logs, optional `/metrics` (Prometheus, nur mit Token).

**Beispiel `docker-compose.yml`**

```yaml
services:
  andon:
    image: ghcr.io/shrippen/andon:latest
    restart: unless-stopped
    volumes:
      - ./data:/data
      # optional: - ./seed.yml:/app/seed.yml:ro
    environment:
      TZ: Europe/Berlin
      BASE_URL: https://andon.example.lan
      SMTP_URL: smtp://andon@mail.example.lan:587?starttls=true
      SMTP_FROM: "Andon <andon@example.lan>"
      # optional beim ersten Start, sonst in den Admin-Einstellungen:
      OIDC_ISSUER: https://auth.example.lan/application/o/andon/
      OIDC_CLIENT_ID: andon
    secrets: [master_key, smtp_password, oidc_client_secret]
    ports: ["8080:8080"]

secrets:
  master_key:         { file: ./secrets/master_key }   # z. B. `openssl rand -base64 32`
  smtp_password:      { file: ./secrets/smtp_password }
  oidc_client_secret: { file: ./secrets/oidc_client_secret }
```

**Beispiel YAML-Export eines Bereichs (Ausschnitt)**

Dasselbe Format dient für Export, Import, Code-Ansicht und `seed.yml`. Zugangsdaten werden nie exportiert.

```yaml
space: personal:alex
settings:
  locale: de             # de | en; Standard für neue Benutzer im Bereich
  search: { engine: "https://searx.example.lan/search?q={query}" }
  goals: { revenue_year: 90000, billable_ratio: 0.7, hours_week_max: 45 }
  tax:
    vat: { method: ist, return_interval: monthly, extension: true }   # ist | soll; monthly | quarterly
    prepayments: { amount: 1200 }

connections:
  - id: kimai
    type: kimai
    url: https://kimai.example.lan
    credentials: personal          # jeder Benutzer hinterlegt sein eigenes Token
  - id: dawarich
    type: dawarich
    url: https://dawarich.example.lan
    credentials: shared            # Token wird im Editor eingegeben, nicht hier
    areas:
      "Muster GmbH Büro": { kimai_customer: 12 }
      "Home": { home: true }
    km_rate: 0.30
    per_diem: { over_8h: 14, full_day: 28 }

widgets:
  - id: kimai-link
    type: link
    title: Kimai
    url: https://kimai.example.lan
    icon: hl-kimai
    status: http
    info: { connection: kimai, metric: today }
    hotkey: 1
  - id: heise
    type: rss
    url: https://www.heise.de/rss/heise-atom.xml
    limit: 8
    refresh: 30m
  - id: open-invoices
    type: table
    connection: invoiceninja
    query: open_invoices

rules:
  kimai.unbilled_hours: { warn_days: 30, critical_days: 60 }
  in.invoice_overdue:   { dunning_after_days: 14 }

boards:
  - name: Start
    theme: null                    # persönliche Wahl bzw. Standard
    sections:
      - title: Freelance
        cols: 3
        widgets: [kimai-link, team:it/snipeit-link]   # Verweis auf ein Team-Widget
      - title: News
        widgets: [heise]

shares:
  - { resource: widget:open-invoices, to: team:buero, right: view }
```

---

## 12. Vorgeschlagene Repo-Struktur

```
dashboard/
├── ROADMAP.md
├── README.md
├── Dockerfile
├── docker-compose.example.yml
├── seed.example.yml
├── pyproject.toml
├── alembic/                 ← Datenbank-Migrationen
├── app/
│   ├── main.py              ← FastAPI, Routen, Scheduler-Start
│   ├── settings.py          ← Betriebswerte aus Env/Secrets
│   ├── db/                  ← SQLAlchemy-Modelle, Sitzung, Revisionen
│   ├── auth/                ← Konten, Passwörter, Sitzungen, CSRF, TOTP, Tokens, Einrichtungscode, oidc.py (authentik)
│   ├── mail/                ← SMTP-Versand, Vorlagen (Einladung, Reset, Sicherheit, Digest)
│   ├── access/              ← Bereiche, Rollen, Freigaben, zentrale Prüfung (`can(user, right, resource)`)
│   ├── crypto.py            ← Verschlüsselung der Zugangsdaten
│   ├── sources/             ← base.py, kimai.py, invoiceninja.py, snipeit.py, dawarich.py,
│   │                          rss.py, http_status.py, open_meteo.py, glances.py, public_ip.py
│   ├── widgets/             ← base.py, link.py, rss.py, clock.py, weather.py, iframe.py,
│   │                          sysinfo.py, kpi.py, hints.py, table.py …
│   ├── editor/              ← Board-Editor, Formulare aus Schema, YAML-Import/-Export, Dashy-Import
│   ├── themes/              ← Theme-Vertrag, Laden, Prüfen, Import/Export
│   ├── icons.py             ← Icon-Auflösung (favicon, si-, hl-, URL, Upload), SVG-Bereinigung, Cache
│   ├── metrics/             ← Kennzahlen je Bereich
│   ├── rules/               ← Regeln je Dienst + cross.py (dienstübergreifend) + deadlines.py
│   ├── notify/              ← Apprise, Digest-Vorlagen
│   ├── i18n/                ← de/ und en/ (.po/.mo), Babel-Konfiguration
│   ├── cli.py               ← import-dashy, backup, rotate-key, create-admin
│   ├── templates/           ← Jinja-Seiten und Partials (HTMX)
│   └── static/
│       ├── vendor/kante/    ← unveränderte Kopie: components.css, base.css, shrippen.js, Schriften + VERSION (tools/sync-design.sh)
│       ├── vendor/sortable/ ← SortableJS (vorgebaut)
│       ├── andon.js     ← Suche, Hotkeys, Uhr, Einklappen
│       ├── editor.js        ← Drag & Drop, Vorschau (nur im Bearbeitungsmodus geladen)
│       └── andon.css    ← nur neue Komponenten, ausschließlich mit Tokens
├── themes/
│   └── kante/               ← einziges mitgeliefertes Theme (theme.json, tokens.css)
├── tools/
│   └── sync-design.sh       ← kopiert Kante aus einem Checkout nach static/vendor/kante/ und erzeugt themes/builtin/kante/tokens.css
└── tests/
    ├── fixtures/            ← anonymisierte API-Antworten je Dienst, Dashy-conf.yml, RSS-Beispiele
    ├── access/              ← Rechte-Matrix, Bereichstrennung je Route
    ├── auth/                ← Anmeldung, Sitzungen, CSRF, Drosselung, OIDC gegen einen Test-Provider
    └── rules/               ← ein Test pro Regel
```

---

## 13. Entscheidungen

| Thema | Entscheidung |
|---|---|
| Benutzerzahl | 1–10 Benutzer → SQLite im WAL-Modus, kein PostgreSQL |
| E-Mail | Vorhandener SMTP-Server für Einladungen, Passwort-Reset, Sicherheitsmeldungen und Digest |
| Single Sign-on | authentik per OIDC als zusätzlicher Anmeldeweg in Phase 2, lokale Anmeldung als Notzugang |
| Kimai, Invoice Ninja | Unterstützt wird jeweils die aktuelle Version (Kimai 2, Invoice Ninja v5 self-hosted) |
| Kimai-Bundles | Holiday-Bundle über `/api/holiday/*`; Abrechnungsstatus über die Kimai-Kern-API |
| Benachrichtigungen | Alle Kanäle über Apprise, je Benutzer konfigurierbar |
| Steuerstatus | Freiberuflich, umsatzsteuerpflichtig (keine Kleinunternehmerregelung, keine Gewerbesteuer). Die Kleinunternehmer-Regel entfällt; dazu kommen Regeln zu USt-Zahllast, Vorsteuer und fehlender Umsatzsteuer |
| Sprache | Deutsch und Englisch, je Benutzer wählbar; Hinweise und E-Mails in der Sprache des Empfängers |
| IT-Doku | Andon erkennt Lücken zwischen Obsidian und den Compose-Repos, schreibt aber nie in den Vault; Änderungen macht Hansei nach Freigabe (Phase 15) |
| Visualisierung | Homelable als reine Ansicht auf Regis, von Andon befüllt; Obsidian bleibt die einzige Quelle, Homelables eigene Doku wird nicht genutzt; Live-Status an, keine Benachrichtigungen (Phase 15) |
| authentik-Gruppen | Bestimmen beim automatischen Anlegen eines Kontos die Start-Rolle und Start-Teams; danach werden Rollen und Teams nur im Dashboard gepflegt, kein Abgleich bei späteren Anmeldungen |

## 14. Offene Fragen

Keine. Geklärt am 06.10.2026:

- **Dashy:** erledigt (Dashy-Abgleich vom 25.09.2026).
- **Umsatzsteuer:** Soll-Versteuerung, Voranmeldung quartalsweise mit Dauerfristverlängerung (in den Bereichseinstellungen einzutragen).
- **Dawarich:** Abgleich mit Kimai gewünscht; Areas noch anzulegen, Zuordnung über das Plugin Anfahrten (Abschnitt 8.4).
