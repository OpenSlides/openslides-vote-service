# Neues Konzept

Das neue Konzept speichert die Daten ausschließlich in der Datenbank. Die nötigen Änderungen sind hier:
https://github.com/OpenSlides/openslides-meta/pull/289

Das neue Konzept basiert auf einer Dreiteilung.

  config -> vote -> result

Jedes dieser Felder ist ein JSON objekt. Das format ist Abhängig von
`poll/method`. Ziel ist, dass in Zukunft neue Methods eingefügt werden können,
ohne das Datenbankschema anpassen zu müssen. Zum Beispiel single transferable
vote. Für jeden Type kann es eine beliebige config geben, es werden eine
bestimmte vote-Objekte erwartet und es entsteht daraus ein bestimmtes result.


## Zusammenspiel mit Backend und anderen Services

Der Vote service übernimmt die "Actions" poll_start und poll_stop. Außerdem
werden weiterhin die votes der Clients angenommen.

Die Aufgabe des Backends ist es, ein valides poll-objekt zu erstellen. Nur der
Vote-Service das das Feld `poll/state` auf `started` setzen. Solange das Feld
auf `started` gesetzt ist, ist der Vote-Service verantwortlich und das Backend
darf das Objekt nicht anfassen. Das Backend sollte daher bei allen schreibenden
Operationen kontrollieren, dass der state nicht `started` ist. Zum Beispiel mit
`[...] WHERE state != "started"`.

Für die `poll.reset` action bedeutet das, dass eine laufende Poll nicht resettet
werden kann. Es spricht jedoch nichts dagegen, dass der Client über einen button
erst poll-stop beim vote-service ausführt und anschließend poll.reset beim
backend. Sollte es zur Fehlerbehebung erforderlich sein, könnte beim
vote-service ein `poll/cancel` handler eingeführt werden, oder die Bedingung,
dass das Backend keine poll mit `state = "started"` bearbeiten darf, gilt für
`poll.reset` nicht.

Nachdem der vote-service den `poll/state` auf `finished` gesetzt hat, geht die
Verantwortung zurück an das Backend. Ab diesem Moment ist das Feld `poll/result`
gesetzt (durch den vote-service) und kann durch das Backend oder andere Services
ausgewertet werden. Ziel ist, dass zur Auswertung einer poll nur das Feld
`poll/result` gelesen werden muss (in abhängigkeit von `poll/method`).


## Handler

Alle Handler sind abhängig von einer Poll-ID. Diese wird als http-argument
`poll_id=XX` übergeben.


### /vote/start

Validiert das Poll Objekt, vor allem poll/config, und setzt das Feld `poll/state` auf `started`


### /vote/stop

Erstellt `poll/result` und setzt `poll/state` auf `finished`.


### /vote

Nimmt ein Vote-Objekt als Request-Body entgegen und validiert dieses abhängig
von `poll/method` und `poll/config`. Anschließend wird dieses in die
`vote`-collection gespeichert. Hierbei wird in einer transaction sichergestellt,
dass die Abstimmung im state `started` ist und es zu der Poll vom user keine
andere Stimme gibt.


### Andere bisherige Handler

Bisher gab es folgende weiteren Handler, die jedoch nicht ins neue Konzept
übernommen werden:

- clear und clear_all: Da der Vote-Service keinen internen state mehr hat, muss
nichts gelöscht werden. Das Löschen eines Poll-Objekts läuft über das backend.

- all_voted_ids / live_votes: Da die Daten in den normalen Collections
gespeichert sind, kann die Information, wer abgestimmt hat bzw. wie abgestimmt
wurde, über den autoupdate-service geladen werden. Der Restrictor muss
entsprechend angepasst werden.

- voted: Über diesen Handler konnte für eine Liste von polls abgerufen werden,
ob man bereits abgestimmt hat. Auch dies läuft in Zukunft über den
autoupdate-service.


## Weitere Features

### Analoge Abstimmungen

Der Vote-Service ist nur für elektronische Abstimmungen verantwortlich. Analoge
Abstimmungen werden vom Backend bearbeitet. Die Daten müssen jedoch entsprechend
dem neuen Konzept gespeichert werden, daher als ein json-blob in `poll/result`.
Über `poll/config` kann dem Client mitgeteilt werden, welche Felder es gibt, die
manuell ausgefüllt werden können.

