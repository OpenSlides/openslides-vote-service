# Neues Konzept

Das neue Konzept speichert die Daten ausschließlich in der Datenbank. Die
nötigen Änderungen sind hier:

https://github.com/OpenSlides/openslides-meta/pull/289

Das neue Konzept basiert auf einer Dreiteilung.

  config -> vote -> result

Jedes dieser Felder ist ein JSON objekt. Das format ist Abhängig von
`poll/method`. Ziel ist, dass in Zukunft neue Methods eingefügt werden können,
ohne das Datenbankschema anpassen zu müssen. Zum Beispiel single transferable
vote. Für jeden Type kann es eine beliebige config geben, es werden eine
bestimmte vote-Objekte erwartet und es entsteht daraus ein bestimmtes result.

Das result ist ein redundanten Feld, welches sich aus der Liste der votes
errechnen lässt.


## Zusammenspiel mit Backend und anderen Services

Der vote-service übernimmt alle Actions des Backends, welche mit der
poll-collection arbeiten.

Die vom vote-serive übernommenen Actions sind daher:
* poll.create
* poll.update
* poll.delete
* poll.start
* poll.stop
* poll.publish
* poll.anonymize
* poll.reset

Außerdem werden weiterhin die Votes der Clients angenommen.

Beim Backend verbleiben die Actions, welche die Collections betreffen, über die
Abgestimmt werden soll. Daher Motion und Assignment. Hierzu gehört auch die
Auswahl der Assignment-Candidaten, die im Anschluss an die poll und damit an den
vote-service übergeben werden sollen.

Die bearbeitung des Meetings bleibt ebenfalls beim Backend. Daher auch die
Config der Polls, wie Beispielsweise die aktivierung oder deaktivierung von
Features oder das Setzen von Default-Werten.

Die Steuerung des Projektors sollte weiterhin beim Backend bleiben. Aktuell gibt
es `meeting/poll_couple_countdown`, wodruch countdowns neu gestartet werden.
Hier müssen wir überlegen ob es in Zukunft vom vote-service kommen soll, oder ob
der Client das nicht automatisiert beim backend aufrufen könnte.


## Handler

Fast alle Handler sind abhängig von einer Poll-ID. Diese wird als http-argument
`poll_id=XX` übergeben. Die Außnahme ist der create handler, der die poll-id
erstellt und zurückgibt.


### /vote/create

Vergleichbar zur aktuellen backend action:

https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.create.md

Erzeugt ein neues poll-objekt.

Im Client wird beim klicken auf "Neue Abstimmung" noch kein Request gesandt,
sondern zunächst ein Overlay zum erstellen der Poll angezeigt. In diesem werden
alle Einstellungen zur Poll vorgenommen und anschließend auf "Speichern"
geklickt. In dem Moment wird dann create request abgesandt.

Bei analogen Polls bedeutet das, dass bereits an dieser Stelle das Ergebnis
übertragen werden kann.

Im Body müssen die Daten zum anlegen der Poll übergeben werden. Das ist ein json-objekt mit folgenden Feldern:

- title (required)
- description (optional)
- content_object_id (required)
- meeting_id (required)
- method (required)
- config (abhängig von method. Kann required oder optional sein)
- visibility (required)
- entitled_group_ids (nur wenn visibility != manually)
- result (nur wenn visibility == manually)


### /vote/update

Vergleichbar zum aktuellen backend:

https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.update.md

Ist ähnlich zur create-view. Manche Felder könnnen jedoch nicht mehr bearbeitet
werden. Bisher war es "Art der Stimmabgabe". Dies entspricht dem alten
`poll/type` (analog, named, pseudoanonymous, crypto). Dies entspricht dem neuen
`poll/visibility`.


### /vote/delete

Vergleichbar zum aktuellen backend:

https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.delete.md


### /vote/start

Validiert das Poll Objekt, vor allem poll/config, und setzt das Feld
`poll/state` auf `started`.

See also:
https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.start.md


### /vote/finalize

Entspricht der früheren stop action, kann aber auch publish und anonymize
aufrufen.

Optional wird "publish" und "anonymize" übergeben.

Ist die poll im state `started`, dann wird sie gestoppt. Hierfür wird der Wert
in `poll/result` geschrieben und der State auf `finshed` gesetzt.

Wurde das Flag `publish` übergeben, wird der state stattdessen auf `published`
gesetzt.

Wurde der Flag `anonymize` übergeben, dann werden alle user-ids aus den
zugehörigen vote-objekten gelöscht.

