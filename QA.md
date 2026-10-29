# Andon: QA-Journeys

Feste User Journeys für explorative QA-Sitzungen: Wer etwas an Andon prüft, spielt diese Abläufe im Browser durch und notiert Reibung, Unlogisches und Fehler. Erste Runde 07.10.2026 (fünf Durchläufe parallel); Funde und Stand in [`ROADMAP.md`](ROADMAP.md), Abschnitt „QA-Runde 07.10.2026“.

## Ablauf einer Sitzung

1. Eine Instanz je Bereich, jede mit eigenem Port und Datenordner:
   `PORT=8101 BASE_URL=http://localhost:8101 DATA_DIR=/tmp/qa-a ./start.sh demo` (ohne `demo` für die leere Instanz in J1).
2. Demo-Logins: `mara@studio-weber.example.test` (Freelancerin, eigene Boards), `lena@studio-weber.example.test` (Admin), Passwort `demo-password-1`. Sprache je Benutzer über das Benutzermenü, den Umschalter DE/EN im Fuß jeder Seite oder `POST /locale` (`locale=de|en`).
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
1. `/connections/new`: Suche „kim“ findet Kimai, die Gruppen (wie in der Galerie) zählen mit → Kimai. Persönlicher Bereich: das Token-Feld muss sofort sichtbar sein.
2. URL `http://127.0.0.1:9/`, beliebiges Token, speichern → `/connections/{id}?welcome`.
3. „Testen“: Ursache in der UI-Sprache („Verbindung abgelehnt …“, „Von der Netzwerk-Regel blockiert“ mit Link Admin → Netzwerk, 401/403 mit Link zum Zugang), die rohe Meldung darunter.
4. Reiter Einstellungen: URL ändern und speichern (mit gespeichertem gemeinsamem Token: das Feld „Neue Adresse? Zugang“ ausfüllen).
5. Unter Admin → Einstellungen → Netzwerk `127.0.0.0/8` erlauben; gegen einen Nachbau, der 401 antwortet, erst falsches, dann richtiges Token auf dem Reiter Zugang; Test.
Erwartet: nichts Getipptes geht verloren, nach dem Speichern eines neuen Tokens läuft der Test von selbst und zeigt sein Ergebnis.

**J-A3 Dienst ohne Anmeldung → Kachel → Board** · Ziel: Wetterwarnungen (DWD) auf der Startseite
1. `/connections/new?service=dwd`, Ort „Weimar“ suchen und wählen, speichern.
2. Nach dem grünen Test unter „Passende Kacheln“ Board wählen, „… auf Board legen“.
3. Alternativ: Bibliothek `/widgets` → Zeilenmenü „Auf Board legen“; ein neues, leeres Board schlägt Kacheln für Verbindungen ohne Kachel vor.
Erwartet: ein Klick von der Verbindung zur Kachel auf dem Board.

**J-A3b Webhook-Dienst** · Ziel: PG Back Web meldet seine Backups
1. `/connections/new?service=pgbackweb`: Das Feld „Adresse der Oberfläche“ ist optional und erklärt, dass Andon dort nichts abfragt. Ohne Adresse speichern.
2. Die Übersicht zeigt oben die Webhook-URL mit Kopierknopf und Beispiel-Body; mit `BASE_URL` auf `localhost` eine Warnung.
3. `curl -X POST <URL> -d '{"event":"execution_success","name":"test"}'` → 204; „Neue URL erzeugen“ → alte URL 404, zurück auf der Übersicht.
Erwartet: ohne Suche nach dem Reiter klar, welche Adresse in den Dienst gehört.

**J-A4 Dashy-Import** 📱
1. `/import`, Format Dashy, eine `conf.yml` mit drei Abschnitten, einem eingeklappten, Umlauten, einem Eintrag ohne URL, `theme: nord`.
2. Vorschau, „Jetzt importieren“, neues Board auf Desktop und Handy ansehen.
3. Dieselbe Datei noch einmal importieren (Doppel?), dann kaputtes YAML.

**J-A5 Zweiter Benutzer**
1. `/admin/users` → einladen; Link in einem frischen Browser öffnen.
2. Zu kurzes Passwort (Name muss stehen bleiben), dann gültiges.
3. Als neuer Benutzer `/`, `/connections`, `/admin/users`, `/spaces/1/connections`.
4. Selbstregistrierung erlauben, doppelte und neue Adresse registrieren: gleiche Antwort (Anmeldung mit Hinweis), mit SMTP Mail „Du hast schon ein Konto“ an die vergebene.
5. Die eigene Admin-Rolle herabsetzen wollen.

