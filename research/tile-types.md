# Recherche: Kacheltypen (07.10.2026)

Abgleich der rund 95 Andon-Kacheltypen mit [Dashy](https://github.com/Lissy93/dashy/blob/master/docs/widgets.md), [Homepage](https://gethomepage.dev/widgets/), [Glance](https://github.com/glanceapp/glance/blob/main/docs/configuration.md) und [Homarr](https://homarr.dev/docs/category/widgets); daraus Vorschläge für Kacheln, Verbindungen, Analysen und Queranalysen. Nichts davon ist begonnen.

## 1. Dashy: was in Andon fehlt

Abgedeckt sind Uhr, Wetter, RSS, Kalender, Bild, öffentliche IP, Blacklist, Domains, Krypto, Kurse, Feiertage, Sport, Witz, xkcd, Flüge, APOD, GitHub-Trends, Systeminfo, Pi-hole, AdGuard, Nextcloud, Proxmox, SABnzbd, Gluetun, Uptime Kuma, Glances-Werte, iFrame und eigene APIs (`custom_api`, `jsonapi`).

| Dashy-Widget | Andon | Anmerkung |
|---|---|---|
| `cve-vulnerabilities` | fehlt | Sicherheitslücken; in Andon besser je laufendem Image (Queranalyse unten) |
| `health-checks` | fehlt | Healthchecks.io: Cron-Herzschläge |
| `code-stats`, `rescue-time` | fehlt | Programmier- bzw. Bildschirmzeit; mit Kimai vergleichbar (unten) |
| `hackernews-trending`, `news-headlines` | teilweise (RSS) | eigene Kachel für Hacker News, Lobsters, Reddit wie bei Glance |
| `ntfy-stream` | fehlt | letzte Nachrichten eines ntfy-Topics |
| `drone-ci` | teilweise | CI-Läufe: Andon meldet den letzten fehlgeschlagenen Lauf (GitHub, Gitea Actions), zeigt aber keinen Verlauf |
| `prometheus` | fehlt | beliebige Prometheus-Abfrage als Wert oder Verlauf |
| `gpu` | fehlt | GPU-Last und Temperatur (Glances liefert sie) |
| `addy`, `wallet-balance`, `eth-gas-prices`, `minecraft-status`, `live-tennis`, `covid-stats`, `stat-ping`, `tactical-rmm`, `filebrowser`, `linkding`, `synology-download` | fehlt | geringer Nutzen hier: Dienst nicht im Einsatz, eingestellt oder durch vorhandene Kacheln gedeckt (Linkwarden, Links) |

## 2. Andere Dashboards: Typen ohne Gegenstück

| Gruppe | Beispiele (Quelle) | Nutzen für Andon |
|---|---|---|
| Sicherung | Proxmox Backup Server, Kopia, Backrest, Duplicati, UrBackup (Homepage) | hoch: gehört in die Backup-Kachel und `backups.*` |
| Strom | PeaNUT/NUT, APC UPS, OpenDTU, EVCC (Homepage) | hoch: USV-Laufzeit, Solarertrag, Laden; passt zu Tibber und `energy` |
| Netz | FRITZ!Box, UniFi, Traefik, Caddy, Nginx Proxy Manager, Headscale, Technitium (Homepage) | mittel: Ausfälle der Leitung, Routen gegen Stacks |
| Herzschläge | Healthchecks, Gatus, Changedetection.io (Homepage, Glance) | mittel: Cron-Jobs, geänderte Seiten |
| Updates | Watchtower, What's Up Docker, Releases je Repo (Homepage, Glance `releases`) | teilweise vorhanden (`updates`, `update_window`) |
| Medien | Jellystat, Tautulli, Audiobookshelf, Navidrome, Seerr (Homepage, Homarr) | gering, `mediaserver` und `arr_upcoming` decken den Kern |
| Aufgaben | Vikunja (Homepage), To-do (Glance) | mittel: Fälligkeiten in Fristen und iCal |
| Finanzen | Firefly III, Ghostfolio (Homepage) | gering: Sure ist da; Ghostfolio nur für ein Depot |
| Sonstiges | Frigate, ESPHome, Homebox, LubeLogger, Syncthing, Mailcow, Mastodon (Homepage) | einzeln, siehe Vorschläge |
| Lesen | Hacker News, Lobsters, Reddit, YouTube-Kanäle, Twitch (Glance) | Startseite, ohne Analyse |

## 3. Vorschläge

### Kacheln

1. **Proxmox Backup Server** in der Backup-Kachel: letzte Sicherung je VM, Verify-Jobs, Belegung.
2. **USV** (NUT): Ladung, Restlaufzeit bei aktueller Last, letzte Stromausfälle.
3. **Herzschläge** (Healthchecks oder Gatus): ausbleibende Cron-Jobs.
4. **Lesen**: Hacker News, Lobsters, Reddit als eigene Kachel mit Punkten und Kommentaren statt nur RSS.
5. **Prometheus-Wert**: eine Abfrage als Zahl oder Verlauf, für alles ohne eigenen Adapter.
6. **Releases**: neue Versionen beobachteter Repos (GitHub, Gitea), auch ohne laufende Installation.

### Verbindungen

Proxmox Backup Server, NUT, Healthchecks/Gatus, Wakapi (selbst gehostetes WakaTime), Syncthing, FRITZ!Box, Traefik/Caddy, Vikunja, LubeLogger, OpenDTU/EVCC, ein CVE-Scanner (Trivy-Server oder OSV-API). Jede neue Integration mit ihren Fähigkeiten in `internal/caps/declared.go`, wo sie Domänen berührt.

### Analysen

| Regel | Dienst | Befund |
|---|---|---|
| `pbs.backup_old`, `pbs.verify_failed` | Proxmox Backup Server | Sicherung älter als N Tage, Verify fehlgeschlagen |
| `ups.runtime_low` | NUT | Restlaufzeit unter Schwelle, Last gestiegen |
| `heartbeat.missed` | Healthchecks/Gatus | Job meldet sich nicht |
| `syncthing.out_of_sync` | Syncthing | Gerät lange nicht verbunden, Ordner mit Fehlern |
| `wan.outages` | FRITZ!Box | Leitungsabbrüche je Woche; ergänzt den ISP-Bericht |
| `vikunja.overdue` | Vikunja | überfällige Aufgaben, auch in Zusammenfassung und iCal |

### Queranalysen

| Regel | Liest | Befund | Warum |
|---|---|---|---|
| `cross.vm_unbacked` | Proxmox × PBS | VM ohne Sicherung | Lücke, die keine der beiden Oberflächen zeigt |
| `cross.code_unbooked` | Wakapi × Kimai | an einem Projekt programmiert, nichts gebucht | Freelance: verlorene Stunden, analog zu `calendar.unbooked` |
| `cross.image_cve` | Compose-Stacks (Gitea) × CVE-Scanner | laufendes Image mit kritischer Lücke, mit Host und Stack | nutzt den Doku-Abgleich aus Phase 15 |
| `cross.route_dead` | Traefik/Caddy/Pangolin × Docker/Komodo | öffentliche Route auf einen gestoppten oder fehlenden Container | ergänzt `exposure` |
| `cross.heartbeat_backup` | Healthchecks × Borg | Borg-Job meldet Erfolg, aber Herzschlag fehlt (oder umgekehrt) | zweite Quelle für Sicherungen |
| `cross.service_due` | Dawarich-Fahrten × LubeLogger | Wartung nach gefahrenen km fällig | Fahrten sind schon klassifiziert (Phase 17) |
| `cross.ups_load` | NUT × Home Assistant/Tibber | Restlaufzeit reicht nicht mehr für das geordnete Herunterfahren | Last wächst unbemerkt |
| `cross.charge_business` | EVCC × Fahrten | Ladekosten nach beruflichem km-Anteil | Steuer, ergänzt `geo.travel_costs` |

**Empfehlung zuerst:** `cross.vm_unbacked` und PBS (Sicherung, hoher Schaden bei Lücke), `cross.code_unbooked` (Geld), `cross.image_cve` (baut auf vorhandenen Stacks auf).