Der handler kann mehrfach aufgerufen werden. Zum Beispiel zuerst ohne flags,
wodurch die poll gestoppt wird. Dann ein weiters mal mit dem flag `publish`,
wodruch `poll/result` nicht mehr angefasst wird, aber der state gesetzt wird und
anschließend ein drittes mal mit dem flag `anonymize`.

Siehe auch:
https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.stop.md
https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.publish.md
https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.anonymize.md


### /vote/reset

Setzt den state auf `created` zurück und löscht alle votes und das ergebnis.

Siehe auch:
https://github.com/OpenSlides/openslides-backend/blob/main/docs/actions/poll.reset.md


### /vote

Nimmt ein Vote-Objekt als Request-Body entgegen und validiert dieses abhängig
von `poll/method` und `poll/config`. Anschließend wird dieses in die
`vote`-collection gespeichert. Hierbei wird in einer transaction sichergestellt,
dass die Abstimmung im state `started` ist und es zu der Poll vom user keine
andere Stimme gibt.

Der Body eines Vote-Requests sieht wie folgt aus:

```json
{
  "user_id": 42,
  "value": "yes"
}
```

Der Wert "user_id" ist optional. Wenn nicht gesetzt, wird die request_user_id
verwendet. Es ist der User, für den abgestimmt werden soll (represented_user_id).

"value" ist der eigentliche Stimmzettel, der später eins zu eins in `vote/value`
gespeichert wird.

Damit ein Nutzer abstimmen darf, muss der represented_user stimmberechtigt sein,
der acting_user (request_user) muss für ihn abstimmen dürfen.

#### Stimmberechtigt

#### Erlaubte Delegation

* Delegation muss aktiviert sein: meeting/users_enable_vote_delegation
* Der represented_user muss die Stimme an den acting_user übertragen haben: meeting_user/vote_delegated_to_id
* Im strict mode (meeting/users_forbid_delegator_to_vote): respreseted_user != acting_user (wenn delegiert)

? Muss der acting_user im meeting sein?
? Muss der acting_user anwesend sein?

### Andere bisherige Handler

Bisher gab es im alten vote-service folgende weiteren Handler, die jedoch nicht
ins neue Konzept übernommen werden:

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

Die Hauptaufgabe des vote-service liegt in den elektronischen Abstimmungen.
Darauf liegt auch der Hauptzweck des Konzept. Analoge Abstimmungen passen jedoch
auch in das Konzept. Hier wird direkt `poll/result` geschrieben. Es gibt keine
`vote` objekte.

Ob eine Abstimmung analog ist, wird über das Feld `poll/visibility` geregelt.
Dieses muss auf "manually" gesetzt sein.

Das Format der analogen Wahl, insbesondere welche Felder es gibt, wird wird über
den Eintrag in `poll/method` und `poll/config` geschrieben werden.


### poll/visibility

Das Feld `poll/visibility` entspricht dem alten Feld `poll/type`. Es
entscheidet, ob die Abstimmung namentlich, geheim oder offen ist.

Der Wert `manually` bedeutet, dass die Werte manuell ermittelt und eingetragen
werden. Dies entspricht der analogen Abstimmung, passt vom Wording jedoch auch
auf andere Fälle, bzw. wenn ein anderes Tool für eine elektronische Abstimmung
verwendet wurde.

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

Bevor crypto-vote implementiert ist, wird bei `secret` die Stimme und die User-ID
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

Noch zu klären


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


### poll/voted_ids

Alle user_ids, die abgestimmt haben. Ist nur wichtig, wenn die vote anonymisiert
wurde. Bei crypto-votes und nicht anonymisierten votes findet man die Info
leicht über poll/votes heraus.

Brauchen wir das feature überhaupt?
Sollte das Feld immer geschrieben werden, oder nur, wenn anonymize ausgeführt wird?
Sollte es bereits während der Abstimmung geschrieben werden oder erst bei Stop?


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

Da das Vote-Weight eine Dezimale-Zahl ist, können auch die Werte in den
Ergebnissen Dezimale Zahlen sein. Aus diesem Grund sind die Ergebnisse im JSON
keine Zahlen, sondern Strings (nicht 3, sondern "3").


### Migration

Eine Migration der Daten vom alten in das neue System ist möglich. Aus den
bisherigen `option` und `vote` Objekten kann das Feld `poll/result` erstellt
werden.

Die Migration sollte für die Einheitlichkeit durch das Backend durchgeführt
werden. Der der Code zum Erstellen von `poll/result` im vote-service
implementiert ist, müssen wir uns überlegen, ob der vote-serivce möglicherweise
eine Hilfsfunktion anbietet, die vom backend aufgerufen werden kann.