**J-A6 Passwort zurücksetzen und TOTP**
1. „Passwort vergessen?“ für bekannte und unbekannte Adresse; ohne SMTP heißt der Link „Admin um neues Passwort bitten“ und die Seite erklärt den Reset-Link der Admins.
2. `/reset/invalid`; Admin → Benutzer → Reset-Link, Passwort setzen (Erfolgsmeldung auf der Anmeldung), Link erneut verwenden.
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
1. `/hints`; in der Filterleiste „Kritisch“, dann ein Dienst (`?level=critical&source=…`), „alle N zeigen“. Am Handy: „Filter (n)“ öffnet die Leiste, der erste Hinweis steht auf dem ersten Bildschirm.
2. Suche `#hint-search` mit Hinweistext, mit Wörtern der Schaltflächen („pausieren“, „Notiz“ dürfen nicht alles treffen).
3. ⋯ Details eines Hinweises; ✎ Notiz und „Erledigt“; „7 Tage pausieren“.
4. Tastatur: `j` dreimal, `a`, `s`; Sammelaktion „Alle N erledigt“ einer Gruppe (nur Regeln mit 2+ Hinweisen haben eine).
5. „Gruppiert / Einzeln“ umschalten, neu laden: bleibt die Wahl? „Nach Geldwert“, „Nach Kunde gruppieren“.
6. `/hints?view=done`: Ist das Erledigte dort? Lässt es sich zurückholen?
7. Zähler vergleichen: Navigation, Filterleiste, Kacheln „Lage“ und „Hinweise“.
8. Am Handy: Menü (☰) öffnet die Navigation von links, aktuelle Seite markiert; Esc, Klick daneben und × schließen, Fokus zurück auf ☰.
9. Querhinweise: Jeder nennt beide Seiten mit Zahl und Zeit, z. B. „Stack showreel bei rotem CI deployt“ (Deploy, Build-Nummer, Repo).

**J-B3 Tastatur**
1. Auf dem Board `/`, „kimai“, ↓, Esc; Text ohne Treffer.
2. Strg+K, „hinw“, ↓, Enter; `?` für die Tastenhilfe.
3. 60× Tab: Fokusring überall sichtbar (auch im Suchfeld).
4. Enter auf `.launch-detail`, Tab im Dialog.

**J-B4 Admin-Board** · Persona: Lena · `/boards/1`
1. „Kompakt“ (`?view=compact`), „Alles zeigen“.
2. „Wandanzeige“ (`?kiosk&every=60&dim=22-7`) bei 1920×1080 und 1280×720: kein Einführungsbanner, Hinweis zum Beenden für einige Sekunden, kein Scrollen; die Kacheln kommen in Sätzen, die den Bildschirm füllen (keine Kachel abgeschnitten; Kacheln der Seitenspalte als normale Kacheln). Braucht ein Board mehrere Sätze, ist jeder mindestens zu einem Drittel gefüllt, gemessen am vollsten Satz (`andonWall.state().fills` in der Konsole); nie ein voller Satz und danach eine einzelne Kachel. Die Sätze wechseln nach der Satzzeit mit dem Übergang aus den Board-Einstellungen, nach dem letzten Satz das nächste Board. Taste, Esc oder Klick in eine Ecke führt zum Board zurück; mit „Bewegung reduzieren“ wechselt der Satz ohne Übergang. Board-Einstellungen → Wandanzeige: Übergang und Beschleunigung ändern spielt die Vorschau, „Vorschau“ spielt sie auch bei reduzierter Bewegung.
3. „Mein Layout“ (`?layout`): Kachelgröße eines Abschnitts ändern, Fertig, bleibt es? „Auf Standard zurücksetzen“.

**J-B5 Weitere Seiten**
1. `/timeline`, `/hosts` (Hinweise zählen als Probleme, Hosts ohne Monitor und Hinweis unter „N ohne Befund“ eingeklappt), `/hosts/<name>`, `/hosts/gibt-es-nicht`, `/reports/isp`.
2. `/reports/isp`: Neuverbindungen der FRITZ!Box mit Zeit und Ausfall (gesehene mit „mindestens … min“), dieselben Zeilen in der CSV; `/timeline` nennt sie als Wechsel „WAN“.
3. `/calendar.ics` mit Sitzung, ohne Sitzung, mit Lese-Token (`/me/security`): doppelte Termine?
4. Kachel „FRITZ!Box“ (Board Studio-IT, Abschnitt Sicherheit): Durchsatz jetzt und als Linie, Repeater mit schwachem WLAN und Thermostat ohne Verbindung farbig, FRITZ!OS mit Update im Fuß. Dialog: Durchsatz-Diagramm mit Achse und Legende, Reiter Geräte, Mesh (WLAN der Box), Anrufe (03:13 „Unbekannt“), Smart Home; Kachel-Optionen blenden Durchsatz, Mesh, Anrufe, Smart Home aus.
5. Kachel „KI-Kosten“ (Board Studio-IT, Abschnitt Status): Summe des Monats und heute in $, je Konto Ausgaben, Guthaben, Schlüssel-Limit oder Abo-Fenster als Balken; die Woche des Claude-Abos (91 %) steht oben. Dialog: Kosten je Tag mit Achse und Legende je Konto, Kontingente und Limits mit Rücksetzzeit, Konten (Budget überschritten rot), Modelle des Monats. Hinweise: Guthaben knapp (DeepSeek, OpenRouter), Kostensprung (Claude API, heute), Woche bei 91 %. Option „Nur diese Anbieter“ (`deepseek`) lässt nur DeepSeek.

