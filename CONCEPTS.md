# Andon: Konzepte

Was Andon kennt und wofür es das verwendet. Begriffe zu Fähigkeiten und Verbünden im Detail: [`CAPABILITIES.md`](CAPABILITIES.md); Plan und Entscheidungen: [`ROADMAP.md`](ROADMAP.md).

```
            Benutzer ── Team
                │        │
                ▼        ▼
             Bereich (persönlich │ Team │ Instanz)
   ┌────────────┼──────────────────────────┐
   ▼            ▼                          ▼
Verbindung   Kachel ──► Board          Einstellungen
   │  (Dienst, Zugang)     (Abschnitte)    (Fristen, Ziele, Regeln)
   │
   ▼  Prüflauf (alle ANALYSIS_MINUTES)
Datensatz ──► Kennzahlen ──► Kacheln, Seiten
   │
   └────────► Regeln ──► Befund ──► Hinweis ──► Push, Zusammenfassung, iCal, API
                 ▲
     Verbund: welche Verbindungen zusammengehören, Zuordnungen zwischen ihnen
```

## Ordnung: wer was sieht

| Konzept | Was es ist | Wofür Andon es nutzt |
|---|---|---|
| **Instanz** | die Andon-Installation | Server-Einstellungen (SMTP, Apprise, Prüflauf), Instanz-Bereich, Anmeldung (lokal, Passkey, authentik) |
| **Benutzer** | ein Konto, Rolle Admin oder Benutzer | eigene Boards, Einstellungen, Benachrichtigungen; Admins sehen keine persönlichen Bereiche anderer |
| **Team** | Benutzer mit Rolle Owner, Editor, Viewer | einen gemeinsamen Bereich pflegen |
| **Bereich** | persönlich, Team oder Instanz (englische Oberfläche: Personal, Team, Instance) | Behälter für Verbindungen, Kacheln, Boards, Einstellungen; Grenze für Daten in Hinweisen |
| **Recht** | view, use, edit, manage auf eine Ressource | Freigaben über Bereichsgrenzen; was nicht sichtbar ist, wird nicht ausgeliefert |
| **Freigabe** | Ressource → Benutzer oder Team mit Recht | Boards und Kacheln teilen |
| **Overlay** | persönliche Layout-Änderung an einem fremden Board | Reihenfolge, Größe, Ausblenden ohne das Board zu ändern |
| **Token** | API-Zugang mit Umfang | `/api/hints`, `/api/docs`, iCal-Feed, Einbettung |

## Startseite

| Konzept | Was es ist | Wofür Andon es nutzt |
|---|---|---|
| **Board** | eine Seite aus Abschnitten | Startseite und Auswertungsseiten; Vorlagen zum Übernehmen |
| **Abschnitt** | Gruppe auf einem Board | Links und Kacheln ordnen, einklappen, mit Icon |
| **Kachel** (Widget) | Typ + Einstellungen + Verbindung, in der Bibliothek | einmal einrichten, auf vielen Boards platzieren; Popup mit Details |
| **Platzierung** | Verweis eines Abschnitts auf eine Kachel | dieselbe Kachel an mehreren Stellen |
| **Link** | Startseiten-Eintrag mit Adresse | Dashy-Ersatz; Erreichbarkeit und Antwortzeit je Tag (Link-Details) |
| **Revision** | gespeicherter Stand eines Boards | Zurückgehen, YAML-/Dashy-Import |
| **Theme** | Farben und Schriften auf Kante-Rollen | Aussehen je Benutzer, Bereich oder Instanz; WCAG-Prüfung |

## Daten

