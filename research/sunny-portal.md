# Recherche: SMA Sunny Portal (07.10.2026)

Wie Andon PV-Anlagen von SMA auswertet. Nur Plan, nichts begonnen.

**Entscheidung (07.10.2026):** über Home Assistant. HA sammelt (Langzeitstatistik), Andon wertet aus und speichert nur Auswertungen (ROADMAP §13, „Datenhaltung“).

```
SMA-Geräte ──(lokal: Webconnect / ennexOS / Speedwire)──► Home Assistant
                                                            │ Recorder: Langzeitstatistik (stündlich, unbegrenzt)
                                                            │ Energie-Dashboard: welche Statistik ist PV, Netz, Batterie
                                                            ▼
Andon ──WebSocket──► recorder/statistics_during_period, energy/get_prefs, energy/solar_forecast
  │
  └─► Auswertung (Tag, Monat, Jahr) ──► samples (ein Wert je Kennzahl und Tag) ──► Regeln, Kachel, Popup
```

## 1. SMA in Home Assistant bringen

| Weg | Geräte | Bewertung |
|---|---|---|
| H1. Kern-Integration `sma` | nur Webconnect (ältere Sunny Boy, Tripower, Sunny Island, Storage); **kein** ennexOS, **kein** Energy Meter / Home Manager | ab Werk, reicht für ältere Wechselrichter |
| H2. HACS „SMA Devices Plus“ ([pysma-plus](https://github.com/littleyoda/pysma)) | Webconnect, ennexOS (Tripower X, Sunny Boy Smart Energy), Speedwire (fast alle), Energy Meter 10/20, Home Manager 2.0 | **empfohlen**: ein Weg für alles, auch Netzbezug und Einspeisung; inoffiziell, Pflege durch eine Person |
| H3. Kern-Integration `modbus` (SunSpec-Register, YAML) | alle mit Modbus TCP (im Gerät freischalten) | nur wenn H2 ein Gerät nicht kann; Register je Gerätetyp von Hand |
| H4. SBFspot → MQTT → HA | ältere Geräte (Speedwire, Bluetooth) | Zusatzdienst, nur für Altgeräte ohne Webconnect |
| H5. Energy Meter / Home Manager per Multicast | 239.12.255.254:9522 UDP | in H2 enthalten; braucht HA im Host-Netz (HA OS: gegeben) |
| — Sunny Portal (Cloud) | — | keine HA-Integration gefunden; offizielle SMA-API ist B2B und kostenpflichtig, Weboberfläche inoffiziell (verworfen) |

Voraussetzungen in HA, damit Andon auswerten kann:
- Energie-Sensoren mit `state_class: total_increasing` (Ertrag, Bezug, Einspeisung, Laden, Entladen) → Langzeitstatistik. H1 und H2 setzen das; bei H3/H4 selbst angeben.
- Energie-Dashboard eingerichtet (PV, Netz, Batterie). Andon liest daraus, welche Statistik was ist; keine Zuordnung in Andon nötig.
- Optional Prognose: Forecast.Solar (Kern) oder Solcast (HACS) → Erwartung des Ertrags ohne eigenes Wettermodell in Andon.

## 2. Was HA speichert

| Ebene | Auflösung | Haltedauer |
|---|---|---|
| Zustände (`states`) | jede Änderung | `recorder.purge_keep_days`, Vorgabe 10 Tage |
| Kurzzeitstatistik | 5 min | wie Zustände |
| Langzeitstatistik | 1 h (Mittel/Min/Max bzw. Summe) | unbegrenzt |

Folgerung: Tageskurve (heute, gestern) aus der Kurzzeitstatistik, alles darüber aus der Langzeitstatistik. Andon braucht keinen eigenen Verlauf.

## 3. Andon liest HA

Andon liest HA heute über REST (`api/states`, `api/history`). Statistik und Energie-Konfiguration gibt es nur über die **WebSocket-API** (`/api/websocket`, Anmeldung mit demselben Token bzw. OAuth-Grant):

| Befehl | Liefert | Nutzen |
|---|---|---|
| `energy/get_prefs` | `energy_sources`: Solar (`stat_energy_from`), Netz (Bezug/Einspeisung), Batterie (Laden/Entladen) | Statistik-IDs ohne Zuordnung in Andon; Struktur je HA-Version prüfen |
| `recorder/statistics_during_period` | je Statistik und Periode (`5minute`, `hour`, `day`, `week`, `month`) `change`, `sum`, `mean`, `min`, `max` | Ertrag, Bezug, Einspeisung je Tag/Monat; Leistungskurve |
| `energy/solar_forecast` | Prognose in Wh je Stunde je Prognose-Integration | Ertrag gegen Erwartung |
| `recorder/list_statistic_ids` | alle Statistiken mit Einheit | Auswahl, falls kein Energie-Dashboard |

## 4. Einbau in Andon

```
web/        Kachel „Solar“ bzw. Strom-Kachel, Popup: Tag/Monat/Jahr mit Achsen und Legende
  ↓
services/   analysis: Auswertung je Tag → samples
  ↓
sources/    hass_energy.go: HassEnergy-Datensatz (Quellen, Summen je Periode, Prognose)
  ↓
drivers/    services/hasswsapi.go: WebSocket-Client (auth, id-Zähler, ein Befehl je Aufruf)
```

1. **Treiber**: WebSocket-Client für HA (Anmelden, Befehl senden, Antwort mit passender `id` lesen, schließen). Kurzlebig je Abruf, keine Dauerverbindung. Ein WebSocket-Code liegt schon in `internal/drivers/services/infra.go`; prüfen, ob er sich teilen lässt.
2. **Quelle** `HassEnergyData` (eigener Datensatz an der bestehenden HA-Verbindung, kein neuer Diensttyp): Energie-Konfiguration, Summen gestern / Monat / Vormonat / Vorjahresmonat, Prognose heute und morgen. Kein SMA-Wissen in Andon: jede PV in HA zählt (SMA, OpenDTU, Fronius …).
3. **Kennzahlen** (`internal/metrics/`, rein): Ertrag, Eigenverbrauch = Ertrag − Einspeisung, Eigenverbrauchsquote = Eigenverbrauch / Ertrag, Autarkie = Eigenverbrauch / (Eigenverbrauch + Bezug), Batterie-Zyklen, Ertrag / Prognose.
4. **Speichern**: je Tag ein Wert je Kennzahl (`Readings.SetOn` für den Vortag, wie Tibber). Vergleiche (Vorjahr, Trend) über `samples`; fehlt dort etwas, fragt Andon HA erneut.
5. **Kachel**: Strom-Kachel bekommt PV aus HA (heute: Ertrag, Leistung jetzt, Eigenverbrauchsquote); Popup mit Tages-, Monats- und Jahresbalken (Ertrag, Eigenverbrauch, Einspeisung; Legende, Achsen, Tooltips).
6. **Demo**: Welt um `power.hass_energy` (Studio Weber: Dach 9,8 kWp, Speicher, ein Jahr Monatswerte) erweitern, `sync-demo.py`, Decode.
7. **Tests**: Kennzahlen in `metrics`, Parser gegen aufgezeichnete Antworten; Live-Test über `.local-test/hass` (nur lesen).
8. **Texte** in beiden Katalogen; `QA.md`: PV aus HA anzeigen.

## 5. Regeln

| Regel | Aus | Hinweis |
|---|---|---|
| `solar.yield_low` | HA: Ertrag × Prognose | Ertrag gestern < x % der Prognose (oder, ohne Prognose, des Mittels gleicher Tage im Vorjahr); Verschattung, Defekt, Abregelung |
| `solar.no_yield` | HA: Ertrag | tagsüber über Stunden 0 Wh trotz Prognose > 0: Wechselrichter aus |
| `solar.battery_idle` | HA: Laden/Entladen | Speicher seit Tagen ohne Zyklus |
| `solar.selfuse_drop` | samples | Eigenverbrauchsquote im Monat deutlich unter Vorjahresmonat |
| `cross.solar_selfuse` | HA × Tibber | Einspeisung zu niedrigem Preis, während Bezug teuer war; Ersparnis im Monat |
| `cross.charge_expensive` | EVCC × Tibber × HA | bestehend; PV-Prognose als Alternative nennen |
| Homelab-Kosten | HA | PV-Anteil senkt den Strompreis (vorhandenes Widget) |

## 6. Nicht-Ziele

- Eigene SMA-Treiber in Andon (Webconnect, ennexOS, Speedwire): das macht HA.
- Rohdaten sammeln oder nachladen.
- Schreiben (Einspeiselimit, Parameter).
- Sunny Portal (Cloud): offizielle API B2B und kostenpflichtig, Weboberfläche inoffiziell.

## 7. Offene Fragen

1. Welche SMA-Geräte laufen (ennexOS oder Webconnect, Speicher, Home Manager 2)? Bestimmt H1 oder H2.
2. Ist das HA-Energie-Dashboard eingerichtet? Sonst Statistik-IDs an der Verbindung wählen.
3. Prognose in HA vorhanden (Forecast.Solar, Solcast)? Sonst `solar.yield_low` nur gegen das Vorjahr.

## Anhang: direkter Zugriff auf die Geräte (verworfen)

Für den Fall, dass HA ausfällt oder fehlt.
- ennexOS: `POST /api/v1/token` (`grant_type=password`), `POST /api/v1/measurements/live` mit `[{"componentId":"IGULD:SELF"}]`, `GET /api/v1/plants/Plant:1/devices`. Verlaufsabfrage nicht belegt.
- Webconnect: `POST /dyn/login.json` → `sid`, `POST /dyn/getValues.json`, Verlauf `POST /dyn/getLogger.json` (Keys `28672` Ertrag 5 min, `28704` Ertrag je Tag, `28752` Einspeisung, `28768` Bezug, `29344`/`29360` Batterie); immer `/dyn/logout.json` (begrenzte Sitzungen).
- SMA Monitoring API: OAuth 2, Vertrag mit SMA, Abrechnung je Anlage; Classic 15 min, ennexOS 5 min.

## Quellen

- [HA: SMA Solar (Kern-Integration)](https://www.home-assistant.io/integrations/sma/)
- [HA: Langzeitstatistik von Sensoren](https://developers.home-assistant.io/docs/core/entity/sensor/#long-term-statistics)
- [pysma-plus / SMA Devices Plus](https://github.com/littleyoda/pysma), [PyPI](https://pypi.org/project/pysma-plus)
- [simon42-Forum: SMA mehrere Wechselrichter](https://community.simon42.com/t/sma-mehrere-wechselrichter/56393)
- [SMA Developer FAQ](https://developer.sma.de/faq)
- [Zonnefabriek: Umzug Sunny Portal → ennexOS](https://www.zonnefabriek.nl/en/news/sma-moves-systems-from-sunny-portal-to-ennexos)
