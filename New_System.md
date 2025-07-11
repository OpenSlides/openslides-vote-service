# Features

- analog -> Nicht Einzelstimmen. Aber mit Mehrheit
- Namentlich - offen - geheim
- live
- Mit oder ohne Enthaltung
- Ungültig?
- min_votes_amount: Doppeldeutig: Entweder Anzahl der Stimmen, die man auf alle Optionen aufteilen muss, oder die Anzahl die Optionen, die man auswählen muss
- max_votes_amount: Wie min_votes_amount
- max_votes_per_option: Man kann einer Option maximal X stimmen geben

- onehundred_percent_base: Ist für die Auswertung wichtig
- Negative Abstimmung: Man kann pro Option Nein sagen
- global_option_id:
  It is possible to give global votes, so a user must not vote for one or more options. Enabling each global vote is done with global_yes, global_no and global_abstain. If such a global vote is given, the value is saved to the global option (poll/global_option_id).
- Vote Delegation
- Vote weight




# Konzept

## Speichern in DB

Nach dem starten werden die Votes an den Vote-Service gesandt. Sie werden in der
normalen Datenbank pro User gespeichert.

Dafür könnte die bisherige "vote" tabelle verwendet werden. Aber achtung. Auch
bei geheimen Wahlen wird hier gespeichert, wer wie abgestimmt hat. Das muss im
restrictor berücksichtigt werden. Da geheime Wahlen in zukunft kryptografisch
sind, ist das nicht so dramatisch

Auch bei crypto-votes machen wir das für die vereinheitlichung so, obwohl dort das bulletin board reichen würde.

Frage: Oder sollen die Votes immer in ein bulletin board gespeichert werden und
wir klären über die Permissions, wer es während einer Wahl sehen darf?

Bei Stop werden werden alle votes zu einem result objekt zusammengezwählt.
Dieses wird in der poll-collection gespeichert.

Die option tabelle wird entfernt.

## Geheim, offen namentlich

Bei geheimen Wahlen werden die vote-objekte gelöscht oder zumindest die user-id
entfernt. Bei offenen Abstimmungen werden die vote-objekte in einem bestimmten
Moment gelöscht. Zum Beispiel, wenn man zum nächsten Tagesordnungspunkt gibt
oder es gibt eine extra action, die aufgerufen werden kann. Bei namentlichen
Abstimmungen wird nichts gelöscht.

## Migration

Für Migrationen müssen wir aus dem alten system die neuen result objekte bauen.

Nachteil: Da die result objekte im ganzen gespeichert werden, sind sql-queries
über mehrere Polls nicht mehr möglich oder nicht mehr so performant. Ich sehe
dafür aber keinen Anwendungsfall.

## Analoge Abstimmung

Bei der analogen Wahl gibt es keine vote-objekte. Dafür werden die results
manuell vom Client erstellt. Frage: Sollen die ans backend oder den vote-service
geschickt werden? Sollen sie vom Server validiert werden?

## Ungültig

Es besteht der wunsch ungültig abzustimmen. Bei crypto kommt es sowieso. Frage:
Soll der Vote-Service validieren? Daher den Nutzer auf eine ungültige Stimme
hinweisen? Oder ist ungültig eine weitere Option wie "enthaltung"?

## Vote Weight

Aktuell muss ein nutzer mit jeder Wahl sein vote-weight angeben. Ist das nötig?
Oder ist das immer objektiv? Dann könnte es auch bei der Auszählung
berücksichtigt werden bzw mit in das vote-objekt gespeichert werden.

Nur wenn wir es bei der crypto-vote wollen (aktuell sagen wir nein), dann müsste
es vom Client dazugeschrieben werden.

## Delegation

Dem Vote-service muss irgendwie mitgeteilt werden, für wen man abstimmt.
Übrigens auch bei der crypto-wahl!

Das bedeutet, man muss neben der eigentlichen Vote auch diese Information
senden. Ein Stimmzettel sieht daher wie folgt aus:

{
"user_id": ID, // optional für wen abestimmt werden soll. Wenn nicht gesetzt, für einen selbst.
"value": JSON, // die eigentliche Stimme nach den unten genannten System. base64 bei crypto-wahl
}

Frage: Da vote-delegation die ausnahme ist, sehen die meisten vote-objekte wie
folgt aus: {"value":"Yes"}. Schöner wäre nur "Yes". Soll das unterstützt werden?
Daher, man kann auch nur den nackten "value" schicken? Oder ist das bei komplexen stimmen verwirrtent?

# Abstimmungsarte

Antrag: Man sagt Ja, Nein, Enthaltung
Auswahl: Man sucht eine oder mehrere Option unter vielen aus
Abwahl: Man sucht eine oder mehrere Optionen aus, die negativ bewertet werden sollen
Auswahl pro Option: Jede Option bekommt eine Zahl oder ein "Ja,Nein,Enthaltung" (unter berücksichtigung von max_votes_amount etc)
single transferable vote?

# Antrag

## Vote

"yes" | "no" | "abstain" | "invalid"

## Result

{"yes": 32, "no": 20, "abstain": 10, "invalid": 0}

# Auswahl/Abwahl

## Vote

Jede Option hat eine Id. Zum Beispiel eine user-id oder option-id

[23,42,72]

## Result

{"23": 30, "42": 50, "72": 1, "404": 30}

# Auswahl pro Option

## Vote

{"23": 1, "42": -1, "72": 5}

oder

{"23": "yes", "42": "no", "72": "abstain"}

## Result

{"23": 30, "42": 50, "72": 1, "404": 30}

oder

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
  }
}
