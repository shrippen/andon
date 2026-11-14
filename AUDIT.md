# Code-Audit andon, 14.11.2026, Stand 9cf9149

## Urteil
Andon ist wartbar. Die Schichten halten ihre Richtung ein: Regeln lesen nur Kennzahlen, Kennzahlen nur Datensätze, die Web-Schicht ruft fast nur Dienste auf, und fast jedes Paket hat Tests. Teuer wird die nächste Änderung durch Wiederholung und durch Absprachen in freiem Text: Namen von Einstellungen und Messreihen stehen an mehreren Stellen von Hand, dieselben kleinen Hilfen und Abläufe sind mehrfach geschrieben, und einige Dateien sind weit über der Grenze von 800 Zeilen. Dies ist das erste Audit, einen Vergleich gibt es nicht. Wegen der Größe haben vier Agenten je einen Teil geprüft; aus ihren 23 Funden stehen hier die zehn mit dem besten Verhältnis von Nutzen zu Aufwand, die übrigen nennt der PR zu diesem Bericht. Eine `ARCHITECTURE.md` fehlt; geprüft wurde gegen `agent.md` und den Schichttest des Repos.

## Kennzahlen
| | letztes Audit | jetzt |
|---|---|---|
| Quelldateien | – | 1253 |
| Einträge in der Bestandsliste | – | 224 |
| größte Datei (Zeilen) | – | 3528 (Hauptskript der Oberfläche); größte Go-Datei 1422 |
| längste Funktion (Zeilen) | – | 3527 (die eine Hülle um das Hauptskript); längste Go-Funktion 380 |

## Funde

### 1. Die Namen der gespeicherten Instanzeinstellungen stehen doppelt im Code, teils als bloßer Text
Andon legt Einstellungen der Instanz in einer Tabelle ab: ein Name, darunter ein Wert. Der Baustein dafür nimmt jeden beliebigen Namen an. 13 Dateien greifen darauf zu, und jede bestimmt den Namen selbst. Vier Namen stehen an zwei Stellen:
- „security“ ist als Konstante (fester, benannter Wert) in `system` angelegt, in `auth` aber noch einmal als bloßer Text geschrieben.
- „oidc“ ist eine Konstante in `oidc`; `auth` schreibt den Namen ebenfalls als Text.
- „registration“ ist in `admin` und in `system` jeweils eine eigene Konstante.
- „dawarich_shared“ ist in `connections` und in `system` jeweils eine eigene Konstante.

Wer einen dieser Namen an einer Stelle umbenennt, liest an der anderen ab sofort einen leeren Wert. Es gibt keine Fehlermeldung, und kein Test fällt auf. Dazu legt die Datenbankschicht in derselben Tabelle auch den Zustand einzelner Verbindungen ab (Homelable-Abgleich, Webhooks), mit einem eigenen Präfix im Namen.

Vorschlag: Jeder Name wird an genau einer Stelle festgelegt, am besten neben dem Speicherbaustein. Die übrigen Stellen benutzen diese Konstante.

Aufwand klein. Risiko gering: Es ändert sich nur, woher der Name kommt; es gibt Tests zu `auth`, `system` und `oidc`.

Beleg: `internal/repos/misc/misc.go:298`, `internal/services/auth/auth.go:291`, `internal/services/auth/auth.go:767`, `internal/services/system/system.go:41`, `internal/services/system/system.go:42`, `internal/services/system/system.go:43`, `internal/services/oidc/oidc.go:40`, `internal/services/admin/admin.go:35`, `internal/services/connections/connections.go:40`, `internal/repos/data/syncstate.go:12`, `internal/repos/data/hooks.go:55`.

### 2. Zwölf Regeln lesen die Systemuhr, obwohl der Analyselauf ihnen die Zeit mitgibt.
Jede Regel bekommt beim Lauf den Zeitpunkt des Laufs mit, unter dem Namen `Today`. Der Name ist irreführend, denn der Wert ist die genaue Uhrzeit, nicht nur der Tag. 145 Stellen nutzen ihn. Zwölf Regeln, darunter „Sicherung zu alt“ und „Sicherungsdienst meldet sich nicht“, fragen stattdessen die Uhr des Rechners. Dieselbe Art Rechnung („Tage seit dem letzten Ereignis“) steht so einmal mit der Laufzeit und einmal mit der Systemuhr im Code, sogar in derselben Datei. Für diese Regeln kann ein Test die Zeit nicht festlegen, und der Grundsatz „Regeln tun nichts außer rechnen“ aus `base.go` stimmt für sie nicht. Vorschlag: `Today` in `Now` umbenennen und die zwölf Stellen darauf umstellen. Aufwand klein. Risiko gering, die betroffenen Regeln haben Tests. Diese müssen dann die Zeit selbst setzen. Beleg: `internal/rules/base.go:62`, `cmd/andon/jobs.go:35`, `internal/rules/devops.go:52`, `internal/rules/devops.go:191`, `internal/rules/pgbackweb.go:37`, `internal/rules/pgbackweb.go:57`, `internal/rules/hass.go:80`, `internal/rules/ci.go:47`, `internal/rules/kimai.go:51`.

