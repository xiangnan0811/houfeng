# syntax=docker/dockerfile:1

ARG NODE_VERSION=22
ARG GO_VERSION=1.26.2

FROM node:${NODE_VERSION}-bookworm-slim AS web-build
WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM golang:${GO_VERSION}-bookworm AS go-build
WORKDIR /src

ARG VERSION=dev
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY db/ ./db/
COPY internal/ ./internal/
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/houfeng-center ./cmd/houfeng-center

FROM debian:bookworm-slim AS runtime

RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends ca-certificates; \
	rm -rf /var/lib/apt/lists/*; \
	groupadd --system houfeng; \
	useradd --system --gid houfeng --home-dir /app --shell /usr/sbin/nologin houfeng

WORKDIR /app

ENV HOUFENG_HTTP_ADDR=:16001
ENV HOUFENG_WEB_DIST_DIR=/app/web/dist

COPY --from=go-build --chown=houfeng:houfeng /out/houfeng-center /usr/local/bin/houfeng-center
COPY --from=web-build --chown=houfeng:houfeng /src/web/dist/ /app/web/dist/
COPY --chown=houfeng:houfeng scripts/docker-entrypoint.sh /usr/local/bin/houfeng-docker-entrypoint
RUN chmod 0755 /usr/local/bin/houfeng-docker-entrypoint

USER houfeng
EXPOSE 16001
ENTRYPOINT ["/usr/local/bin/houfeng-docker-entrypoint"]
CMD ["/usr/local/bin/houfeng-center"]
