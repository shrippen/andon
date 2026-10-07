# Andon: QA-Journeys

Feste User Journeys für explorative QA-Sitzungen: Wer etwas an Andon prüft, spielt diese Abläufe im Browser durch und notiert Reibung, Unlogisches und Fehler. Erste Runde 07.10.2026 (fünf Durchläufe parallel); Funde und Stand in [`ROADMAP.md`](ROADMAP.md), Abschnitt „QA-Runde 07.10.2026“.

## Ablauf einer Sitzung

1. Eine Instanz je Bereich, jede mit eigenem Port und Datenordner:
   `PORT=8101 BASE_URL=http://localhost:8101 DATA_DIR=/tmp/qa-a ./start.sh demo` (ohne `demo` für die leere Instanz in J1).
2. Demo-Logins: `mara@studio-weber.example.test` (Freelancerin, eigene Boards), `lena@studio-weber.example.test` (Admin), Passwort `demo-password-1`. Sprache je Benutzer über das Benutzermenü oder `POST /me/locale` (`locale=de|en`).
3. Jede Journey auf dem Desktop (1440×960) und die markierten (📱) auch auf dem Handy (390×844).
4. Nebenher beobachten: Server-Log (`level=ERROR`, `request failed`), Browser-Konsole (Fehler, CSP-Meldungen), Antworten ≥ 400, seitliches Scrollen, abgeschnittene Texte, rohe Katalogschlüssel (`hint.xyz.title`, `{n}`), Englisch auf deutschen Seiten und umgekehrt.
5. Fund notieren mit: Stufe (Fehler, Reibung, unlogisch, kosmetisch), Journey und Schritt, was passierte gegen was erwartet war, Beleg (Screenshot, Log-Zeile, URL). Nur Beobachtetes, Ungetestetes ausdrücklich nennen.
6. Gefundene Fehler: zuerst ein Test, der fehlschlägt, dann die Korrektur (siehe `agent.md`).

Werkzeug: Playwright (Chromium) mit kleinen Skripten je Journey; Login über `#email`, `#password`, Enter. Die Demo schreibt nicht in Dienste (Verbindungen `demo://`): prüfen, ob die Oberfläche das verständlich sagt.

---

## A. Erster Start und Verbindungen (ohne Demo)

**J-A1 Frische Installation** · Persona: Selbsthoster, Admin · Ziel: erstes Konto und klarer nächster Schritt
1. `/` öffnen → Weiterleitung auf `/setup`.
2. Falschen Setup-Code eingeben, dann den aus der Log-Zeile „SETUP CODE“. Eingaben (Name, E-Mail) müssen nach dem Fehler stehen bleiben.
3. Anmelden → `/welcome`.
4. `/`, `/hints`, `/clients`, `/billing`, `/receipts`, `/timeline`, `/hosts`, `/widgets`, `/spaces/1/connections` besuchen.
Erwartet: jede leere Seite nennt den nächsten Schritt (Verbindung anlegen, Kachel hinzufügen).

**J-A2 Erste Verbindung mit Fehlern** · Persona: Admin · Ziel: Kimai mit falscher Adresse und falschem Token, dann reparieren
1. `/connections/new` → Kimai. Persönlicher Bereich: das Token-Feld muss sofort sichtbar sein.
2. URL `http://127.0.0.1:9/`, beliebiges Token, speichern → `/connections/{id}?welcome`.
3. „Testen“: verständliche Fehlermeldung, in der UI-Sprache, mit Hinweis auf Admin → Netzwerk, falls die Adresse gesperrt ist.
4. Reiter Einstellungen: URL ändern und speichern (mit gespeichertem gemeinsamem Token: das Feld „Neue Adresse? Zugang“ ausfüllen).
5. Unter Admin → Einstellungen → Netzwerk `127.0.0.0/8` erlauben; gegen einen Nachbau, der 401 antwortet, erst falsches, dann richtiges Token auf dem Reiter Zugang; Test.
Erwartet: nichts Getipptes geht verloren, nach dem Speichern eines neuen Tokens stimmt der Zustand.

