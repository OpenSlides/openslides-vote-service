# OpenSlides Vote Service

The Vote Service is part of the OpenSlides environments. It handles the votes
for an electonic poll.


## Install and Start

### With Golang

```
go build ./cmd/vote
./vote
```


### With Docker

The docker build uses the redis messaging service, the auth token and the real
datastore service as default. Either configure it to use the fake services (see
environment variables below) or make sure the service inside the docker
container can connect to redis and the datastore-reader. For example with the
docker argument --network host. The auth-secrets have to given as a file.

```
docker build . --tag openslides-vote
printf "my_token_key" > auth_token_key 
printf "my_cookie_key" > auth_cookie_key
docker run --network host -v $PWD/auth_token_key:/run/secrets/auth_token_key -v $PWD/auth_cookie_key:/run/secrets/auth_cookie_key openslides-vote
```

It uses the host network to connect to redis.


### With Auto Restart

To restart the service when ever a source file has shanged, the tool
[CompileDaemon](https://github.com/githubnemo/CompileDaemon) can help.

```
go install github.com/githubnemo/CompileDaemon@latest
CompileDaemon -log-prefix=false -build "go build ./cmd/vote" -command "./vote"
```

The make target `build-dev` creates a docker image that uses this tool. The
environment varialbe `OPENSLIDES_DEVELOPMENT` is used to use default auth keys.

```
make build-dev
docker run --network host --env OPENSLIDES_DEVELOPMENT=true openslides-vote-dev
```


## Example Request with CURL

### Start a Poll

To start a poll a POST request has to be send to the create-url.

To send the same request twice is ok.

```
curl -X POST localhost:9013/internal/vote/create?id=1 
```


### Send a Vote

A vote-request is a post request with the ballot as body. Only logged in users
can vote. The body has to be valid json. For example for the value 'Y' you have
to send `{"value":"Y"}`.

This handler is not idempotent. If the same user sends the same data twice, it
is an error.

```
curl localhost:9013/system/vote?id=1 -d '{"value":"Y"}'
```


### Stop the Poll

With the stop request a poll is stopped and the vote values are returned. The
stop request is a POST request without a body.

A stop request can be send many times and will return the same data again.

```
curl -X POST localhost:9013/internal/vote/stop?id=1
```


### Clear the poll

After a vote was stopped and the data is successfully stored in the datastore, a
clear request should be used to remove the data from the vote service. This is
especially important on fast votes to remove the mapping between the user id and
the vote. The clear requet is idempotent.

```
curl -X POST localhost:9013/internal/vote/clear?id=1 
```


### Clear all polls

Only for development and debugging there is an internal route to clear all polls
at once. It there are many polls, this url could take a long time fully blocking
redis. Use this carfully.

```
curl -X POST localhost:9013/internal/vote/clear_all
```


### Have I Voted

A user can find out if he has voted for a list of polls.

```
curl localhost:9013/system/vote/voted?ids=1,2,3
```

The responce is a json-object in the form like this:

```
{
  "1":true,
  "2":false,
  "3":true
}
```


### Vote Count

With the vote count handler it is possible to find out how many user have voted
for a poll. The interface of this request is the same as the datastore-reader:

```
curl localhost:9013/internal/vote/vote_count -d '{"requests":["poll/1/vote_count","poll/2/vote_count]}'
```

The responce is a json-object like this:

```
{
  "poll":{
    "1":{
      "vote_count":23,
    },
    "2":{
      "vote_count":42,
    }
  }
}
```


## Configuration

### Environment variables

The Service uses the following environment variables:

* `VOTE_HOST`: The device where the service starts. The default is am empty
  string which starts the service on any device.
* `VOTE_PORT`: The port the vote service listens on. The default is `9013`. 
* `VOTE_BACKEND_FAST`: The backend used for fast polls. Possible backends are
  redis, postgres or memory. Default is `redis`.
* `VOTE_BACKEND_LONG`: The backend used for long polls. Default is `postgres`.
* `DATASTORE_READER_HOST`: Host of the datastore reader. The default is
  `localhost`.
* `DATASTORE_READER_PORT`: Port of the datastore reader. The default is `9010`.
* `DATASTORE_READER_PROTOCOL`: Protocol of the datastore reader. The default is
  `http`.
* `MESSAGING`: Sets the type of messaging service. `fake`(default) or
  `redis`.
* `MESSAGE_BUS_HOST`: Host of the redis server. The default is `localhost`.
* `MESSAGE_BUS_PORT`: Port of the redis server. The default is `6379`.
* `REDIS_TEST_CONN`: Test the redis connection on startup. Disable on the cloud
  if redis needs more time to start then this service. The default is `true`.
* `VOTE_REDIS_HOST`: Host of the redis used for the fast backend and the vote
  config. Default is `localhost'.
* `VOTE_REDIS_PORT`: Port of the redis host. Default is `6379`.
* `VOTE_DATABASE_USER`: Username of the postgres database for the long running
  backend. Default is `postgres`.
* `VOTE_DATABASE_PASSWORD`: Password for the postgres database. Default is
  `password`.
* `VOTE_DATABASE_HOST`: Host of the postgres database. Default is `localhost`.
* `VOTE_DATABASE_PORT`: Port of the postgres database. Default is `5432`.
* `VOTE_DATABASE_NAME`: Name of the postgres database. Default is `vote`.
* `AUTH`: Sets the type of the auth service. `fake` (default) or `ticket`.
* `AUTH_HOST`: Host of the auth service. The default is `localhost`.
* `AUTH_PORT`: Port of the auth service. The default is `9004`.
* `AUTH_PROTOCOL`: Protocol of the auth servicer. The default is `http`.
* `OPENSLIDES_DEVELOPMENT`: If set, the service starts, even when secrets (see
  below) are not given. The default is `false`. It also enables debug output.
* `VOTE_DISABLE_LOG`: Disables the debug log. Only relevant if `OPENSLIDES_DEVELOPMENT`
  is also set. In other case the debug log is disabled anyway. Default is `false`.


### Secrets

Secrets are filenames in `/run/secrets/`. The service only starts if it can find
each secret file and read its content. The default values are only used, if the
environment variable `OPENSLIDES_DEVELOPMENT` is set.

* `auth_token_key`: Key to sign the JWT auth tocken. Default `auth-dev-key`.
* `auth_cookie_key`: Key to sign the JWT auth cookie. Default `auth-dev-key`.
