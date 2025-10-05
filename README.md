# OpenSlides Vote Service

The Vote Service is part of the OpenSlides environments. It is responsible for
the `poll` and `vote` collections. It handles the electronic voting.

The service has no internal state but uses the normal postgres database to save
the polls.


## Handlers

All requests to the vote-service have to be POST-requests.

With the exception of the "vote", all requests can only be sent by a manager.
The permission depends on the field `content_object_id` of the corresponding poll.

- motions: `motion.can_manage`
- assignments: `assignment.can_manage`
- topic: `poll.can_manage`

With the exception of the create request, all requests need an http-get-argument
in the url, to specify the poll-id. For example `/system/vote/update?id=23`


### Create a poll

`/system/vote/create`

The permissions for the create requests are a bit different, since the poll does
not exist in the database, when the request is sent. Therefore the permission
check depends on the field `content_object_id` in the request body.

The request expects a body with the fields to create the poll:

- `title` (required)
- `description` (optional)
- `content_object_id` (required)
- `meeting_id` (required)
- `method` (required)
- `config` (depends on the method)
- `visibility` (required)
- `entitled_group_ids` (only if visibility != manually)
- `live_voting_enabled` (only if visibility != manually)
- `result` (only if visibility == manually)


### Update a poll

`/system/vote/update?id=XX`

The fields `content_object_id` and `meetin_id` can not be changed. You have to
create a new poll to "update" them.

The fields `method`, `config`, `visibility` and `entitled_group_ids` can only be
changed, before the poll has started. You can reset a poll to change this
values.


### Delete a poll

`/system/vote/delete?id=XX`

The delete request removes the poll and all its votes in any state. Be careful.


### Start a poll

`/system/vote/start?id=XX`

To start a poll means that the users can send their votes.


### Finalize a poll

`/system/vote/finalize?id=XX`

To finalize a poll means that users can not send their votes anymore. It
creates the `poll/result` field.

The request has two optional attributes: `publish` and `anonymize`. `publish`
sets the field `poll/state` to `published`. `anonymize` removes all user ids
from the corresponding `vote` objects.

The request can be send many times. It only creates the result the first time.
`publish` and `anonymize` can be used on a later request.

To stop a poll and publish and anonymize it at the same time, the following request can be used:

`/system/vote/finalize?id=XX&publish&anonymize`


### Reset a poll

`/system/vote/reset?id=XX`

Reset sets the state back to `created` and removes all vote objects.


### Send a vote

A vote-request is a post request with the ballot as body. Only logged in users
can vote. The body has to be valid json.

The service distinguishes between two users on each vote-request. The acting user
is the request user, that sends the vote-request. The represented user is the
user, for whom the vote is sent. Both users can actually be the same user.

The acting user has to be present in the meeting and needs the permission to vote
for the represented user. The represented user has to be in one of the group of
the field `poll/entitled_group_ids`.

The request body has to be in the form:

```json
{
  "user_id": 23,
  "value": "Yes"
}
```

In this example, the request user would send the Vote `Yes` for the user with
the id 23. If the acting user and the represented user are the same, then field
`user_id` is not needed.

Valid values for the vote depend on the `poll/method`.


### Read the poll

The service only handles write requests. All Reads have to be done via the
autoupdate-service.


## poll/visibility

The field `poll/visibility` can be one of `manually`, `named`, `open` and
`secret`.


### manually

Manually polls are polls without electronic voting. The result is calculated
from individual vote-requests from the users, but the manager sets the result
manually.

Manually polls behave diferently. When created, the field `poll/state` is set to
`finished`. The poll result can be set either wih the create request or with an
update request. The server does not validate the field `poll/result`, but accept
any string.

vote-requests are not possible. A finalize-request is possible, but only to set
the `poll/published` field. A reset-request sets/leaves the state at `finished`.


### named and open

At the moment, the visibilities `named` and `open` behave nearly the same. They
have two different meanings. In future versions, there will probably be
different features for this two modes.

The value `named` means that the mapping between votes and users is not deleted
at the end. In a political context, a `named`-poll also means that eligible
voters are called individually, publicly, and one after another, and asked for
their vote. In the future, a feature could be considered where, for
`named`-polls, users cannot vote themselves, but instead the manager is guided
through a form in which they can enter the votes for all eligible voters one
after another. A `named`-poll can not be anonymized.