**J-A3 Dienst ohne Anmeldung → Kachel → Board** · Ziel: Wetterwarnungen (DWD) auf der Startseite
1. `/connections/new?service=dwd`, Ort „Weimar“ suchen und wählen, speichern.
2. Unter „Passende Kacheln“ die Kachel anlegen.
3. Prüfen, ob und wie sie auf die Startseite kommt (Bibliothek `/widgets`, Board bearbeiten → Kachel hinzufügen → „Auch hier zeigen“).
Erwartet: ein kurzer Weg von der Verbindung zur Kachel auf dem Board.

**J-A4 Dashy-Import** 📱
1. `/import`, Format Dashy, eine `conf.yml` mit drei Abschnitten, einem eingeklappten, Umlauten, einem Eintrag ohne URL, `theme: nord`.
2. Vorschau, „Jetzt importieren“, neues Board auf Desktop und Handy ansehen.
3. Dieselbe Datei noch einmal importieren (Doppel?), dann kaputtes YAML.

**J-A5 Zweiter Benutzer**
1. `/admin/users` → einladen; Link in einem frischen Browser öffnen.
2. Zu kurzes Passwort (Name muss stehen bleiben), dann gültiges.
3. Als neuer Benutzer `/`, `/connections`, `/admin/users`, `/spaces/1/connections`.
4. Selbstregistrierung erlauben, doppelte und neue Adresse registrieren.
5. Die eigene Admin-Rolle herabsetzen wollen.

**J-A6 Passwort zurücksetzen und TOTP**
1. „Passwort vergessen?“ für bekannte und unbekannte Adresse (ohne SMTP: was sagt die Seite?).
2. `/reset/invalid`; Admin → Benutzer → Reset-Link, Passwort setzen, Link erneut verwenden.
3. `/me/security` → TOTP einrichten: falscher Code, dann richtiger; Seite neu laden.
4. Abmelden, mit TOTP anmelden: falscher Code, richtiger Code; fünfmal falsch, dann richtig (muss „zu viele Versuche“ sagen).

## B. Alltag (Demo, Mara)

**J-B1 Morgen-Check** · Ziel: Kacheln überblicken, Details lesen
1. Anmelden, Start-Board; Boards 2–5 durchsehen.
2. Jeden Detaildialog öffnen (Puls-Symbol bzw. `[data-details]`), reihum mit Esc, ×, Klick auf den Hintergrund schließen.
3. Einem Hinweis-Link aus der Kachel „Hinweise“ folgen (`/hints#hint-…`).
4. Banner „Was ist das hier?“ schließen, anderes Board öffnen.
Erwartet: Dialoge mit Titel, Fokus zurück auf den Auslöser; Zahlen in Kachel und Dialog stimmen überein (z. B. „Hinweise“: offen, kritisch, Warnungen).

**J-B2 Hinweise abarbeiten** 📱
1. `/hints`; Filter „Kritisch“, dann ein Dienst (`?level=critical&source=…`).
2. Suche `#hint-search` mit Hinweistext, mit Wörtern der Schaltflächen („pausieren“, „Notiz“ dürfen nicht alles treffen).
3. ⋯ Details eines Hinweises; ✎ Notiz und „Erledigt“; „7 Tage pausieren“.
4. Tastatur: `j` dreimal, `a`; Sammelaktion „Alle N erledigt“ einer Gruppe.
5. `/hints?view=done`: Ist das Erledigte dort? Lässt es sich zurückholen?
6. Zähler vergleichen: Navigation, Filterchips, Kacheln „Lage“ und „Hinweise“.

**J-B3 Tastatur**
1. Auf dem Board `/`, „kimai“, ↓, Esc; Text ohne Treffer.
2. Strg+K, „hinw“, ↓, Enter; `?` für die Tastenhilfe.
3. 60× Tab: Fokusring überall sichtbar (auch im Suchfeld).
4. Enter auf `.launch-detail`, Tab im Dialog.

**J-B4 Admin-Board** · Persona: Lena · `/boards/1`
1. „Kompakt“ (`?view=compact`), „Alles zeigen“.
2. „Wandanzeige“ (`?kiosk&every=60&dim=22-7`): kein Einführungsbanner, alles sichtbar?
3. „Mein Layout“ (`?layout`): Kachelgröße eines Abschnitts ändern, Fertig, bleibt es? „Auf Standard zurücksetzen“.

