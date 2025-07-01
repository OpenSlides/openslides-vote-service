ARG CONTEXT=prod

FROM golang:1.24.4-alpine as base

## Setup
ARG CONTEXT
WORKDIR /app/openslides-vote-service
ENV APP_CONTEXT=${CONTEXT}

## Install
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

## External Information
LABEL org.opencontainers.image.title="OpenSlides Vote Service"
LABEL org.opencontainers.image.description="The OpenSlides Vote Service handles the votes for electronic polls."
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.source="https://github.com/OpenSlides/openslides-vote-service"

EXPOSE 9013

## Command
HEALTHCHECK CMD ["/app/openslides-vote-service/openslides-vote-service", "health"]

# Development Image

FROM base as dev

RUN ["go", "install", "github.com/githubnemo/CompileDaemon@latest"]

CMD CompileDaemon -log-prefix=false -build="go build" -command="./openslides-vote-service"

# Testing Image

FROM base as tests

RUN apk add --no-cache build-base

RUN go install golang.org/x/lint/golint@latest

# Production Image

FROM base as builder
RUN go build

FROM scratch as prod

WORKDIR /
ENV APP_CONTEXT=prod

COPY --from=builder /app/openslides-vote-service/openslides-vote-service .

## External Information
LABEL org.opencontainers.image.title="OpenSlides Vote Service"
LABEL org.opencontainers.image.description="The OpenSlides Vote Service handles the votes for electronic polls."
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.source="https://github.com/OpenSlides/openslides-vote-service"

EXPOSE 9013

## Command
ENTRYPOINT ["/openslides-vote-service"]

HEALTHCHECK CMD ["/openslides-vote-service", "health"]
