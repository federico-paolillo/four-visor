# syntax=docker/dockerfile:1.7

FROM golang:1.26-bookworm AS build

WORKDIR /src

COPY go.mod src/go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY ./ ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -trimpath -ldflags="-s -w" -o /out/four-visor ./cmd/app

RUN chmod 0555 /out/four-visor

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app

COPY --from=build --chown=0:0 /out/four-visor /usr/bin/four-visor

USER nonroot

ENTRYPOINT ["/usr/bin/four-visor"]

EXPOSE 65102

CMD ["process"]
