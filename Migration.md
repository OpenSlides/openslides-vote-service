# Migration zum neuen Vote-Service

Das alte System und das neue unterscheiden sich wesentlich. Eine eins zu eins
übersetzung der alten und neuen Felder ist nicht möglich.


## Bisheriges System

Im bisherigen System hat jede Poll mehrere optionen. Diese werden über
`poll/option_ids` und `poll/global_option_id` verlinkt. Auch für motions wird
eine global-option angelegt, obwohl diese dort nie verwendet werden sollte.

Jede option hat die Werte `yes`, `no` und `abstain`. Bei der Abstimmung gibt
jeder Nutzer für jede Option eine dieser drei Möglichkeiten an. Es gibt daher
pro User mehrere `vote` objekte. Diese werden immer als Ja-Nein-Enthaltung
gespeichert, auch wenn es eigentlich eine Auswahl ist. Die `option`-Objekte
enthalten das Result. Sie speichern in den `yes`-`no`-`abstain`-Feldern die
Summe aller auf sie bezogenen vote objekte. Die globale Obtion werden separat
gezählt. Daher als "Generelle Ablehnung", "Generelle Enthaltung" oder "Generelle
Zustimmung".

Die vote objekte dienen lediglich der Anzeige, wer wie abgestimmt hat. Das Feld
`user_token` hilft dabei, verschiedene votes eines Nutzers zu bündeln, wenn die
user-id entfernt wurde. Bei nicht anonymisierten polls is `vote/user_id` der
Nutzer, für den die Stimme gezählt werden soll und `vote/ delegated_user_id`,
der die Stimme abgegeben hat. Werden pro Nutzer mehrere Stimmen erlaubt, dann
werden diese Stimmen in den vote-objekten über das vote-weight-feature
gebündelt.

Die vom Nutzer eigentlich gesendeten Daten werden nicht gespeichert, sondern
interpretiert in die vote-objekte aufgeteilt.

Fragen: Sind folgende Aussagen korrekt:
* Bei motion gibt es zwar immer eine global-option, diese wurde aber nie genutzt.



## Neues System

Im neuen System gibt es keine optionen. Stattdessen wird das Ergebnis direkt im
Feld `poll/result` gebündelt. Die Votes (jetzt ballot genannt) enthalten genau
die Daten, die ein Nutzer gesendet hat. Es gibt daher pro Poll und User nur ein
ballot-objekt. options gibt es nicht mehr als Collection. Jedoch werden bei
Wahlen die möglichen optionen in `poll_config_X/options` gespeichert.

Eigentlich sollte das Feld `poll/result` redundant sein. Daher, es lässt sich zu
jeder Zeit aus den votes neu berechnen. Dies gilt nicht für manuelle polls und
es wäre in Ordnung, wenn es auch nicht für migrierte polls gilt.


## Übertragung

Pro altem poll (`old`) wird ein neues Poll erstellt. Dieses hängt davon ab, ob
es eine motion, assignment oder topic poll ist.


### motion

#### poll_config_approval

```
{
  id: kann automatisch erstellt werden,
  poll_id: old.id
  allow_abstain: if old.method == "YNA" then "" else false,
}
```

#### poll

```
{
  id: old.id,
  title: old.title,
  config_id: neu erstellte config-id,
  visibility: old{"analog": "manually", "named": "open", "pseudoanonymous": "secret", "cryptographic": @panic(immpossible)},
  state: if old.state == "published" then "finished" else old.state,
  result: see below,
  published: old.state == "published",
  allow_invalid: false,
  allow_vote_split: false,
  sequential_number: old.sequential_number,
  content_object_id: old.content_object_id,
  vote_ids: Egal in rel-db. Die Vote-objekte setzten die Relation,
  voted_ids: [e.user_id for e in old.entitled_users_at_stop if e.voted],
  entitled_group_ids: old.entitled_group_ids,
  projection_ids: Egal in rel-db,
  meeting_id: old.meeting_id
}
```


#### poll/result

Im alten System gibt es pro poll eine Option. Es gibt zusätzlich eine
global-option die jedoch ignoriert werden kann. Das neue `poll/result`
entspricht im wesentlichen dieser einen option. Sollte es mehr als eine option
geben, dann @panic.