**J-B7 Werkbänke finden** 📱
1. Ohne Stern: Die Leiste zeigt neben „Hinweise“ nur Werkbänke mit offenen Hinweisen, mit Zähler (Demo: Abrechnung, USt-Abgleich, Abos, Updates).
2. Menü „Werkbänke ▾“: alle Werkbänke, Gruppen durch Linien getrennt, Zähler wie in der Leiste; „Alle Werkbänke“ öffnet `/benches`.
3. `/benches`: drei Gruppen mit Beschreibung und Zähler; „In die Menüleiste“ bei „Belege zuordnen“: Ab jetzt zeigt die Leiste nur die Werkbänke mit Stern, auch ohne offene Hinweise. Stern wieder entfernen.
4. Strg+K, „ust“: Gruppe „Werkbänke“ mit Beschreibung.
5. Am Handy: ☰ zeigt Hinweise und alle Werkbänke; die Karten auf `/benches` überlappen nicht.

**J-B8 Warten** · DevTools „Network“ auf „Slow 3G“ (oder `tools/loading.py`)
1. Seitenwechsel über die Leiste: Nach kurzer Zeit läuft oben der gestreifte Balken, um den geklickten Link ein Lauflicht, die alte Seite wird blass. Ohne Drosselung blitzt nichts auf.
2. Langsamer Seitenwechsel, dann Esc: Die alte Seite bleibt, alles wieder ruhig.
3. Ein Formular absenden (Hinweis „Erledigt“, Einstellung speichern): Der Knopf zeigt Streifen und ist gesperrt; ab 8 s steht darunter „Dauert länger … 9 s“ und zählt. Esc bricht ein abgeschicktes Formular nicht ab.
4. Offline (DevTools „Offline“) einen Link klicken: Die Seite bleibt, ein Toast „Laden fehlgeschlagen“ bietet „Erneut versuchen“; online wieder, Klick darauf lädt die Seite. Ebenso für Hinweis-Details. Ein abgeschicktes Formular, das scheitert, meldet „Senden fehlgeschlagen“ ohne „Erneut versuchen“.
5. Hinweis-Details aufklappen, Belege-Suche tippen: Beim Nachladen wird der alte Inhalt blass, nicht leer; Platzhalter überall gleich (Quadrate und „lädt …“).
Erwartet: Mit „Bewegung reduzieren“ stehen Balken und Lauflicht still, die Zustände bleiben sichtbar. Kachel-Polls zeigen nie Toasts.

**J-B6 Sprache**
1. Abgemeldet auf der Anmeldeseite im Fuß „EN“ wählen: die Seite ist englisch und bleibt es nach Neuladen. Anmelden: es gilt die Sprache des Profils.
2. Im Fuß (oder Benutzermenü) auf Englisch wechseln, alle Seiten und Dialoge von J-B1 bis J-B5 erneut; ein anderer Browser hat nach der Anmeldung dieselbe Sprache.
3. Nach rohen Schlüsseln, rohen Regel-IDs, `{n}`, Deutsch auf englischen Seiten („Instanz“, „Verbund“) und Englisch auf deutschen suchen.
4. Profil → Zahlenformat „€1,234.56“ bei deutscher Sprache: Beträge und Zahlen (Kacheln, Dialoge, Hinweise, CSV-Export) lesen sich englisch, Texte bleiben deutsch. Betragsfelder (USt-Werkbank, Belege, Abos, eigene Regeln) sind so vorbelegt und lesen „1,234.56“; „Wie die Sprache“ stellt zurück.

## C. Geld und Zeit (Demo, Mara)

