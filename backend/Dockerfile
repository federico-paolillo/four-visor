# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM golang:1.26.5-bookworm@sha256:3f6236bd765f898a2a3c2946112b04097814c4529d44534674700cd07b9c6b4c AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -mod=readonly -trimpath -buildvcs=false \
        -ldflags="-s -w -buildid=" -o /out/four-visor ./cmd/app

FROM gcr.io/distroless/static-debian13:nonroot@sha256:23795be0fe67b7d47d1ee62b19c7db750152db627d5bbfa31307e892a7575bec AS runtime

WORKDIR /

ENV FOURVISOR_SERVER_ADDRESS=:65102 \
    FOURVISOR_MEMCACHED_ADDRESS="" \
    FOURVISOR_OTLP_ENDPOINT=http://otelcol:65103 \
    FOURVISOR_ACQUISITION_RATE_INTERVAL=1s \
    FOURVISOR_ACQUISITION_MAX_CONCURRENCY=10 \
    FOURVISOR_ACQUISITION_REQUEST_TIMEOUT=5s \
    FOURVISOR_ACQUISITION_MAX_RETRIES=2 \
    FOURVISOR_ACQUISITION_RETRY_BACKOFF=1s \
    FOURVISOR_ACQUISITION_DEADLINE=4h \
    FOURVISOR_SYNCHRONIZATION_INTERVAL=4h \
    FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE=10 \
    FOURVISOR_COMMIT_HASH=""

COPY --from=build --chown=0:0 --chmod=0555 /out/four-visor /usr/bin/four-visor

USER 65532:65532

EXPOSE 65102

ENTRYPOINT ["/usr/bin/four-visor"]