### 3. Eine Zahl aus einer Einstellung wird in den Kacheln auf vier verschiedene Arten gelesen
Eine Einstellung kommt als „irgendein Wert“ an und muss in eine Zahl umgewandelt werden. Die Kacheln haben dafür vier eigene kleine Hilfsfunktionen. Zwei davon sind wörtlich gleich und erkennen nur Kommazahlen. Die dritte erkennt auch ganze Zahlen. Die vierte liest zusätzlich Text, auch mit deutschem Komma. Ob eine Grenze wie „80“ ankommt oder still als 0 gelesen wird, hängt also davon ab, welche Hilfsfunktion der Autor zuerst gefunden hat. Wer eine neue Einstellung einbaut, muss erst alle vier vergleichen. Vorschlag: eine Funktion behalten, die alle Fälle kann, und die anderen drei durch sie ersetzen. Aufwand klein, Risiko niedrig: Mehrere Kacheln haben Tests, aber keiner prüft die Umwandlung selbst. Ein kurzer Test kommt deshalb zuerst. Beleg: `internal/widgets/start.go:69`, `internal/widgets/crossfin.go:195`, `internal/widgets/detail_parts.go:184`, `internal/widgets/thresholds.go:94`.

### 4. Zwei Dienste schreiben an der Datenbankschicht vorbei direkt in Tabellen.
Nach den Regeln des Repos spricht die Anwendungslogik nur über die Datenbankschicht (`repos`) mit der Datenbank. Das Umbenennen eines Teams und das Ändern eines Freigaberechts schreiben ihren Datenbankbefehl aber selbst. Wer die Tabellen `teams` oder `shares` ändert, findet diese beiden Stellen bei der Suche in der Datenbankschicht nicht.
Vorschlag: Beide Befehle als Funktionen in die passenden Pakete der Datenbankschicht verschieben (`users`, `misc`).
Aufwand: klein. Risiko: gering; `teams` und `shares` haben Tests.
Beleg: `internal/services/teams/teams.go:277`, `internal/services/shares/shares.go:254`.

### 5. Die automatische Schichtprüfung schützt nur die oberen Schichten
Ein Test prüft, dass keine Schicht eine weiter unten liegende überspringt. Regeln hat er aber nur für `web`, `services`, `widgets`, `metrics` und `rules`. Für `repos`, `sources`, `outbound` und `drivers` gibt es keine Regel. Wenn dort jemand eine Anwendungsfunktion oder die Web-Schicht einbindet, also nach oben greift, meldet niemand etwas.

Eine solche Verbindung besteht schon: `outbound` (Schreiben an fremde Dienste) benutzt die Arten von Knoten und Kanten aus `metrics` (Auswertungen). Zugleich beschreibt der Kopf des Pakets `outbound` es noch als reinen Apprise-Versand. `agent.md` sagt dasselbe.

Vorschlag: Der Test bekommt Regeln für die vier unteren Schichten. Die Grafik-Arten wandern entweder in einen neutralen Baustein, oder die Verbindung wird ausdrücklich erlaubt. Die Beschreibung von `outbound` wird an den heutigen Umfang angepasst.

Aufwand klein. Risiko gering: Der Test selbst ist die Absicherung.

Beleg: `internal/arch/layers_test.go:19`, `internal/outbound/homelable.go:27`, `internal/outbound/homelable.go:56`, `internal/outbound/notify.go:1`.