## Poll Mehod

Das Feld `poll/method` definiert, wie die Felder `poll/config`, `vote/value` und
`poll/result` interpretiert werden sollen.

Die folgenden Werte sind Beispiele. Das Konzept ermöglicht es, in Zukunft leicht
neue Methoden hinzuzufüren. Möglicherweise werden die hier beispielsweise
genannten Methoden im laufe der Implementierung angepasst.


### motion

Dies ist die klassische Methode für Motions. Es gibt eine Sache, zu der Ja, Nein
und Enthaltung gesagt werden kann.

Es gibt nur eine Einstellung "abstain", die entweder True oder False sein kann.
Der default ist True.

Die vom vom user gesendeten Werte in `vote/value` können die Werte `yes`, `no`
und `abstain` haben. Letztes kann über die Einstellung verboten sein.

`poll/result` sieht dann wie folgt aus:

`{"yes": "32", "no": "20", "abstain": "10"}`

Die Werte sind Strings, da es Dezimal-Werte sein könnten (siehe vote-weight).


### selection

Selektion ist eine Auswahl zwischen mehreren Möglichkeiten. Die Auswahl kann
entweder positiv (ich wähle folgende Optionen) oder negativ (ich will folgende
Optionen nicht) sein.

In `poll/config` wird gespeichert, welche Optionen es gibt. Der Nutzer
übersendet eine Liste der option-indexes, für die er stimmen möchte. Über
`poll/config` wird definiert, wie viele Stimmen ein Nutzer hat und ob die
übersandten ids positiv oder negativ gewertet werden sollen (im Bisherignen
System `Y` vs. `N`).

`poll/config` sieht wie folgt aus:

```json
{
  "options": ["Hans", "Gregor", "Tom"],
  "max_options_amount": 2,
  "min_options_amount": 1
}
```

`options` kann dabei alles sein. Der vote-service muss nur wissen, wie viele es
sind oder verwendet nur die indexe. Wenn es für assigments verwendet wird, dann
werden es vermutlich ids von `poll_candidate` objekten sein.

Ein `vote` ist eine Liste von Indexen. Zum Beispiel `[0,1]` für "Hans" und
"Gregor".

Über die Einstellungen `max_options_amount` und `min_options_amount` kann die
Anzahl an Optionen bestimmt werden, welche pro Abstimmung abgegeben werden
können. Der Default ist keine Beschränkung.

`poll/result` kann Beispielsweise wie folgt aussehen, wobei die keys die
option-ids sind.

`{"0": "30", "1": "50", "2": "1", "abstain": 3}`


### rating

Rating ist eine Abstimmung, bei denen verschiedene Optionen mit einer Zahl
bewertet werden.

Die Optionen sind:
	- options: Wie bei selection eine Reihne von Optionen mit beliebigen Value
	- max_options_amount: Die maximale Anahl von Optionen pro Vote
	- min_options_amount: Die minimale Anzahl von Optionen pro Vote
	- max_votes_per_option: Der maximale Wert pro Option
	- max_vote_sum: Der maximale Wert aller Optionen aufsummiert
	- min_vote_sum: Der minimale Wert aller Optionen aufsummiert


`poll/result` kann dann wie folgt aussehen:

`{"23": "30", "42": "50", "72": "1", "404": "30"}`


### rating-motion

Ratin-motion ist wie rating, doch jede Option wird sie mit `yes`, `no` oder
`abstain` bewertet.

Die Optionen sind:
	- options: Wie bei selection eine Reihne von Optionen mit beliebigen Value
	- max_options_amount: Die maximale Anahl von Optionen pro Vote
	- min_options_amount: Die minimale Anzahl von Optionen pro Vote
	- abstain: Wenn true, dürfen enthaltungen gesendet werden.


`poll/result` kann dann wie folgt aussehen:

```json
{
  "23": {
    "yes": "30",
    "no": "20",
    "abstain": "10"
  },
  "42": {
    "yes": "50",
    "no": "10",
    "abstain": "0"
  },
  "72": {
    "yes": "1",
    "no": "0",
    "abstain": "0"
  },
  "404": {
    "yes": "30",
    "no": "20",
    "abstain": "10"
  }
}
```

### single_transferable_vote

Der Wert ist als beispielshafter Platzhalter eingefügt. Single transferable vote
würde sich im neuen Konzept aber leicht implementieren lassen.