Wenn poll.state "created" oder "started" ist, dann ist poll/result leer. Ansonsten:

`poll/result`: `{"yes": option.yes, "no": option.no, "abstain": option.abstain}`

Bei manually polls werden invalide Stimmen unterstützt. Diese standen bisher in
poll.votesinvalid. In Zukunft können sie als weiteres attribute in `poll/result`
geschrieben werden. Allerdings nicht als decimal, sondern als int. `{...,
"invalid": 42}`


#### ballot

Wenn poll.state "created" oder "started" ist, dann gibt es keine ballots.

Wenn poll.visibility == "manually", dann wird kein ballot-objekt erstellt.

Ansonsten:

Im alten system gibt es pro user nur ein vote. Die Votes können über
old_poll.option_ids[0].vote_ids gefunden werden.

```
{
  id: kann automatisch erstellt werden ich würde nicht die alten ids verwenden,
  weight: old.weight,
  split: false,
  value: old{"Y": "yes", "N": "no", "A": "abstain"} ansonsten @panic,
  poll_id: old.poll_id,
  acting_meeting_user_id: old.delegated_user_id -> jedoch seine meeting_user_id im poll.meeting,
  represented_meeting_user_id: old.user_id -> jedoch seine meeting_user_id im poll.meeting.
}
```


### assignment

Wenn im alten system "Ja/Nein/Enthaltung pro Liste" ausgewählt wurde (Ich
glaube, dann gibt es nur eine option mit content_object_id auf
poll_candidate_list), dann behandle es wie bei motion. Daher mit "method":
"approval". Daher alles hier ignorieren und nur wie bei motion bearbeiten.


#### poll_config_rating_approval

```
{
  id: kann automatisch erstellt werden,
  poll_id: old.id,
  max_options_amount: old.max_votes_amount,
  min_options_amount: old.min_votes_amount,
  allow_abstain: if old.method == "YNA" then "" else false,
}
```


#### poll_config_option

Relevant sind die alten options (old_option) der poll. Für jede option sollte der Werte
option.content_object_id ein user-collection sein. Ansonsten @panic. Von diesem
Feld wird die user_id und zu dieser die meeting_user_id im entsprechenden meeting gesucht.

```
{
  id:
  poll_config_id: poll_config_rating_approval/ID_VON_OBEN,
  weight: old_option.weight,
  meeting_user_id: old_option.content_object_id -> Davon user_id, von dieser die meeting_user_id herausfinden,
}
```


#### Poll

```
{
  id: old.id,
  title: old.title,
  config_id: poll_config_rating_approval/ID_FROM_ABOVE,
  visibility: old{"analog": "manually", "named": "open", "pseudoanonymous": "secret", "cryptographic": @panic(immpossible)},
  state: if old.state == "published" then "finished" else old.state,
  result: see below,
  published: old.state == "published",
  allow_invalid: false,
  allow_vote_split: false,
  sequential_number: old.sequential_number,
  content_object_id: old.content_object_id,
  vote_ids: Egal in rel-db. Die Vote-objekte setzten die Relation,
  voted_ids: [e.user_id for e in old.entitled_users_at_stop if e.voted],
  entitled_group_ids: old.entitled_group_ids,
  projection_ids: Egal in rel-db,
  meeting_id: old.meeting_id
}
```


#### poll/result

Poll/result ist ein dict. Pro alter option gibt es einen Eintrag. Der Key ist
jeweils die oben angelegte poll_config_option-id. Die Werte "yes", "no" und "abstain"
werden als object übernommen. Zusätzlich wird bei manuellen polls als weiterer
Wert "invalid" aus der alten poll übernommen.

`{"1":{"yes":"5","no":"1"},"2":{"yes":"1","abstain":"6"},"invalid":1}`

Zusätzlich müssen die globalen Optionen in das Ergebnis mit einberechnet werden.
Daher die "yes"-"no"-"abstain" Werte der globalen Abstimmung wird bei jeder
Option addiert.


#### ballot