| Konzept | Was es ist | Wofür Andon es nutzt |
|---|---|---|
| **Dienst** | ein unterstütztes Programm (Kimai, Invoice Ninja, Docker …) | bestimmt Adapter, Regeln, Kacheltypen |
| **Verbindung** | Adresse + verschlüsselter Zugang zu einem Dienst, in einem Bereich | alles, was Andon liest oder schreibt, geht über sie |
| **fest / Vorlage** | ein Zugang für alle oder einer je Person | geteilte Daten oder jedem seine eigenen; eine Vorlage braucht eine Aktivierung |
| **Datensatz** | was eine Quelle von einer Verbindung holt, gecacht | Grundlage für Kennzahlen, Regeln und Kacheln |
| **Prüflauf** | Hintergrund-Job, beim Start und alle `ANALYSIS_MINUTES` | holt alle Datensätze, wendet Regeln an; Seiten zeigen diesen Stand |
| **Fähigkeit** | Domäne × Operation, mit Voraussetzungen | erkennen, was eine Verbindung kann (Plugin, Recht, Schnittstelle), und wer was speichert |
| **Domäne** | ein Thema, das mehrere Dienste kennen | siehe „Fachliche Dinge“ |
| **Verbund** | Verbindungen verschiedener Dienste, die zusammenarbeiten (englische Oberfläche: Bundle) | Partner finden (welches Kimai zu welchem Ninja), Zuordnungen tragen; implizit, solange es je Dienst eine Verbindung gibt |
| **Zuordnung** | ein Ding in mehreren Diensten, je Dienst mit seiner ID | Kunde Kimai 12 = Ninja „Kx9“ = Sure-Zahler „ACME“ = Paperless-Korrespondent 7 |
| **Verlauf** | Kennzahlen je Tag (Snapshots) | Trends, Saisonvergleich, Prognosen in Kacheln |

## Auswertung

| Konzept | Was es ist | Wofür Andon es nutzt |
|---|---|---|
| **Kennzahl** | reine Funktion: Datensatz → Zahl | Kacheln, Popups, Kundenseite |
| **Regel** | reine Funktion: Datensätze → Befunde, mit Bereichs-Einstellungen | Auffälligkeiten erkennen, z. B. `kimai.missing_day`, `in.overdue`, `docs.missing` |
| **Befund** | ein Treffer einer Regel mit Fingerprint und Parametern | wird zum Hinweis; gleicher Fingerprint = derselbe Hinweis |
| **Hinweis** | Befund mit Stufe (Info, Warnung, kritisch) und Zustand | Liste `/hints`, Kacheln, Push, Zusammenfassung, iCal, API |
| **Zustand** | offen, zurückgestellt, quittiert, erledigt | quittieren je Person oder fürs Team; erledigt, wenn die Regel nicht mehr anschlägt |
| **Arbeit** | Zuständige Person und Stand (offen, in Arbeit, erledigt) | Hinweise im Team verteilen |
| **Flattern** | ein Hinweis, der oft kommt und geht | wird nicht wiederholt gepusht, im Rauschbericht genannt |

## Fachliche Dinge

Die Domänen aus `internal/caps` und weitere Dinge, die Andon auswertet. „Liest“ und „schreibt“ nennen die Dienste.