The value `open` is likely the normal case for a vote. The mapping between votes
and users CAN be deleted afterwards with the `anonymize` flag of the finalize
handler.


### secret

At the moment, a `secret`-poll is identical to an `open`- or `named`-poll. But
is handled differently in the autoupdate-service. The field
`vote/acting_user_id` and `vote/represented_user_id` get restricted for
everyboddy.

For the future, this value will be used for crypto votes. See the entry in the
[wiki](https://github.com/OpenSlides/OpenSlides/wiki/DE%3AKonzept-geheime-Wahlen-mit-OpenSlides)


## Poll methods

The values of `poll/config`, `vote/value` and `poll/result` depend on the field
`poll/method`.


### approval

On an approval poll, the users can vote with `yes`, `no` or `abstain`. This is
the usual method to vote on a motion.


#### poll/config

`allow_abstain`: if set to `true`, users are allowed to vote with `abstain`. The
default is `true`.


#### vote/value

Valid votes look like: `{"value":"yes"}`, `{"value":"no"}` or
`{"value":"abstain"}`.


#### poll/result

The poll result looks like:

`{"yes": "32", "no": "20", "abstain": "10", "invalid": 2}`

Attributes with a zero get discarded.

The values are decimal values decoded as string. See [Vote Weight](#vote-weight).


### selection

On a selection poll, the users select one or many options from a list of
options. For example one candidate in a assignment-poll.


#### poll/config

`options` (required): map from a string to any value. The strings can by
anything. For example assignment-candidate-ids, encoded as strings. The values
are not used by the server, `null` would be valid values. The values can be used
to descript the values, if the `poll/config` get inspected by a human. For
example, it could be the username of the assignment-candidate:
`{"options":{"1":"Max","2":"Hubert"}}`

`max_options_amount`: The maximal amount of options a user can vote on. For
example, with a value of `1`, a user is only allowed to vote for one candidate.
The default is no limit.

`min_options_amount`: The minimum amount of options, a user has to vote on. The
default is no limit.

`allow_nota`: Allow `nota` votes, where the user can disprove of all options.


#### vote/value

TODO


#### poll/result

TODO


### rating-score


### rating approval


## Delegation

A user can delegate his voice to another user. This is only possible in a
meeting, where `meeting/users_enable_vote_delegation` is set to true.

The term `acting_user` means the user, that sends the request. The term
`represented_user` is the user, for whom the acting user sends the vote.

If `meeting/users_forbid_delegator_to_vote` is set to true, then only the user,
where the voice was delegated can vote. If set to `false`, then the
represented_user keeps the permission to vote for himself.


## Vote Weight

Everyvote has a weight. It is a decimal number. The default is `1.000000`. When
`meeting/users_enable_vote_weight` is set to `true`, this value can be changed
for each user. Each user has a default vote weight (`user/default_vote_weight`),
that can be changed for each meeting (`meeting_user/vote_weight`).

This weight is saved vote (`vote/weight`) and taken into account when generating
the result.

The weight is a not a floating number, but a decimal number. JSON can not
represent decimal numbers, so they are represented as strings. This is also the
reason, that the vote results are represented as strings.

This feature does not work on crypto votes, since the server does not know,
which vote belongs to which user.


## Vote Splitting

Not implemented yet.

https://github.com/OpenSlides/openslides-vote-service/issues/392


## Invalid Votes

Normaly, the services validates the vote requests from the users. So invalid
votes in the database and therefore in the `poll/result` should not be possible.

When the field `poll/allow_invalid` is set to true, then the service skips the
validation and saves the vote exactly, how the user has provided it. An invalid
vote can have any (invalid) value.

On crypto votes, the server can not read the value and has to accept it. Invalid
votes also accur, when the value can not be decrypted.

When a poll has invalid votes, the amount gets written in the poll result. for example:

`{"invalid":1,"no":"1","yes":"2"}`

The value is an integer and not a decimal value decoded as string.


## Configuration of the service

The service is configured with environment variables. See [all environment variables](environment.md).