Wenn poll.state "created" oder "started" ist, dann gibt es keine ballots.

Wenn poll.visibility == "manually", dann wird kein ballot-objekt erstellt.

Ansonsten:

Im alten system gibt es pro user und option eine vote. Diese müssen in jeweils
ein ballot-objekt zusammengefasst werden. Die Votes können über
old_poll.option_ids.vote_ids gefunden werden. Werte mit identischem user-token
gehören zusammen.

```
{
  id: kann automatisch erstellt werden ich würde nicht die alten ids verwenden,
  weight: old.weight (muss bei allen votes identisch sein, sonst @panic),
  split: false,
  value: Siehe unten,
  poll_id: old.poll_id,
  acting_meting_user_id: old.delegated_user_id -> Als meeting_user_id im entsprechenden meeting,
  represented_meeting_user_id: old.user_id -> Als meeting_user_id im entsprechenden meeting
}
```


`ballot/value` sieht wie folgt aus:
`{"option_id_A":"yes","option_idB":"abstain"}`. Daher pro option gibt es ein
Attribut als String. Der Wert wird genauso umgerechnet, wie bei motion: old{"Y":
"yes", "N": "no", "A": "abstain"} ansonsten @panic.


### topic

Wird fast identisch wie bei assignment durchgeführt.

Aber als key bei poll/result und config werden nicht die meeting_user_ids
verwendet, sondern option.text.



## Informationen, die Verloren gehen:

* Kurzlaufend oder langlaufend
* poll/description, sollte aber nie gesetzt gewesen sein
* Wahlverzeichet: entitled_users_at_stop. Es wird gerade nur übertragen, wer gewählt hat, aber nicht, wer stimmberechtigt war.
* Bei kummulativen Wahlen: poll.max_votes_per_option (daher, was die Einstellung war)
* Global options werden nicht mehr separat aufgeführt, sondern in das Ergebnis mit einberechnet.
* Poll.valid wurde bisher separat gezählt. In Zukunft muss es berechnet werden. Aus anzahl der votes minus result.invalid


## Einzelvergleich

### Alte Felder

* meeting/poll_default_backend was removed. No migration necessary. Just remove the value.
* motion/option_ids was removed. I think, it can just be removed (ignored) since it has no meaning.
* poll/description was removed. No migration needed. Was not used before.
* poll/type was renamed to poll/visibility and the values have changed.
  * "analog" -> "manually"
  * "named": Its not clear to me if old "named" values should be "named" in the new system or "open". I think, "open" is ok.
  * "pseudoanonymous" -> "secret"
  * "cryptographic": There should be no case. If so, "secret" can be used.

* poll/backend: was removed. No migration necessary.
* poll/is_pseudoanonymized: Was removed. No migraton necessary.
* poll/pollmethod. Was removed, is now part of poll/config_id.
* poll/state: The value `published` was removed. polls in this state have to be set to `finished` and the field `poll/published` has to be set to true.
* poll/min_votes_amount, poll/max_votes_amount, poll/max_votes_per_option, poll/global_yes, poll/global_no, poll/global_abstain are removed. The new field poll/config has to be generated from them.
* poll/onehundred_percent_base has be removed. TODO after the client is done.
* poll/votesvalid, poll/votesinvalid, poll/votescast where removed. They have to be used to generate the field `poll/result`.
* poll/entitled_users_at_stop was removed. TODO after the client is done.
* poll/live_voting_enabled was removed. No migration needed, since there are no ongoing polls at the same time as the migration.
* poll/live_votes was removed. No migration needed.
* poll/crypt_key, poll/crypt_signature, poll/votes_raw, poll/votes_signature were removed: No migration needed. There was no case with this values.
* poll/option_ids, poll/global_option_id was removed: No migration needed. But are necessary to generate `poll/result`.
* The `option` collection was removed. No migration needed, but necessary to generate `poll/result`.
* vote/user_token was removed: No migration necessary
* vote/user_id was renamed to ballot/represented_meeting_user_id.
* vote/delegated_user_id was renamed to ballot/acting_meeting_user_id.
* vote/meeting_id was removed. No migration necessary.