**J-C1 Monatsabrechnung** 📱
1. Start-Board: Kennzahlen notieren; jeden Detaildialog öffnen.
2. `/billing` mit `/boards/3` (Nicht abgerechnet, Offene Rechnungen) und `/clients` vergleichen; Zeitraum „12 Monate / Jahr / Vorjahr“ umschalten: Jede Kennzahl nennt ihren Zeitraum (oder „Stand heute“), vor Beginn der Daten „Daten ab …“.
3. Je Kunde muss ein Entwurf angeboten werden („Entwurf in Invoice Ninja“); ein offener Entwurf beim selben Kunden steht mit Nummer, Datum und Link davor. Der Hinweis „nicht abgerechnet“ führt zu `/billing#drafts`.
4. „ZIP herunterladen“: CSVs prüfen (Lieferant gefüllt?, Zahlen wie auf den Seiten).
5. „An Paperless“ bei einer Rechnungsmail.

**J-C7 Abos**
1. Hinweis „Fehlt in Wallos“ → „In den Abos erledigen“ führt nach `/subs`.
2. Demo: Nordhost mit neuem Preis (Wallos und Invoice Ninja, vorab angehakt), Wellenklang doppelt in Wallos, Farbraum fehlt in Wallos. Betrag ändern, „Prüfen“ zeigt alt → neu; „Schreiben“ (Demo lehnt ab).

**J-C11 Inventur**
1. Hinweis „Geräte ohne Inventar“ → `/inventory`: Geräte der FRITZ!Box mit IP und MAC, „Gelesen aus“ nennt sie.
2. Die Demo hat Kept und Snipe-IT; Kept zählt: „In Kept anlegen“ ohne Modell und Status → „Prüfen“ nennt „Als … anlegen“ (Demo lehnt ab); ein anderes „Ignorieren“ → verschwindet, steht unter Bereich → Regeln in den Ausnahmen. Nur mit Snipe-IT: „In Snipe-IT anlegen“ verlangt Modell aus der Liste und Status.
3. Kept mit Token nur zum Lesen oder ohne Modul MCP: Hinweis „Kept nimmt von Andon keine neuen Gegenstände an“, kein „Anlegen“.
**J-C10 Updates**
1. Hinweis „Updates verfügbar“ → `/updates`: Uptime Kuma (WUD) mit neuem Tag, Dawarich aus dem Release-Feed (0.30.6 → 0.30.9, Link zum Release), Immich ohne festen Tag nicht wählbar.
2. Tag ändern, „Prüfen“ nennt Repo, Datei und alt → neu; „Pull Requests öffnen“ (Demo lehnt ab).
**J-C9 Jahr**
1. `/year`: letztes Jahr vorgewählt, Chips für drei Jahre; Einnahmen, Ausgaben je EÜR-Zeile (ohne Feld: Hinweis mit Link auf `/euer`), Abschreibung mit Summe, Homeoffice-Tage mit Pauschale.
2. „Paket herunterladen“ → ZIP enthält `euer.csv`, `afa.csv` (mit Inventar: Kept, sonst Snipe-IT; Nutzungsdauer aus dem Kept-Feld), `homeoffice.csv` (mit Kimai).
**J-C8 Buchungen einordnen**
1. `/subs` → „Buchungen ohne Kategorie einordnen“: Kabelwerk Studiobedarf mit Vorschlag „Technik“ angehakt, Bäckerei ohne Vorschlag.
2. „Prüfen“ nennt Anzahl, Summe und Kategorie; „In Sure einordnen“ (Demo lehnt ab).

**J-C14 Wiederherstellungsprobe**
1. Backups-Kachel → Dialog: je System „Wiederherstellung testen, Probe: …“ mit „Getestet“ und „Fehlgeschlagen“.
2. „Fehlgeschlagen“ → Hinweis „Wiederherstellung fehlgeschlagen“ (Warnung); „Getestet“ danach beendet ihn.
**J-C13 Anschaffungen**
1. `/purchases`: Gegenstände aus Kept (in der Demo vor Snipe-IT); Monitor (vor zwei Monaten, ohne Garantie) mit 24 Monaten vorgewählt; Lücken als Chips, Links zu USt-Abgleich und Belegen.
2. „Prüfen“ → „In Kept schreiben“ (Demo lehnt ab); Monate außerhalb 1–120 werden übersprungen. Kept ohne Modul MCP: Hinweis oben, keine Garantie-Auswahl.