Ob eine Abstimmung analog ist, wird über das Feld `poll/method` geregelt. Dies
entspricht der Regelung, dass `poll/config` und `poll/result` abhängig von
diesem Feld ausgewertet werden müssen.


### poll/visibility

Das Feld `poll/visibility` entspricht dem alten Feld `poll/type`. Es
entscheidet, ob die Abstimmung namentlich, geheim oder offen ist.

Der Wert `named` bedeutet, dass die Zuordnung von den votes zu den Nutzern am
Ende nicht gelöscht werden. Im politischen Kontext bedeutet namentliche
Abstimmung auch, dass die Wahlberechtigten einzeln, öffentlich und nacheinander
aufgrufen und nach ihrer Stimme gefragt werden. In Zukunft kann über ein Feature
nachgedacht werden, dass bei namentlichen Abstimmungen die User nicht selbst
abstimmen können, sondern der Sitzungsleiter durch ein Formular geführt wird, in
dem er nacheinander die Stimmen für alle Wahlberechtigten eintragen kann.
Datenbanktechnisch könnte ein solches Feature sehr leicht über delegation und
normale `vote`-Requests abgebildet werden, weshalb ein solches Feature
unabhängig vom vote-service implementiert werden kann.

Der Wert `open` dürfte der Normalfall einer Abstimmung sein. Die Zuordnung von
den Votes zu den Usern KANN im Nachgang gelöscht werden. Hierfür bietet das
Backend eine Action zum anonymisieren an, bei dem die Felder
`vote/acting_user_id` und `represented_user_id` gelöscht werden. Die Action KANN
vom Client in einem einheitlichen Button ausgelöst werden, welcher die
Abstimmung beendet und veröffentlicht. Dies könnte für live-votes interessant
sein. Die Aktion kann in anderen Workflows aber auch separat aufgerufen werden.
Werden die Daten nicht anonymisiert und gibt es bei der namentlichen Abstimmung
keinen Einzelabruf, dann ist die Bedeutung von `open` und `namend` identisch.

Der Wert `secret` bedeutet, dass eine crypto-Wahl nach dem im Wiki beschrieben
Konzept durchgeführt werden soll. Bei dieser Methode werden zwar alle Daten
primär im Bulletin Board gespeichert, zusätzlich jedoch auch in der hier
beschriebenen Datenstruktur, damit die anderen Services in ihren normalen
Abläufen darauf zugreifen können.

Bis crypto-vote implementiert ist, wird bei `secret` die Stimme und die User-ID
mit einer Zuordnung gespeichert. Über den restricter muss sichergestellt werden,
dass die Information nicht nach außen dringt. Solange openslides ordnungsgemäßg
betrieben wird, kann abgesehen vom server-admin niemand herausfinden, wer wie
abgestimmt hat.


### Live Abstimmungen

Da jede Vote mit der User-ID in der Collection gespeichert wird, kann das
Live-Voting über den autoupdate-restricter implementiret wierden.


### Verschiedene Einstellungen

Alle Einstellungsmöglichkeiten werden in `poll/config` gespeichert und können
bei unterschiedlichen Werten von `poll/method` unterschiedlich sein. Darunter
fällt auch, ob Enthaltungen zulässig sind (YNA anstatt von YN) oder die
aktuellen Felder `poll/min_votes_amount`, `poll/max_votes_amount` oder
`poll/max_votes_per_option`.


### 100% basis

Auch die Grundlage für die 100% basis wird über `poll/config` geregelt. Der
absolute Wert der bases (z. B. 400 Nutzer) wird am Ende in `poll/result`
gespeichert, damit das Ergebnis angezeigt werden kann, ohne auf andere Felder
zugreifen zu müssen.


### Option

Die Collection `Option` wird nicht mehr gebraucht. Gibt es tatsächlich
verschiedene Wahl Optionen, zum Beispiel mehrere Kandidaten, dann werden diese
in `poll/config` definiert. Zum Beispiel die Liste der user_ids. Was es
weiterhin geben kann sind Tabellen wie `assignment_candidate`, über welche eine
Zuordnung von einem Nutzer oder anderem Objekt zu einer Poll-Option hergestellt
werden kann.

Ebenfalls braucht es keine global-options mehr. Wenn ein Nutzer auf dem
Stimmzettel eine Entscheidung für Optionen treffen möchte (z. B. global yes),
dann kann das ein Feature der `poll/method` sein, so dass entsprechende Daten in
`vote/value` valide ist und bei der erstellen von `poll/resut` berücksichtigt
werden kann.


