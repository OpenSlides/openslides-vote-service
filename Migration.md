Poll related model changes:

* meeting/poll_default_backend was removed. No migration necessary. Just remove the value.
* motion/option_ids was removed. I think, it can just be removed (ignored) since it has no meaning. I am not 100% sure.
* poll/description was removed. No migration needed. Was not used before.
* poll/type was renamed to poll/visibility and the values have changed.
  * "analog" -> "manually"
  * "named": Its not clear to me if old "named" values should be "named" in the new system or "open". I think, "named" is ok.
  * "pseudoanonymous" -> "secret"
  * "cryptographic": There should be no case. If so, "secret" can be used.

* poll/backend: was removed. No migration necessary.
* poll/is_pseudoanonymized: Was removed. No migraton necessary.
* poll/pollmethod was renamed to method and the values have changed. See "New fields" for the new value.
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
* vote/user_id was renamed to vote/acting_user_id. Is this correct?
* vote/delegated_user_id was renamed to vote/represented_user_id. Is this correct?
* vote/meeting_id was removed. No migration necessary.


# New fields

* poll/method -> See below
* poll/config -> See below
* poll/result -> See below
* poll/published -> true, if poll/state was `published`
* poll/allow_invalid -> always `false`
* vote/poll_id -> see vote/option_id -> option/poll_id in the old poll


## poll/method

for motion-polls, the new value is `approval`.
for assignment-polls, the new value is `rating-approval`.
For topic-polls: TODO


## poll/config

For motion-pools: if the old value of `poll/pollmethod` was `YNA`, use an empty
value. If it was `YN`, use `{"allow_abstain": false}`. For eny other value, panic.

For assignment-polls: TODO

For topic-pools: TODO


## poll/result

TODO:

## vote/value

The value has changed: TODO