**J-C15 Kept: Fristen und Belege**
1. Hinweise: „Feuerlöscher Studio: Ablaufdatum am …“ (Info), „NAS: Wartung überfällig seit …“ (kritisch), „Studio: Dokument am …“; Garantien und Rückgaben erscheinen hier nicht (Garantien meldet `inventory.warranty_expiring`; Arten in der Regel `kept.due` wählbar). Dieselben Daten stehen im Kalender-Feed.
2. „Kein Beleg zu Monitor 27"“ (`inventory.receipt_missing`), „1 Beleg in Kept verweist ins Leere: Möwe 7“ (Paperless-Dokument 999 fehlt). „In Kept öffnen“ führt auf `/t/<Kurzcode>`.
3. Ein Kept zählt in jedem Verbund, in den es kommt, vor Snipe-IT (AfA, Kosten); eine Verbindung, die als einzige ihres Dienstes besteht, kommt auch in fremde Verbünde (`verbund.fillGroup`). `inventory.split` nennt Gegenstände, die nur Snipe-IT hat, und rät, sie in Kept zu erfassen und Snipe-IT danach zu entfernen.
4. Link-Kachel Kept (Board Übersicht → Dienste): „5 Dinge · 4 fällig“ (alles, was Kept nicht mehr „demnächst“ nennt, auch die Rückgabe).
5. Kachel „Tabelle“ mit Verbindung Kept, Tabelle „Termine im Inventar“: Kepts Termine der nächsten 90 Tage, Überfälliges (Rückgabe, Wartung) zuerst, jeder Name öffnet das Ding in Kept (ohne Ding: Kepts Liste „expiring“); Popup mit allen Zeilen. Kachel „Kennzahl“ Inventarwert mit Kept: Summe der Kaufpreise, Zahl der Dinge.
**J-C12 Release-Feeds**
1. `/updates` → „Release-Feeds anpassen“: fest genannte Images ohne Feed mit vorgeschlagener Adresse und Kategorie; nichts angehakt.
2. Adresse ändern, „Prüfen“; eine Nicht-GitHub-Adresse wird übersprungen; „In FreshRSS schreiben“ (Demo lehnt ab).

**J-C6 Woche schließen**
1. `/week`: letzte Woche vorgewählt, Chips für vier Wochen; „Gelesen aus“ nennt Dawarich und Kalender des Verbunds.
2. Jede Lücke mit Kunde, Zeiten, Projekt und Tätigkeit vorgeschlagen; vollständige angehakt. „Prüfen“ → „In Kimai eintragen“ (Demo lehnt ab).

**J-C7 Abrechnung: Fahrtkosten und Mahnlauf**
1. `/billing` mit Dawarich im Verbund: ein Entwurf bietet „Fahrtkosten mitberechnen“; angehakt steht die Zeile in der Rechnung, beim nächsten Entwurf beginnen die Fahrten danach.
2. `/dunning`: der Mahnplan aus Invoice Ninja steht oben (ohne eingerichtete Erinnerungen: Hinweis auf Andons Standard); fällige Erinnerungen mit Stufe und „fällig seit“; „Texte von Claude schreiben lassen“ füllt Betreff und Text; nichts angehakt; „Prüfen“ → „Mit Invoice Ninja verschicken“ (Demo lehnt ab).

**J-C5 USt-Abgleich: Abgabe**
1. `/vat` → „ELSTER-Kennzahlen“: Werte mit „Kopieren“; „Als gemeldet merken“; danach eine Buchung im Zeitraum ändern → „Berichtigung fällig“ mit den Kennzahlen.
2. Ausgabe eines ausländischen Lieferanten ohne Steuer steht unter „Reverse Charge“ mit Kz 46/47 oder 84/85.
3. Überweisung Geschäft → Privat: „Umbuchung … archivieren“ vorgeschlagen, nicht angehakt.
4. „Doppelte Lieferanten zusammenführen“ → `/vendors`: Gruppe wählen, Prüfen, Schreiben (Demo lehnt ab).

**J-C4 Kategorien und EÜR**
1. Verbund mit Invoice Ninja und Sure → „Kategorien“: Sure-Vorschläge stehen vorausgewählt („Vorschlag“), das EÜR-Feld ist erkannt, keine Zuordnung Kategorie → EÜR; Speichern.
2. `/euer`: Ausgaben ohne EÜR-Zeile, Vorschlag aus der letzten Ausgabe desselben Lieferanten („wie zuletzt bei diesem Lieferanten“), nichts vorab angehakt; Dropdown-Feld als Auswahl. Anhaken, „Prüfen“ → „In Invoice Ninja schreiben“ (Demo lehnt ab). Ohne gewähltes Feld: Hinweis mit Link zu den Kategorien.
3. `/vat`: „Neue Ausgabe“ hat eine EÜR-Auswahl, vorbelegt mit der Zeile des Lieferanten; die Prüfung zeigt sie, ein Wert außerhalb des Dropdowns wird übersprungen.

