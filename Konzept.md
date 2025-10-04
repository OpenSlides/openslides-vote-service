# Neues Konzept


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