**J-B5 Weitere Seiten**
1. `/timeline`, `/hosts`, `/hosts/<name>`, `/hosts/gibt-es-nicht`, `/reports/isp`.
2. `/calendar.ics` mit Sitzung, ohne Sitzung, mit Lese-Token (`/me/security`): doppelte Termine?

**J-B6 Sprache**
1. Im Benutzermenü auf Englisch wechseln, alle Seiten und Dialoge von J-B1 bis J-B5 erneut.
2. Nach rohen Schlüsseln, `{n}`, Deutsch auf englischen Seiten und Englisch auf deutschen suchen.

## C. Geld und Zeit (Demo, Mara)

**J-C1 Monatsabrechnung** 📱
1. Start-Board: Kennzahlen notieren; jeden Detaildialog öffnen.
2. `/billing` mit `/boards/3` (Nicht abgerechnet, Offene Rechnungen) und `/clients` vergleichen.
3. Je Kunde muss ein Entwurf angeboten werden („Entwurf in Invoice Ninja“), ein offener Entwurf beim selben Kunden sollte erwähnt sein.
4. „ZIP herunterladen“: CSVs prüfen (Lieferant gefüllt?, Zahlen wie auf den Seiten).
5. „An Paperless“ bei einer Rechnungsmail.

**J-C2 Belege**
1. `/receipts`: „Angehakte verknüpfen“ (Demo lehnt ab: nur Fehler, kein Erfolgshinweis daneben).
2. Reiter „Belege zuerst“: Ausgaben suchen, „Als Ausgabe anlegen“ → „Anlegen und verknüpfen“; bleibt der Reiter?
3. Reiter „Verknüpft“. Zähler auf allen Reitern stimmen überein.

**J-C3 Kunden und Verbund**
1. `/clients`, ein Kunde, „Verknüpfung ändern“ → `/spaces/{id}/verbund#implicit`.
2. „Kunden“ → `/verbund/{id}/customers`; „Alle Vorschläge bestätigen“, eine Zelle lösen, „Namen angleichen“.
3. `/billing` und die Kundenseite danach.

**J-C4 Kimai Lite**
1. Kachel „Kimai-Timer“ anlegen und auf ein Board setzen.
2. Einträge heute (`/kimai/day`): bearbeiten, teilen, löschen.
3. „+“: Ende vor Beginn, dann gültig.
4. Beschreibung tippen, Stop; einen letzten Eintrag starten. Ablehnungen müssen als Meldung auf der Kachel erscheinen.
5. Hinzufügen-Formular offen lassen, 70 s warten: Geht Getipptes verloren?
6. Uhrzeiten mit der Zeitzone der Instanz (`TZ`) vergleichen; Summen „heute“ in Kimai-Kachel, Heute-Kachel, Kimai Lite.

**J-C5 Fahrten und Orte**
1. Board „Reisen“, Detaildialoge.
2. `/travel/places`: Zuhause zuordnen; ein häufiges Ziel als „privat“ anlegen (Vorbelegung prüfen).

**J-C6 Vom Geld-Hinweis zur Lösung**
1. `/hints?sort=value`, jeder Geld-Hinweis: führt die Aktion an die Stelle, wo man ihn behebt (in Andon, wenn Andon es kann)?

## D. Boards bauen (Demo, Mara)

**J-D1 Board und Kacheln anlegen** 📱
1. `/boards/new`: Name leer, nur Leerzeichen, 300 Zeichen, vorhandener Name, „QA Board“; aus Vorlage.
2. Bearbeiten: „+ Abschnitt“, „Kachel hinzufügen“ → Galerie (Vorschauen ohne Fehler?) → Uhr → Zonen `Europe/Berlin, America/New_York, Not/AZone`, 24 h.
3. Auf dem Board: Format wie gewählt; ↕, ↔, ×; Schnell-Link `example.org`, dann `not a url` (Meldung, kein 500).

**J-D2 Rückgängig, Verlauf, Wiederherstellen**
1. Zwei Tabs, B fügt Abschnitt hinzu, A speichert veraltet: verständlicher Konflikt?
2. „↶ Rückgängig“ dreimal: geht es schrittweise zurück?
3. `/boards/{id}/history`: ältere Fassung wiederherstellen. Nach Löschen eines Boards darf ein neues keinen fremden Verlauf zeigen.