**J-C3 Schreib-Journal**
1. Kontomenü → „Schreib-Journal“: nach einem Abgleich auf `/vat` und einer Verknüpfung auf `/receipts` stehen beide oben; Bereichs-Chips filtern.
2. „Rückgängig“ bei „als privat archiviert“ zeigt den Umsatz auf `/vat` wieder, bei „Beleg verknüpft“ löst es beide Seiten; das Zurücknehmen steht danach selbst im Journal.
3. Hinweis „Ausgabe nicht erfasst“ → „Im USt-Abgleich erledigen“ öffnet `/vat` beim Zeitraum der Buchung.

**J-C2 Belege**
1. `/receipts`, Reiter „Zu verknüpfen“ (Belegjagd): je Ausgabe einen Beleg, einen Sammelbeleg oder eine Rechnungsmail wählen (sichere Treffer vorgewählt, j/k und Enter wählen) → „Auswahl prüfen“ zeigt jeden Schritt → „Schreiben“ (Demo lehnt ab: nur Fehler, kein Erfolgshinweis daneben).
   - Beleg-Suche von Hand, „Belege zuerst“ und „Als Ausgabe anlegen“ führen ebenfalls auf die Prüfseite; dort „Belege von Claude prüfen lassen“ (nur mit KI).
   - `/vat` → Stand → „Ausgaben ohne Beleg“ öffnet `/receipts` für denselben Zeitraum; der Chip „Zeitraum …“ hebt ihn auf.
   - Mehrere Verbünde mit Invoice Ninja und Paperless: Auswahl „Verbund“ oben, die Wahl bleibt; mehrere Paperless ohne Verbund: Hinweis „Welche gehören zusammen?“ mit Link zu den Verbünden. Dasselbe auf `/vat`.
2. Reiter „Belege zuerst“: Ausgaben suchen, „Als Ausgabe anlegen“ → „Anlegen und verknüpfen“; bleibt der Reiter?
3. Reiter „Verknüpft“. Zähler auf allen Reitern und Jahren stimmen mit den Listen überein, auch „… Belege ohne Ausgabe“.
4. Eine Ausgabe aus dem Jahrespaket (`/billing`, `ausgaben.csv`) unter „Verknüpft“ oder bei „Ausgaben suchen“ finden.

**J-C2b USt-Abgleich** 📱
1. `/vat`: Welcher Zeitraum öffnet (der letzte bis zu seiner Abgabefrist, danach der laufende)? Die Zeitraum-Karten (älteste links) nennen je Stand, Frist und Zahllast: offen mit Zahl, abgegeben, Korrektur fällig, nicht markiert, läuft. Stimmt die gewählte Karte mit dem Stand darunter? Zeitraum wechseln, zurück; am Telefon brechen die Karten um.
2. Stand: offene Umsätze, Bankabruf bis, Ausgaben ohne Steuer, „Nur in Sure“; Umsatzsteuer, Vorsteuer, Zahllast „vorläufig“, solange etwas offen ist.
3. Werkbank: links die Liste (Kopf: offen, ohne Beleg, ohne EÜR-Zeile, fertig), je Eintrag Aktion und Teile („5 / 9“, Balken); rechts der gewählte Umsatz. `j`/`k` wechseln (nicht in Feldern). Aktion oben umschalten: Es stehen nur ihre Felder da, der Eintrag zählt neu. Vorbelegung (Lieferant, Kategorie, Steuer, EÜR, Rechnungsnr., Notiz) und ihre Herkunft prüfen; nicht eingerichtete Felder (EÜR, Rechnungsnr.) fehlen und zählen nicht.
   - Beleg: Vorschläge mit Vorschau und Treffer; „Ohne Beleg“; Suche (Enter sucht, schickt nichts ab) bringt weitere zum Wählen; ein Beleg mit Nummer füllt die Rechnungsnr., solange keine eigene getippt ist.
   - „Aus dem Umsatz“: Betrag, Währung, Tag; „enthält Steuer“ und „bezahlt“ an, „in Rechnung stellen“ wie die letzte Ausgabe des Lieferanten.
   - „Übernehmen und weiter“ merkt „zur Prüfung“ vor und springt weiter, „Später“ nimmt es heraus.
   - „Vorhandener zuordnen“: unter der gewählten Ausgabe „Fehlt EX-…“ nur mit dem, was ihr fehlt (Beleg, Kategorie, EÜR, Rechnungsnr., Notiz), vorbelegt; fehlt nichts, steht „Der Ausgabe fehlt nichts.“ Der Zähler zählt die Lücken mit. Passen mehrere Ausgaben, zählt die vorgewählte; eine andere wählen zeigt nur deren Lücken.
   - „Ausgaben mit Lücken“ unten in der Liste: verknüpfte Ausgaben des Zeitraums ohne EÜR-Zeile oder Beleg, Aktion „Ergänzen“; Demo Oktober: Elbnetz bekommt Beleg (Treffer 100) und Rechnungsnr., die Prüfung nennt „Ausgabe EX-0044 ergänzen“ mit beidem. Ein Wert, den die Ausgabe schon hat, wird nie überschrieben.
   - „Belege ohne Ausgabe“ darunter: Belege mit Beleg-Tag, die weder eine Ausgabe noch ein Umsatz beansprucht (bar oder privat bezahlt), Aktion „Aus Beleg anlegen“; Demo: Quittung Copyshop Altona. Beleg fest gewählt, Betrag aus dem Beleg änderbar, Tag des Belegs, Notiz mit Herkunft „Beleg“. Die Prüfung nennt „Ausgabe … aus dem Beleg anlegen“ und keine Umsatz-Verknüpfung. Ein Beleg, zu dem ein offener Umsatz passt (Tankquittung; auch aus einem anderen Zeitraum, z. B. Kartenzahlung am Monatsende), steht nicht hier, ebenso einer, zu dem eine Ausgabe aus den ersten Tagen des Folgejahrs passt. Beleg-Fälle sind nie vorgemerkt: „bar/privat bezahlt“ bestätigt man mit „Übernehmen“; ohne Betrag sind sie nicht fertig. Passt zum getippten Betrag ein offener Umsatz, sagt die Prüfung „dort zuordnen“.