### Stimm-Delegation

Die Delegation funktioniert wie bisher. Lediglich die Feldnamen wurden
umbenannt. Bisher war `vote/user_id` unklar, ob es die User-ID des Nutzers ist,
der auf "Abstimmen" geklickt hat oder der Nutzer, für den die Stimme gelten
soll. Die neuen Feldnamen `vote/acting_user_id` und `vote/represented_user_id`
sind eindeutig.


### Vote Weight

Bisher musste der Client sein Vote-Weight mit angeben. Der Hintergrund war, dass
der Server die Daten nicht anpassen sollte, das Feature aber auch bei
pseudo-anonymen Wahlen funktionieren sollte, wenn der Server am Ende eine Stimme
nicht mehr einem Nutzer zuordnen kann.

In Zukunft ist es ein rein Serverseitiges Feature. Pro vote wird das weight
gespeichert, wobei `1.00000` der default ist. Auch nach einer anonymisierung
einer offenen Abstimmung bleibt der Wert bestehen.

Bei geheimen Abstimmungen gibt es kein `weight`, da es dem Server nach unserem
crypto-vote-Konzept unmöglich ist, eine Stimme einem Nutzer zuzuordnen und daher
das weight zu ermitteln.

Haben unterschiedliche Gruppen unterschiedliches Stimmgewicht, können pro Gruppe
eine eigene geheime Abstimmung durchgeführt werden und das Ergebnis in einer
analogen Wahl manuell zusammengefasst werden. In Zukunft könnten wir uns
überlegen, diesen Weg automatisch als Feature zu implementieren.


### Migration

Eine Migration der Daten vom alten in das neue System ist möglich. Aus den
bisherigen `option` und `vote` Objekten kann das Feld `poll/result` erstellt
werden.

## Poll Mehod

Das Feld `poll/method` definiert, wie die Felder `poll/config`, `vote/value` und
`poll/result` interpretiert werden sollen.

Die folgenden Werte sind Beispiele. Das Konzept ermöglicht es, in Zukunft leicht
neue Methoden hinzuzufüren. Möglicherweise werden die hier beispielsweise
genannten Methoden im laufe der Implementierung angepasst.


### analog

Es gibt keine vote objekte. Wie `poll/config` und `poll/result` aussehen sollen,
kann zwischen Client und Backend ausgehandelt werden.

### motion

Die vom vom user gesendeten Werte in `vote/value` können die Werte `yes`, `no`
und `abstain` haben. Über `poll/config` kann letzters verboten werden.

`poll/result` sieht dann wie folgt aus:

`{"yes": 32, "no": 20, "abstain": 10, "base": 70}`

### selection

In `poll/config` wird gespeichert, welche Optionen es gibt und jeder Option eine
eindeutige Nummer zugeordnet. Der Nutzer übersendet eine Liste der option-ids,
für die er stimmen möchte. Über `poll/config` wird definiert, wie viele Stimmen
ein Nutzer hat und ob die übersandten ids positiv oder negativ gewertet werden
sollen (im Bisherignen System `Y` vs. `N`)

`poll/result` kann Beispielsweise wie folgt aussehen, wobei die keys die
option-ids sind.

`{"23": 30, "42": 50, "72": 1, "404": 30, "abstain": 3, "base": 120}


### rating

Bei Rating kann pro Option ein Wert abgegeben werden. Das kann - je nach
`poll/config` - eine Zahl oder ein Wert wie `yes`, `no`, `abstain` sein.

`poll/result` kann dann wie folgt aussehen:

`{"23": 30, "42": 50, "72": 1, "404": 30, "base": 70}`

oder

```json
{
  "23": {
    "yes": 30,
    "no": 20,
    "abstain": 10
  },
  "42": {
    "yes": 50,
    "no": 10,
    "abstain": 0
  },
  "72": {
    "yes": 1,
    "no": 0,
    "abstain": 0
  },
  "404": {
    "yes": 30,
    "no": 20,
    "abstain": 10
  },
  "base": 70
}
```

### single_transferable_vote

Der Wert ist als beispielshafter Platzhalter eingefügt. Single transferable vote
würde sich im neuen Konzept aber leicht implementieren lassen.