| Konzept | Liest aus | Schreibt nach | Was Andon damit tut | Wo sichtbar |
|---|---|---|---|---|
| **Kunde** | Kimai (Kunden), Invoice Ninja (Clients, führt die Namen), Sure (Zahler), Paperless (Korrespondenten) | Kimai (Name aus Ninja übernehmen) | über Dienste zuordnen; Stunden, Unabgerechnetes, offene Rechnungen, Umsatz, Zahlungsgewohnheit, Stundensatz, Abhängigkeit von einem Kunden | Kundenseite `/clients`, Verbund → Kunden, Abrechnung |
| **Arbeitszeit** | Kimai (Zeiten, Sollzeit aus dem Vertrag) | Kimai (Timer, Buchungen) | fehlende Tage, Überstunden, Woche gegen Soll, unabgerechnete Stunden | Kimai-Kacheln, Hinweise |
| **Abwesenheit** | Kimai (holiday-bundle: Urlaub, Feiertage) | – | Soll und fehlende Tage richtig rechnen | Kimai-Kacheln |
| **Rechnung** | Invoice Ninja | Invoice Ninja (Entwürfe aus Kimai-Zeiten) | offen, überfällig, langsame Zahler, fehlende USt, Umsatzziel, Liquidität, Jahresprognose; Jahrespaket für die Steuer | Abrechnung `/billing`, Geld-Kacheln |
| **Zahlung** | Invoice Ninja, Sure (Kontobewegungen) | Invoice Ninja (Buchung aus Sure) | Eingänge den Rechnungen zuordnen, unerwartete Eingänge | Abrechnung, Hinweise |
| **Beleg** | Invoice Ninja (Ausgaben), Paperless (Scans), Postfach (Anhänge) | Invoice Ninja, Paperless (gegenseitige Verweise, Upload) | Ausgaben mit Scans verknüpfen; Rechnungen und Eingänge ohne Ausgabe | Belege `/receipts` |
| **Abo** | Sure (wiederkehrende Buchungen), Wallos, Paperless (Verträge) | – | Kosten, Kündigungsfristen, ungenutzte oder ausgebliebene Abos, Wallos-Abos ohne Buchung | Kacheln, Hinweise |
| **Termin** | Kalender (iCal) | – | gegen Kimai-Buchungen prüfen | Hinweise |
| **Ort** | Dawarich (Areas), Kimai-Plugin Anfahrten (Orte mit Art) | Kimai, Dawarich, Andon (Art und Kunde) | Kundenorte, Zuhause, Arbeit; Grundlage für Fahrten | Akte der Dawarich-Verbindung → Orte |
| **Fahrt** | Dawarich (Tracks), Kimai-Plugin Anfahrten | Kimai | Strecke, Einordnung beruflich, Pendeln oder privat mit Grund; km, Fahrtkosten, Verpflegung | Reise-Kachel, Hinweise `geo.*` |
| **Frist** | Bereichs-Einstellungen (Steuer), Paperless (Verträge) | – | anstehende Termine | Zusammenfassung, iCal-Feed |
| **Host** | Homelab-Verbindungen, Uptime Kuma, Zertifikate | – | was auf einer Maschine läuft und ob es antwortet | Host-Seite `/hosts` |
| **Stack, Doku-Notiz** | Gitea (Compose-Repos, Obsidian-Frontmatter), Komodo (was läuft) | – (nur Hansei schreibt in den Vault) | Doku gegen Compose abgleichen, Compose gegen Komodo (Host aus dem verknüpften Repo, sonst Servername; Stack = Ordner), Befunde an Hansei | Doku-Kacheln, `/api/docs` |
| **Backup** | Borg, Proxmox, TrueNAS, PgBackWeb u. a. | – | Alter, Fehler, Wiederherstellungstest | Backup-Kachel, Hinweise |

## Benachrichtigung

| Konzept | Was es ist | Wofür Andon es nutzt |
|---|---|---|
| **Kanal** | Apprise-URL eines Benutzers (ntfy, Telegram …), ab Stufe, nur bestimmte Quellen | Ziel neuer Hinweise als Push |
| **Apprise-Server** | `APPRISE_API_URL`, einmal je Instanz | stellt die Pushs an die Kanäle zu |
| **Ruhezeit** | Zeitfenster je Benutzer | Pushs zurückhalten, Kritisches optional durchlassen |
| **Zusammenfassung** | Mail täglich oder wöchentlich, Inhalt einstellbar, Log der Sendungen | offene Hinweise und Fristen im Überblick; wöchentlich mit Rückblick und optional KI-Text |
| **iCal-Feed** | Kalender mit Token | Fristen und fällige Hinweise im eigenen Kalender |
| **Webhook** | eingehender Stand eines Dienstes (Hansei) | Batches, die auf Freigabe warten |

## Sicherheit und Betrieb

| Konzept | Wofür Andon es nutzt |
|---|---|
| **Hauptschlüssel** (`MASTER_KEY`) | verschlüsselt Zugangsdaten und die Datenbank-Datei |
| **Export** | Bereich oder Board als Datei, ohne Geheimnisse; Jahrespaket der Abrechnung |
| **Selbstsicherung** | regelmäßige verschlüsselte Kopie der Datenbank |
| **Audit-Log** | wer was geändert hat |
| **Demo-Modus** | nur für Screenshots, mit der Demowelt „Studio Weber“; nie in Auslieferungen |
