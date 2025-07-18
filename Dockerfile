FROM golang:1.24.5-alpine as base
WORKDIR /root/openslides-vote-service

RUN apk add git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build service in seperate stage.
FROM base as builder
RUN go build


# Test build.
FROM base as testing

RUN apk add build-base

CMD go vet ./... && go test ./...


# Development build.
FROM base as development

RUN ["go", "install", "github.com/githubnemo/CompileDaemon@latest"]
EXPOSE 9012

WORKDIR /root
CMD CompileDaemon -log-prefix=false -build="go build -o vote-service ./openslides-vote-service" -command="./vote-service"


# Productive build
FROM scratch

LABEL org.opencontainers.image.title="OpenSlides Vote Service"
LABEL org.opencontainers.image.description="The OpenSlides Vote Service handles the votes for electronic polls."
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.source="https://github.com/OpenSlides/openslides-vote-service"

COPY --from=builder /root/openslides-vote-service/openslides-vote-service .
EXPOSE 9013

ENTRYPOINT ["/openslides-vote-service"]
HEALTHCHECK CMD ["/openslides-vote-service", "health"]