4. „Prüfen“ (alle vorgemerkten) oder „Alle fertigen prüfen“ (nur fertige): Die Liste nennt jeden Schreibvorgang mit Vorsteuer; nichts ist geschrieben. „In Invoice Ninja schreiben“: Demo lehnt ab. „Privat“ archiviert in Invoice Ninja (auch ein Schreibvorgang in der Prüfung).
5. Konten: Geschäftskonto angehakt, Privatkonto nicht. Deutschlandticket und Einkauf stehen unten in der Liste unter „Privat oder gemischt, ohne Vorschlag“, die Erstattung fehlt; „Alle als privat archivieren“ prüft nur diese. Ein Konto umstellen, speichern: Liste und Stand ändern sich. Ein Depot in Sure steht als „ignoriert (Vorschlag)“; gespeichert fehlt es in Kontostand, Stand und Hinweisen, steht aber weiter in der Liste. Kreditkarte in Sure: „Verrechnung (Vorschlag)“; die Kartenabrechnung vom Girokonto steht als Umbuchung, die Käufe auf der Karte als Ausgaben. Privatkonto auf „gemischt“: Eingänge erscheinen, Abbuchungen ohne bekannten Lieferanten stehen unter „Privat oder gemischt, ohne Vorschlag“.
6. Mit API-Schlüssel: „Claude um Vorschläge bitten“ (auch für die Privatkonto-Zeilen); was ändert sich, was bleibt (Verlauf eines Lieferanten gewinnt)?
7. Kachel „Belege ohne Ausgabe“ und Monatsabschluss verlinken auf `/vat`.
8. Tankquittung: „Neue Ausgabe“ bietet den Beleg an (Treffer 100); in der Prüfung „mit Beleg … verknüpfen“. September: ein Eingang mit „Zur Zahlung“. Die Prüfung nennt Rechnungsnr., Notiz und was vom Standard abweicht (in Rechnung stellen, nicht bezahlt, ohne Steuer).
9. Kennzahl-Kachel „Kontostand“: Feld „Konten“ auf Geschäftskonten, Privatkonten, alle stellen; der Betrag folgt.

**J-C3 Kunden und Verbund**
1. `/clients`, ein Kunde (der gewählte Zeitraum bleibt), „Verknüpfung ändern“ → `/spaces/{id}/verbund#implicit`.
2. „Kunden“ → `/verbund/{id}/customers`; „Alle Vorschläge bestätigen“, eine Zelle lösen, „Namen angleichen“.
3. `/billing` und die Kundenseite danach.
4. „Konten“ → `/verbund/{id}/accounts`: Geschäftskonto in Ninja und Sure vorgeschlagen (gemeinsame Buchungen), bestätigen; Verwendung eines Kontos ändern: das verknüpfte folgt, `/vat` zeigt es. Tagesgeld unter „Ohne Gegenstück“.

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
3. In den ersten sieben Tagen des Monats: Hinweis „Laden für Geschäftsfahrten“ (Wallbox im persönlichen Bereich) nennt Ladevorgänge, kWh, Kosten und den geschäftlichen Anteil; passen die km zu den Fahrten des Vormonats?

