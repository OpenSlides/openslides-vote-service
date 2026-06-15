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
Wahlen die möglichen optionen in `poll_option` gespeichert. Die alte
`options` collection und die neue `poll_option` sind zwei verschiedene Dinge
die nur zufällig ähnlich heißen.

Eigentlich sollte das Feld `poll/result` redundant sein. Daher, es lässt sich zu
jeder Zeit aus den votes neu berechnen. Dies gilt nicht für manuelle polls und
es wäre in Ordnung, wenn es auch nicht für migrierte polls gilt.


## Migrating the polls

### General rules

The following information is relevant for all the poll types.

#### poll and poll_config_X

For each old poll 2 models have to be created: a new `poll` and a related
`poll_config_X`. How the data should be migrated depends on whether it is a
motion, assignment or a topic poll. When it comes to assignement polls, there
are 3 possibilities based on content_object_id and whether the global yes or no
option is used.

#### poll_ballot objects and poll/result

If poll.state is "created" or "started", then poll/result is empty.

If old_poll.state if "analog" (corresponds to the new state "manually"), no
ballots should be created for the poll. The result for the finished polls in
this case should be carried over from the old poll.

For the other finished poll the ballots have to be generated from the votes
and the result has to be calculated and saved in the field `poll/result`.

### motion

#### poll_config_approval

```
{
  poll_id: new_poll.id,
  allow_abstain: if old.method == "YNA" then "true" else "false",
  onehundred_percent_base: old_poll.onehundred_percent_base. Map (old_poll -> new):
      - YN -> yes_no
      - YNA -> valid
      - valid: (remains unchanged).
      - cast: (remains unchanged).
      - entitled: (remains unchanged).
      - entitled_present: (remains unchanged).
      - disabled: (remains unchanged).
      - Y -> @panic(not allowed for this config type)
      - N -> @panic(not allowed for this config type)
}
```

#### poll

```
{
  title: old.title,
  visibility: old.type. Map (old -> new):
      - analog -> manually
      - named -> open
      - pseudoanonymous -> secret
      - cryptographic -> @panic(immpossible)
  state: if old.state == "published" then "finished" else old.state,
  result: old.result if old.type == "analog" else see below,
  published: old.state == "published",
  anonymized: old.is_pseudoanonymized,
  allow_invalid: old.type == "analog",
  allow_vote_split: false,
  live_voting_enabled: old.live_voting_enabled if old.type != "analog",
  sequential_number: old.sequential_number,
  content_object_id: old.content_object_id,
  voted_ids: old.voted_ids -> replace each user_id with meeting_user_id in poll.meeting_id,
  entitled_group_ids: old.entitled_group_ids if old.type != "analog",
  meeting_id: old.meeting_id,
}
```


#### poll/result

In the old system, there is one option per poll. There is also a global option,
but this can be ignored. The new `poll/result` essentially corresponds to this
single option. If there is more than one option, then @panic.

The value "total_ballots" is calculated additionally. It should be stored as an
integer. The other values are strings as they are decimal.

Example: `{"yes":"32","no":"20","abstain":"10","total_ballots":62}`

Calculated from `old_poll.option_ids[0].vote_ids`:

```
{
  yes: option.yes -> string,  (skip if 0)
  no: option.no -> string,  (skip if 0)
  abstain: option.abstain -> string,  (skip if 0)
  total_ballots: count(option.vote_ids) -> number
}
```

#### poll_ballot

In the old system only one vote should exist per user. The votes can be found
via `old_poll.option_ids[0].vote_ids`. For each old vote a new
`poll_ballot` object sould be created:

```
{
  poll_id: new_poll.id,
  weight: old.weight,
  split: false,
  value: Map (old -> new):
      - Y -> yes
      - N -> no
      - A -> abstain
      - else @panic(impossible value)
  acting_meeting_user_id: meeting_user_id from old.delegated_user_id and poll.meeting,
  represented_meeting_user_id: meeting_user_id from old.user_id and poll.meeting
}
```


### assignment with poll_candidate_list

