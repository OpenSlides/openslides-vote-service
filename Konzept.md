# Neues Konzept


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
  "min_options_amount": 1,
  "allow_nota": true
}
```

`options` kann dabei alles sein. Der vote-service muss nur wissen, wie viele es
sind oder verwendet nur die indexe. Wenn es für assigments verwendet wird, dann
werden es vermutlich ids von `poll_candidate` objekten sein.

`allow_nota` ermöglicht die Wahl mit dem String "nota" was separat gezählt wird:
https://en.wikipedia.org/wiki/None_of_the_above

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

# Aktueller Stand / Offene TODOs
- Methods in README dokumentieren


# Fragen

- Bei motion-rating: Muss man für jede Option eine stimme abgeben? Ist abstain
  der default? Was wenn abstain deaktiviert ist?
