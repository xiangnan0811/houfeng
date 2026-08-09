# syntax=docker/dockerfile:1

ARG NODE_VERSION=22
ARG GO_VERSION=1.26.2

FROM node:${NODE_VERSION}-bookworm-slim AS web-build
WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY internal/center/http/csp-policy.txt /src/internal/center/http/csp-policy.txt
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
RUN go build -trimpath -ldflags "-s -w" -o /out/houfeng-content-processor ./cmd/houfeng-content-processor

FROM debian:bookworm-slim AS runtime

RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends ca-certificates poppler-utils; \
	rm -rf /var/lib/apt/lists/*; \
	groupadd --system --gid 10001 houfeng; \
	useradd --system --uid 10001 --gid houfeng --home-dir /app --shell /usr/sbin/nologin houfeng; \
	install -d -o houfeng -g houfeng -m 0755 /var/log/houfeng; \
	install -d -o houfeng -g houfeng -m 0700 /var/lib/houfeng/attachments /var/lib/houfeng/processor-workspace

WORKDIR /app

ENV HOUFENG_HTTP_ADDR=:16001
ENV HOUFENG_WEB_DIST_DIR=/app/web/dist
ENV HOUFENG_LOG_FILE=/var/log/houfeng/center.log

COPY --from=go-build --chown=houfeng:houfeng /out/houfeng-center /usr/local/bin/houfeng-center
COPY --from=go-build --chown=houfeng:houfeng /out/houfeng-content-processor /usr/local/bin/houfeng-content-processor
COPY --from=web-build --chown=houfeng:houfeng /src/web/dist/ /app/web/dist/
COPY scripts/docker-entrypoint.sh /usr/local/bin/houfeng-docker-entrypoint
RUN chmod 0755 /usr/local/bin/houfeng-docker-entrypoint

EXPOSE 16001
USER houfeng:houfeng
ENTRYPOINT ["/usr/local/bin/houfeng-docker-entrypoint"]
CMD ["/usr/local/bin/houfeng-center"]