### 6. Die Namen der Messreihen im Verlauf stehen als freier Text an der Stelle, die schreibt, und an den Stellen, die lesen.
Andon speichert bei jedem Lauf Zahlen wie „belegter Anteil des Speicherpools tank“ oder „Zahl der Fotos in Immich“ unter einem Namen wie `immich.items`. Diesen Namen setzt eine Datei aus Wortteilen zusammen, wenn sie schreibt. Wer liest, also die Detailansichten der Kacheln und die Speicherprognose, tippt denselben Namen noch einmal ab, mal in Teilen, mal als fertigen Text. Benennt jemand eine Reihe um oder ändert die Schreibweise, bricht kein Test und kein Übersetzer. Das Diagramm bleibt dann einfach leer, und die Prognose rechnet ohne diese Reihe. Für einige Reihen gibt es schon benannte Namensfestlegungen (Konstanten, also feste Namen im Code), zum Beispiel für den Stromverbrauch. Vorschlag: Für jede Reihe, die außerhalb der schreibenden Datei gelesen wird, eine solche benannte Festlegung oder kleine Funktion in `metrics` anlegen und an beiden Seiten benutzen. Aufwand klein. Risiko gering, `history_test.go` und `record_test.go` prüfen das Schreiben. Beleg: `internal/metrics/history.go:162`, `internal/metrics/history.go:215`, `internal/metrics/history.go:255`, `internal/metrics/homelabx.go:146`, `internal/widgets/detail_homelab.go:495`, `internal/widgets/detail_homelab.go:931`, `internal/widgets/detail_money.go:472`, `internal/widgets/catalog.go:568`.

### 7. Die Form der Werte in Hinweisen und Infozeilen (Geldbetrag, Datum, Zahl) wird an fünf Stellen je für sich gebaut.
Ein Hinweis trägt Werte wie „12,50 €“ nicht als fertigen Text, sondern als kleine Beschreibung („Geld: 12.5, Währung EUR“). Die Übersetzung macht daraus je nach Sprache den richtigen Text. Diese Beschreibung bauen die Regeln mit eigenen Hilfsfunktionen, die Kennzahlen mit anderen, die Infozeilen von Hand an mindestens zehn Stellen, die Kacheln und die Belegprüfung mit noch einmal eigenen. Schon jetzt verhalten sie sich verschieden: Die Regeln runden auf Cent und setzen Euro ein, wenn keine Währung angegeben ist. Die Kacheln tun beides nicht, und die Belegprüfung lässt die Währung ganz weg. Wer eine neue Art Wert einführt oder die Form ändert, muss alle Stellen finden. `agent.md` verweist auf Hilfsfunktionen `money()`, `day()`, `num()` in `internal/i18n`, die es dort nicht gibt. Vorschlag: Die Hilfsfunktionen einmal in `internal/i18n` anlegen, neben dem Leser `Typed`, und alle Stellen darauf umstellen. Aufwand klein bis mittel. Risiko gering, Regeln und Kennzahlen haben Tests. Beleg: `internal/rules/base.go:256`, `internal/rules/base.go:264`, `internal/rules/base.go:278`, `internal/metrics/story.go:55`, `internal/metrics/story.go:59`, `internal/metrics/ops.go:79`, `internal/metrics/ops.go:109`, `internal/metrics/info.go:40`, `internal/widgets/detailbody.go:205`, `internal/services/receipts/match.go:257`, `internal/i18n/i18n.go:154`.

### 8. „Zertifikat prüfen ja/nein“ hat in den Treibern vier verschiedene Formen
Ob Andon das HTTPS-Zertifikat eines Dienstes prüft, steht in der Verbindung als „ja/nein“. Auf dem Weg zum Netz wechselt diese Angabe mehrmals ihre Form:
- `Verify` (ja heißt prüfen) in den meisten Treibern,
- `VerifyTLS` in der Verbindung und in `outbound`,
- `SkipVerify` (ja heißt *nicht* prüfen) im HTTP-Baustein,
- ein Aufzählungstyp `TLS` (fester Satz benannter Werte) in einigen Treibern.

Dazwischen stehen Umrechnungen hin und zurück. Manche Treiber negieren direkt (`SkipVerify: !a.Verify`), andere rechnen über den Aufzählungstyp. Ein Kommentar warnt eigens, der Wert sei „nicht umgedreht“; das zeigt, wie leicht hier ein Fehler passiert. Jede neue Anbindung muss die Umrechnung erneut richtig treffen. Ein vertauschtes Zeichen schaltet die Prüfung still ab. Zudem verlangt `RULES.md` Aufzählungstypen statt ja/nein-Werten als Parameter.

Vorschlag: Der Aufzählungstyp `httpclient.TLS` gilt durchgehend, von der Verbindung bis zum HTTP-Baustein; die ja/nein-Felder entfallen.

Aufwand mittel, weil er rund 30 Treiber und die Aufrufer in `sources` und `outbound` berührt. Risiko mittel: Der HTTP-Baustein hat Tests, viele Treiber haben keine eigenen.

Beleg: `internal/drivers/services/services.go:91`, `internal/drivers/services/services.go:107`, `internal/drivers/services/homelab.go:118`, `internal/drivers/services/keyed.go:83`, `internal/drivers/services/backups.go:63`, `internal/drivers/httpclient/httpclient.go:284`, `internal/drivers/httpclient/httpclient.go:309`, `internal/outbound/target.go:7`, `internal/model/model.go:102`.