If in the old system the collection of `old_poll.content_object_id` is
`poll_candidate_list`, the following collections should be created the same way
as for the [Motion poll](#motion):

* poll_config_approval
* poll (including calculation of poll/result)
* poll_ballot

#### poll_option

Additionally, each `poll_candidate` ("old") in
`old_poll.option_ids[0].content_object_id.poll_candidate_ids` should be saved as
a `poll_option`:

```
{
  poll_id: new_poll.id,
  weight: old.weight,
  text: NULL,
  meeting_user_id: meeting_user_id from old.user_id and old.meeting_id
}
```

### topic

`poll` is being created the same way as for [Motion poll](#motion). The other
collections are being migrated differently.

#### poll_config_selection

```
{
  poll_id: new_poll.id,
  max_options_amount: old_poll.max_votes_amount,
  min_options_amount: old_poll.min_votes_amount,
  allow_nota: old_poll.global_option_id exists,
  strike_out: old_poll.pollmethod == N,
  display_chart: pie,
  onehundred_percent_base: old_poll.onehundred_percent_base. Map (old_poll -> new):
      - YNA -> valid
      - Y -> no_general
      - N -> no_general
      - valid: (remains unchanged).
      - cast: (remains unchanged).
      - entitled: (remains unchanged).
      - entitled_present: (remains unchanged).
      - disabled: (remains unchanged).
      - YN -> @panic(not allowed for this config type)
}
```

#### poll_option

A new `poll_option` should be created for each old `option` ("old"). They can
be found via `old_poll.option_ids`.

```
{
  poll_id: new_poll.id,
  weight: old.weight,
  text: old.text,
  meeting_user_id: None
}
```

#### poll/result

For each old option there is one entry in the result dict. The key is the
poll_option.text. The value is being calculated from the old votes.

`global_yes` and `global_no` in polls with `poll_config_selection` are being
calculated separately into the value "nota".

Example: `{"Option 1":"40","Option 2":"23","nota":"6","abstain":"7","total_ballots":76}`

Calculation:
```
{
  for each option (if not option.used_as_global_option_in_poll_id == old_poll.id):
    poll_option.text: option.yes -> string,  (skip if value is 0)

  abstain: sum(all_options.abstain) -> string,  (skip if 0)
  nota: old_poll.global_option_id -> (option.yes + option.no) -> string,  (skip if 0)
  total_ballots: count(all_options.vote_ids) -> number
}
```

#### poll_ballot

```
{
  poll_id: new_poll.id,
  weight: old.weight,
  split: false,
  value: old.value -> Replace old options ids with corresponding new poll_options ids,
  acting_meeting_user_id: meeting_user_id from old.delegated_user_id and poll.meeting,
  represented_meeting_user_id: meeting_user_id from old.user_id and poll.meeting
}
```

### assignment with global_yes or global_no

Assignment polls with `global_yes` or `global_no` in the new voting system will
function almost like the topic polls: with `poll_config_selection` but with
`meeting_user_id`s instead of the `text` in `poll_option`.

Migrate poll this way if:

* Collection of the old poll's content_object_id is `assignment`
* Old poll has `global_option_id`
* `global_yes` and/or `global_no` for the poll is true

The following collections should be created the same way
as for the [Topic poll](#topic):

* poll
* poll_option
* poll_ballot

Other collection have minor differences.

#### poll_config_selection

`poll_config_selection` for the assignment poll is similar to the topic poll
but it always has `allow_nota: true` and should not have `display_chart`:

```
{
  poll_id: new_poll.id,
  max_options_amount: old_poll.max_votes_amount,
  min_options_amount: old_poll.min_votes_amount,
  allow_nota: true,
  strike_out: old_poll.pollmethod == N,
  display_chart: null,
  onehundred_percent_base: old_poll.onehundred_percent_base. Map (old_poll -> new):
      - YNA -> valid
      - Y -> no_general
      - N -> no_general
      - valid: (remains unchanged).
      - cast: (remains unchanged).
      - entitled: (remains unchanged).
      - entitled_present: (remains unchanged).
      - disabled: (remains unchanged).
      - YN -> @panic(not allowed for this config type)
}
```

#### poll/result

Calculated similarly to the topic polls, but ids of the poll_options created
above are used as the keys instead of the poll_option/text.

Example: `{"1":"40","2":"23","nota":"6","abstain":"7","total_ballots":76}`

Calculation:
```
{
  for each option (if not option.used_as_global_option_in_poll_id == old_poll.id):
    poll_option.id: option.yes -> string,  (skip if value is 0)

  abstain: sum(all_options.abstain) -> string,  (skip if 0)
  nota: old_poll.global_option_id -> (option.yes + option.no) -> string,  (skip if 0)
  total_ballots: count(all_options.vote_ids) -> number
}
```

### assignment: other cases

#### poll_config_rating_approval

```
{
  poll_id: new_poll.id,
  max_options_amount: old.max_votes_amount,
  min_options_amount: old.min_votes_amount,
  allow_abstain: if old.method == "YNA" then "true" else "false",
  onehundred_percent_base: old.onehundred_percent_base. Map (old -> new):
      - YN -> yes_no
      - YNA -> valid
      - valid: (remains unchanged).
      - cast: (remains unchanged).
      - entitled: (remains unchanged).
      - entitled_present: (remains unchanged).
      - disabled: (remains unchanged).
      - Y -> @panic(not allowed for this config type)
      - N -> @panic(not allowed for this config type)
}
```

#### poll

```
{
  title: old.title,
  visibility: old.type. Map (old -> new):
      - analog -> manually
      - named -> open
      - pseudoanonymous -> secret
      - cryptographic -> @panic(immpossible)
  state: if old.state == "published" then "finished" else old.state,
  result: old.result if old.type == "analog" else see below,
  published: old.state == "published",
  anonymized: old.is_pseudoanonymized,
  allow_invalid: old.type == "analog",
  allow_vote_split: false,
  live_voting_enabled: old.live_voting_enabled if old.type != "analog",
  sequential_number: old.sequential_number,
  content_object_id: old.content_object_id,
  voted_ids: for each user in old.voted_ids -> meeting_user_id in old.meeting_id,
  entitled_group_ids: old.entitled_group_ids if old.type != "analog",
  meeting_id: old.meeting_id
}
```

#### poll_option

For each old option ("old"), the option.content_object_id value has to be a
`user` collection. Otherwise, @panic. The user_id has to be retrieved from this
field, and the meeting_user_id associated with it is then looked up in the
corresponding meeting.

```
{
  poll_id: new_poll.id,
  weight: old.weight,
  text: NULL,
  meeting_user_id: meeting_user_id from old.user_id and old.meeting_id
}
```

#### poll/result

Poll/result is a dict. There is one entry for each old option. The key is
the poll_option-id created above. The values "yes", "no" and "abstain" are
adopted as objects.

Example: `{"1":{"yes":"5","no":"1"},"2":{"yes":"1","abstain":"6"},"total_ballots":7}`

Calculation:
```
{
  for each option:
    option.id: {
      yes: option.yes -> string,  (skip if 0)
      no: option.no -> string,  (skip if 0)
      abstain: option.abstain -> string   (skip if 0)
    },

  total_ballots: count(all_options.vote_ids) -> number
}
```

#### poll_ballot

In the old system for each pair user-option a sepatate `vote` was created. A single
`poll_ballot` should be generated for each group of the votes with the same
`user_token`. All of these votes must have the same `weight` - else @panic.

Old votes that can be found via `old_poll.option_ids.vote_ids`.

Calculation (one `poll_ballot` per each `user_token`):

```
{
  poll_id: new_poll.id,
  weight: old.weight (must be same for all),
  split: false,
  value: see below,
  acting_meeting_user_id: meeting_user_id from old.delegated_user_id and poll.meeting,
  represented_meeting_user_id: meeting_user_id from old.user_id and poll.meeting
}
```

Example of `poll_ballot.value`: `{"1":"yes","2":"abstain"}`.

It's a dictionary where each key-value pair represents an old `vote`:

* key: vote.option_id -> should be replaced with the id of the new `poll_option`
  instances created above
* value: transformed vote.value:
      - Y -> yes
      - N -> no
      - A -> abstain
      - else @panic(impossible value)


## Information that will be lost:

* poll/backend: long or short
* poll/description: was not used
* poll/entitled_users_at_stop: only sata about the actual poll results is
  being migrated, but not who was entitled to vote
* For cumulative polls: poll.max_votes_per_option
* Global options are no longer listed separately, but are included in the result.
* poll/valid was previously counted separately. In future, it should be
  calculated by subtracting result.invalid from the total number of votes.


## Einzelvergleich

(Dieser Abschnitt ist möglicherweise veraltet. Ich gehe davon aus, dass es ihn
aufgrund der Beschreibung oben nicht braucht.)

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
* poll/is_pseudoanonymized: poll/anonymized.
* poll/pollmethod. Was removed, is now part of poll/config_id.
* poll/state: The value `published` was removed. polls in this state have to be set to `finished` and the field `poll/published` has to be set to true.
* poll/min_votes_amount, poll/max_votes_amount, poll/max_votes_per_option, poll/global_yes, poll/global_no, poll/global_abstain are removed. The new field poll/config has to be generated from them.
* poll/onehundred_percent_base moved to config_id and some options were removed or renamed. YNA -> valid, YN -> yes_no.
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


## Permissions

Mit dem neuen System werden auch Permissions angepasst. Hier der Diff:

https://github.com/OpenSlides/openslides-meta/pull/506/changes#diff-028ac608b338b62cdb586060b482ae073373845dd5de19118d2cbd253b597418

Es wurden neue Rechte eingefügt, die für die Migration nicht gebraucht werden.
Die einzige migrationsrelevante Änderung ist:

`poll.can_manage` heißt jetzt `agenda_item.can_manage_polls`.
