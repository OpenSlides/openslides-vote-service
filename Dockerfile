ARG CONTEXT=prod

FROM golang:1.24.3-alpine as base

ARG CONTEXT
WORKDIR /root/openslides-vote-service

## Context-based setup
### Add context value as a helper env variable
ENV ${CONTEXT}=1

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
HEALTHCHECK CMD ["/openslides-vote-service", "health"]



# Development Image

FROM base as dev

WORKDIR /root

RUN ["go", "install", "github.com/githubnemo/CompileDaemon@latest"]

CMD CompileDaemon -log-prefix=false -build="go build -o vote-service ./openslides-vote-service" -command="./vote-service"


# Testing Image

FROM base as tests

RUN apk add --no-cache build-base

CMD go test -test.short -race -timeout 12s ./...


# Production Image

FROM base as builder
RUN go build


FROM scratch as prod

WORKDIR /

COPY --from=builder /root/openslides-vote-service/openslides-vote-service .

## External Information
LABEL org.opencontainers.image.title="OpenSlides Vote Service"
LABEL org.opencontainers.image.description="The OpenSlides Vote Service handles the votes for electronic polls."
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.source="https://github.com/OpenSlides/openslides-vote-service"

EXPOSE 9013

## Command
ENTRYPOINT ["/openslides-vote-service"]
