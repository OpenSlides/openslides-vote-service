ARG CONTEXT=prod

FROM golang:1.24.2-alpine as base

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
COPY ./dev/command.sh ./
RUN chmod +x command.sh
CMD ["./command.sh"]



# Development Image

FROM base as dev

WORKDIR /root

## Command (workdir reset)
COPY ./dev/command.sh ./
RUN chmod +x command.sh
CMD ["./command.sh"]
RUN ["go", "install", "github.com/githubnemo/CompileDaemon@latest"]



# Testing Image

FROM base as tests

RUN apk add --no-cache build-base



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
