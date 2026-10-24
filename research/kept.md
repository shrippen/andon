# Kept als Inventar (Test 24.10.2026)

[Kept](https://github.com/keptinventory/kept) (AGPL-3.0, TypeScript, PostgreSQL mit pgvector) ist
ein Haushaltsinventar: Orte, Räume, Kisten, Dinge mit Kauf, Garantie, Belegen, Wertangaben.
Anlass: Snipe-IT wird allein genutzt; Ausgabe an Personen, Lizenzplätze, Audits und
Verbrauchsmaterial braucht niemand. Getestet: Kept 1.12.5 als Wegwerf-Container
(`.local-test/extra/kept/`, http 18133, https 18134).

## API

- Token je Person: `Authorization: Bearer kpt_…`, Bereich `read` oder `write`, gilt nur für die
  gewählten Orte. Kept gibt Tokens nur über https heraus (`https_required`).
- Offen für Tokens ist nur, was `apps/server/src/tokens/access.ts` (`TOKEN_ROUTES`) nennt; alles
  andere antwortet 403 `token_scope`.
- Änderungen brauchen `If-Match: <rowVersion>`, sonst 428.

| Andon braucht | Weg | Token |
|---|---|---|
| Dinge mit Kauf und Garantieende, alle auf einmal | `GET /api/v1/things/sheet?locationId=…` (Seiten über `cursor`): `purchase {purchasedOn, price, currency}`, `warranty {endsOn, lifetime}`, Modell, Seriennummer | lesen |
| Eigene Felder (Nutzungsdauer) | `GET /api/v1/things/{id}`: `custom {nutzungsdauer: 36}`; das Sheet liefert `custom` nicht | lesen |
| Belege | `GET /api/v1/things/{id}/attachments` (`role: receipt`, Datei oder `url`) | lesen |
| Paperless-Verknüpfungen | `GET /api/v1/paperless-links?thingId=…` (je Ding): `{connected, url, items: [{documentId, kind, title, typeName, correspondent, createdOn, openUrl}]}`; `url` ist das Paperless des Kept-Orts, `kind` `receipt`, `invoice`, `warranty_doc`, `manual` … (Kept liest es aus Dokumenttyp und Tags); Titel aus Kepts Speicher, auch wenn Paperless nicht antwortet | lesen |
| Termine | `GET /api/v1/agenda?from=…&to=…`: `items [{sourceType, kind, state, dueOn, title, subject {type, id, name, shortCode}}]`, `state` `upcoming`/`expiring`/`due`/`overdue` mit Kepts Vorlauf je Eintrag; `sourceType` `thing_expiry`, `warranty`, `document`, `schedule`, `loan`. Geprüft: Ablaufdatum 2026-11-20 mit 30 Tagen Vorlauf ist am 24.10. `expiring`, Garantien `upcoming` | lesen |
| Orte und Module | `GET /api/v1/locations`: `locations [{id, name, modules}]`; `modules` enthält `mcp`, wenn das Modul an ist | lesen |
| Räume eines Orts | `GET /api/v1/locations/{id}/places`: `places [{id, name, isUnplaced}]`; „Unplaced“ nimmt Dinge ohne Raum auf | lesen |
| Ding anlegen | `POST /api/v1/things` mit `locationId`, `placeId` (Pflicht; Andon nimmt den Raum mit `isUnplaced`) und `purchase` | schreiben |
| Feld ändern | `PATCH /api/v1/things/{id}` mit `custom`, `If-Match` | schreiben |
| Garantie setzen | REST `POST /things/{id}/warranties` ist für Tokens gesperrt; MCP-Werkzeug `add_warranty` (`thing_id`, `kind`, `term_months` oder `ends_on`) geht, wenn im Ort das Modul `mcp` an ist | schreiben |
| Schreibrecht erkennen | MCP `tools/list`: ein Schreib-Token bekommt `add_warranty`, ein Lese-Token nicht; ohne Modul MCP gibt es keine Werkzeuge, das Recht bleibt unbekannt | lesen |
| Wertangaben, Webhooks, CSV-Import/-Export | nur mit Anmeldung in Kept | – |

Garantie: Kept rechnet das Ende selbst (`startsOn` + `termMonths`, ohne Start das Kaufdatum).

## Abschreibung

Kept kennt keine AfA. Wertangaben (`valuations`) sind von Hand gepflegte Zeitwerte (Versicherung),
kein Buchwert. Die AfA bleibt in Andon (`metrics.Depreciation`); Kept liefert Kaufdatum, Preis und
die Nutzungsdauer aus einem eigenen Feld des Typs (Zahl, Monate; Schlüssel als Option der
Verbindung, Vorgabe `nutzungsdauer`).

## Unterschiede zu Snipe-IT

- IDs sind UUIDs (Snipe-IT: Zahlen), Kurzcodes mit 6 Zeichen (`H6ZQ46`).
- Keine Lizenzen, Audits, Lebensende, Ausgabe an Personen; Verleih (`loans`) und Verbrauchsgüter
  (`consumables`) gibt es, beide für Tokens gesperrt.
- Ablaufdatum je Ding (`expiresOn` mit `expiryLeadDays`, per Token änderbar) und alle Termine
  über `agenda`: Grundlage für Fristen in Andon.
- Webhooks (`thing.created`, `thing.updated` …) richtet nur Kept ein; Andon könnte sie als
  Auslöser für einen neuen Abruf nehmen (`caps.Traits.Webhooks`).