**J-C6 Vom Geld-Hinweis zur Lösung**
1. `/hints?sort=value`, jeder Geld-Hinweis: führt die Aktion an die Stelle, wo man ihn behebt (in Andon, wenn Andon es kann)?

## D. Boards bauen (Demo, Mara)

**J-D1 Board und Kacheln anlegen** 📱
1. `/boards/new`: Name leer, nur Leerzeichen, 300 Zeichen, vorhandener Name, „QA Board“; aus Vorlage.
2. Bearbeiten: „+ Abschnitt“, „Kachel hinzufügen“ → Galerie (Vorschauen ohne Fehler?) → Uhr → Zonen `Europe/Berlin, America/New_York, Not/AZone`, 24 h.
3. Auf dem Board: Format wie gewählt; ↕, ↔, ×; Schnell-Link `example.org`, dann `not a url` (Meldung, kein 500).

**J-D2 Rückgängig, Verlauf, Wiederherstellen**
1. Zwei Tabs, B fügt Abschnitt hinzu, A speichert veraltet (Abschnitt anlegen, bearbeiten, Schnell-Link, Kachel): Meldung, neuer Stand, das Getippte steht noch da; erneut speichern geht.
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
1. Eigene Rolle: steht als Text da, keine Auswahl.
2. Jonas befördern, zurückstufen, deaktivieren (Sitzung endet), reaktivieren, Reset-Link, Benutzer löschen.
3. `/admin/users/9999/reapply` (404, kein 500).
4. Als Mara POST auf `/admin/users/*`, `/admin/invite`, `/admin/settings/general`: 403.

**J-E3 Rechte als Betrachter (Jonas)**
1. `/teams`, `/spaces/{id}/settings/page`, `/spaces/{id}/connections`, `/spaces/{id}/verbund`, `/boards/1…5`, `/connections/{id}/edit`, `/shares/board/1`.
2. POST auf Einstellungen, Verbindungen, Geheimnisse, Webhook-Rotation: alles abgelehnt, mit Fehlerseite in der UI-Sprache (Grund, Zurück). Sichtbare Speichern-, Test- oder Bearbeiten-Knöpfe ohne Recht notieren; Instanz-Einstellungen (`/spaces/1/…`) als Nicht-Admin: 403.

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
2. Homelable: Reiter Abgleich zeigt den Lauf nach dem Prüflauf (Zähler, Quelle Gitea); „Jetzt abgleichen“ landet wieder dort, der zweite Lauf ändert nichts (alles unverändert). Bei `demo://` kein „In Homelable öffnen“.
3. Umziehen: in Einstellungen eine Adresse auf einem anderen Server eintragen → Name und Rest gespeichert, weiter auf „Umziehen“ mit der neuen Adresse. Feste Verbindung: „Gespeicherten Zugang senden“ oder neuer Zugang; Ziel, das nicht antwortet → Testergebnis, nichts gespeichert, Häkchen „Ohne erfolgreichen Test“. Danach: gleiche Kacheln, Verbund, Verlauf. Vorlage: Mara sieht ihren Zugang pausiert mit „Zugang dorthin senden“. Eine Adresse, die auf einen anderen Server umleitet (Test oder letzter Fehler), bietet „Auf die neue Adresse umziehen“ an.
4. Übernehmen: zweite Verbindung desselben Dienstes im selben Bereich anlegen; in ihren Einstellungen „Von einer anderen Verbindung übernehmen“ (Rückfrage) → Kacheln, Verbund-Zuordnungen, Hinweise mit Notizen, Verlauf und Freigaben hängen an der neuen, die alte ist weg (404), Meldung nennt den Verbund zum Prüfen. Ohne zweite Verbindung kein Abschnitt.

**J-E8 Server, Betrieb, Audit**
1. `/admin/settings`: ungültige Zahlen und URLs, CIDR „not-a-cidr“, `javascript:` als iframe-Herkunft, OIDC-Test leer.
2. Mit `SMTP_URL`/`ANALYSIS_MINUTES` in der Umgebung neu starten: Felder gesperrt, POST ändert nichts.
3. `/admin/operations`, `/admin/audit`: rohe Schlüssel, Leerlauf-Einträge?
4. Update-Hinweis (ohne Demo, Build mit älterer Version, `data/release.json` mit neuerer): Lena sieht über jeder Seite „Andon x ist erschienen“ mit Link, Mara nicht. × blendet ihn bis zur nächsten Version aus; `ANDON_UPDATE_CHECK` auf „aus“ nimmt ihn sofort weg. Im Demo-Modus nie.
