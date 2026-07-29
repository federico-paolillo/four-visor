# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM node:26.5.0-bookworm-slim@sha256:607728e8751f69809c871e2cfa65d645f51ffacb5a0e9fc60f4cb7404ee5c16a AS build

ARG TARGETPLATFORM

RUN test "$TARGETPLATFORM" = "linux/amd64"

WORKDIR /src

COPY package.json package-lock.json ./

RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY . ./

RUN npm run build

FROM caddy:2.11.4-alpine@sha256:98eb57d882ccd5213d1688764db10c1ca2c58a1ca3a6717a3411ad798f7a423a AS caddy

COPY --chown=0:0 --chmod=0444 Caddyfile /etc/caddy/Caddyfile

FROM gcr.io/distroless/static-debian13:nonroot@sha256:23795be0fe67b7d47d1ee62b19c7db750152db627d5bbfa31307e892a7575bec AS runtime

WORKDIR /srv

COPY --from=caddy --chown=0:0 --chmod=0555 /usr/bin/caddy /usr/bin/caddy
COPY --from=caddy --chown=0:0 /etc/caddy/ /etc/caddy/
COPY --from=build --chown=0:0 /src/dist/ /srv/

USER 65532:65532

EXPOSE 65101

ENTRYPOINT ["/usr/bin/caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
