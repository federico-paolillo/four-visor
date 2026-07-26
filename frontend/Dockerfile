# syntax=docker/dockerfile:1.7

FROM node:26-bookworm AS frontend-build

WORKDIR /app/frontend

COPY package.json package-lock.json ./

RUN --mount=type=cache,target=/root/.npm \
    npm ci

COPY ./ ./

RUN --mount=type=cache,target=/root/.npm \
    npm run build

RUN cp -a dist /out \
    && find /out -type d -exec chmod 0555 {} + \
    && find /out -type f -exec chmod 0444 {} +

FROM caddy:2.11.3-builder AS caddy-build

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        xcaddy build v2.11.3 --output /out/caddy

RUN mkdir -p /out/etc/caddy /out/var/lib/caddy \
    && chmod 0555 /out/etc/caddy \
    && chmod 0755 /out/var/lib/caddy

COPY Caddyfile /out/etc/caddy/Caddyfile

RUN chmod 0444 /out/etc/caddy/Caddyfile

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

ENV HOME=/var/lib/caddy
ENV XDG_DATA_HOME=/var/lib/caddy

WORKDIR /srv

COPY --from=caddy-build --chown=0:0 --chmod=0555 /out/caddy /usr/bin/caddy
COPY --from=caddy-build --chown=0:0 /out/etc/caddy/ /etc/caddy/
COPY --from=caddy-build --chown=nonroot:nonroot /out/var/lib/caddy/ /var/lib/caddy/
COPY --from=frontend-build --chown=0:0 /out/ /srv/

USER nonroot:nonroot

EXPOSE 65101

ENTRYPOINT ["/usr/bin/caddy", "run", "--config", "/etc/caddy/Caddyfile"]