### 9. Zwölf Werkbänke haben denselben Ladeablauf kopiert.
Eine Werkbank ist eine Seite, auf der man Änderungen prüft und dann in einen Dienst schreibt, etwa Mahnungen, Feeds oder Anschaffungen. Jede Werkbank beginnt mit denselben Schritten: passende Verbünde suchen, die zuletzt gewählte Auswahl lesen und einen Verbund wählen. Diese Abfolge steht zwölfmal fast gleich im Code. Danach folgt an 13 Stellen dieselbe Regel: Sind frische Daten noch nicht da, nimm die zwischengespeicherten. Soll sich diese Regel ändern, etwa damit die Seite „lädt noch“ anzeigt, muss man 13 Stellen finden und gleich ändern. Dazu kommt: Elf der zwölf Werkbänke geben eine Fehlerbegründung nur unverändert weiter, schreiben dafür aber jede eine eigene Methode.
Vorschlag: Die Verbundwahl und die Regel „frisch, sonst zwischengespeichert“ je einmal als Funktion ins Paket `workbench` legen. Die Fehlerbegründung bekommt dort eine Standardfassung, die nur die Mehrwertsteuer-Werkbank ersetzt.
Aufwand: mittel (viele Dateien, je wenige Zeilen). Risiko: gering; jede Werkbank hat Tests.
Beleg: `internal/services/feeds/feeds.go:98`–`internal/services/feeds/feeds.go:121`, `internal/services/vendors/vendors.go:91`–`internal/services/vendors/vendors.go:111`, `internal/services/dunning/dunning.go:124`–`internal/services/dunning/dunning.go:143`, `internal/services/purchases/purchases.go:122`–`internal/services/purchases/purchases.go:147`, weitere Kopien der Regel in `internal/services/euer/euer.go:162`, `internal/services/categorize/categorize.go:128`, `internal/services/billing/travel.go:66`. Die Fehlerbegründung: `internal/services/feeds/feeds.go:242`, `internal/services/dunning/dunning.go:274`, abweichend nur `internal/services/vatcheck/write.go:517`.

### 10. Bestandsliste: was sich an einer Naht trennen lässt und was bleiben kann
Die Bestandsliste ist die Datei `structure.baseline`. Sie nennt die Stellen, die heute über den Grenzen der Strukturprüfung liegen (Länge, Verzweigungen, Verschachtelung); rund die Hälfte der 224 Einträge sind Tests.

An einer Naht trennen sich:
- Die Funktion, die jede Kachel lädt (380 Zeilen, 120 Verzweigungen), ist der größte Eintrag in Go. Sie hat für jede der 17 Sonderarten von Kacheln einen eigenen Abschnitt; je Sonderart eine kleine Ladefunktion in einer Tabelle löst sie auf. Aufwand mittel, Tests vorhanden.
- Das Hauptskript der Oberfläche (3528 Zeilen) enthält die Umsatzsteuer-Werkbank und die Wandanzeige, die nur je eine Seite braucht; beide können in eigene Dateien. Aufwand mittel, Risiko mittel: Es gibt nur Browser-Prüfungen.
- Die Sammeldatei der Detail-Fenster für das Heimlabor (1422 Zeilen) zerfällt, wenn jedes Detail-Fenster in die Datei seiner Kachel zieht.
- Die Anmeldung (945 Zeilen) erledigt drei Aufgaben: Anmeldung und Sitzungen, Zwei-Faktor-Codes, Zugangsschlüssel für Programme.
- Der Programmstart (167 Zeilen) trennt sich in „vorbereiten“ und „bedienen und herunterfahren“.
- Das Laden aus Invoice Ninja (150 Zeilen) lädt sieben Arten von Datensätzen nacheinander; je Art ein Schritt.

Bleiben können: die flachen Listen (je Dienst eine Zeile für die Link-Kachel, die Aufzeichner des Verlaufs, die Tabelle der Hilfsfunktionen für die Seitenvorlagen) und die langen Tests, die je einen ganzen Ablauf durchspielen.

Aufwand je Naht klein bis mittel. Risiko gering, wo Tests vorhanden sind. Beleg: `internal/services/widgetlib/display.go:250`, `internal/web/static/andon.js:2`, `internal/web/static/andon.js:1285`, `internal/web/static/andon.js:2580`, `internal/widgets/detail_homelab.go:36`, `internal/services/auth/auth.go:526`, `internal/services/auth/auth.go:797`, `cmd/andon/main.go:51`, `internal/sources/ninja.go:213`, `internal/widgets/start.go:206`, `internal/metrics/history.go:158`, `internal/web/render.go:40`.

## Seit dem letzten Audit
Erstes Audit.