**J-D3 Geteilte Kachel**
1. Dieselbe Kachel auf zwei Boards („Auch hier zeigen“), bearbeiten, beide Boards prüfen.
2. Löschen: Warnung mit Anzahl der Boards? Rückgängig?

**J-D4 Code-Ansicht** · `/spaces/{id}/code`
1. Unverändert mit „Ersetzen“ speichern (bleiben IDs und Lesezeichen?), dann „Zusammenführen“ mit dem vollen Text (Doppel?).
2. Ungültiges YAML, unbekannte Kachel, unbekannter Schlüssel, doppelter Slug.

**J-D5 Teilen** · `/shares/board/{id}`
1. Mit einem Team teilen, mit einer Person (Recht Bearbeiten), Recht ändern, widerrufen.
2. Als die andere Person: `/boards`, das Board, `?edit`, Einstellungen; ein nicht geteiltes Board direkt aufrufen (403).

**J-D6 Themes**
1. Duplizieren, Farbe mit 1:1-Kontrast, ungültiger Wert (Meldung, Wert bleibt), gültiger Wert; auf ein Board legen.
2. Export, Import, Nicht-ZIP importieren, löschen.

**J-D7 Board-Export und -Import**
1. `/boards/{id}/export`, in denselben und in einen anderen Bereich importieren: Kommen die Kacheln mit?

## E. Verwaltung (Demo, Lena)

**J-E1 Einladen**
1. `/admin/users`: Einladung an „not-an-email“ (abgelehnt), an Mara (vergeben), an jonas@ (Englisch, Team Produktion, Betrachter), noch einmal an jonas@ (ersetzt die erste).
2. Link im frischen Browser: Name und Passwort; Link erneut (404).

**J-E2 Rollen und Status**
1. Eigene Rolle ändern (abgelehnt, besser gar nicht angeboten).
2. Jonas befördern, zurückstufen, deaktivieren (Sitzung endet), reaktivieren, Reset-Link, Benutzer löschen.
3. `/admin/users/9999/reapply` (404, kein 500).
4. Als Mara POST auf `/admin/users/*`, `/admin/invite`, `/admin/settings/general`: 403.

**J-E3 Rechte als Betrachter (Jonas)**
1. `/teams`, `/spaces/{id}/settings/page`, `/spaces/{id}/connections`, `/spaces/{id}/verbund`, `/boards/1…5`, `/connections/{id}/edit`, `/shares/board/1`.
2. POST auf Einstellungen, Verbindungen, Geheimnisse, Webhook-Rotation: alles abgelehnt, mit Meldung in der UI-Sprache. Sichtbare Speichern-Knöpfe ohne Recht notieren.

**J-E4 Teams**
1. Jonas zum Editor (Zugriff öffnet sich), Team leer umbenennen, doppelter Name, neues Team, Jonas entfernen (Zugriff schließt sich), den letzten Owner entfernen.

**J-E5 Benachrichtigungen (Mara)** 📱
1. `/me/notify` ohne SMTP und Apprise: was sagt die Seite? Kanal mit ungültiger URL, https, ntfy:// mit Filter; Testen.
2. Ruhezeit „25:99“, Zeit „7.30“ (Fehler am Feld, Eingabe bleibt).
3. Zusammenfassung wöchentlich speichern, Vorschau `/me/notify/digest/preview`, „Jetzt an mich senden“ mit totem SMTP.

**J-E6 Sicherheit**
1. Passwort ändern: falsches aktuelles, gleiches, echtes (Erfolgsmeldung, zweite Sitzung).
2. TOTP mit Wiederherstellungscode; zwölfmal falsches Passwort (Sperre), andere Person von derselben IP.
3. Token für die Einbettung ohne Board, mit 0 Tagen.

**J-E7 Verbindungen als Admin**
1. `/connections/{id}`: Reiter Übersicht, Zugang, Einstellungen, Verlauf; Test; leerer Name; Vorlage-Verbindung.

**J-E8 Server, Betrieb, Audit**
1. `/admin/settings`: ungültige Zahlen und URLs, CIDR „not-a-cidr“, `javascript:` als iframe-Herkunft, OIDC-Test leer.
2. Mit `SMTP_URL`/`ANALYSIS_MINUTES` in der Umgebung neu starten: Felder gesperrt, POST ändert nichts.
3. `/admin/operations`, `/admin/audit`: rohe Schlüssel, Leerlauf-Einträge?
