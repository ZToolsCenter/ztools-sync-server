FROM golang:1.23-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY auth ./auth
COPY config ./config
COPY models ./models
COPY repository ./repository
COPY store ./store
COPY sync ./sync
COPY transport ./transport
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/ztools-sync-server ./cmd/ztools-sync-server

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 ztools \
    && useradd --uid 10001 --gid ztools --no-create-home --home-dir /data ztools \
    && install -d -o ztools -g ztools -m 0700 /data

COPY --from=builder /out/ztools-sync-server /usr/local/bin/ztools-sync-server

USER ztools
ENV PORT=23517 \
    DATA_DIR=/data \
    DB_DRIVER=sqlite
VOLUME ["/data"]
EXPOSE 23517

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl --fail --silent http://127.0.0.1:23517/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/ztools-sync-server"]
