# ZTools Sync Server

English | [简体中文](./README.md)

ZTools Sync Server is the open-source, self-hosted synchronization backend for ZTools. It synchronizes plugin documents, document revision histories, checkpoints, and attachments.

This repository contains only the synchronization capabilities required for a standalone deployment. It does not include the ZTools plugin market, comments, notifications, application updates, analytics, or the SaaS administration console.

## Quick Start

Install Docker and Docker Compose v2, then run:

```bash
git clone https://github.com/ZToolsCenter/ztools-sync-server.git
cd ztools-sync-server
cp .env.example .env
```

Open `.env` and replace `ZTOOLS_PASSWORD` with a strong password. Then start the service:

```bash
docker compose up -d
docker compose ps
curl http://127.0.0.1:23517/health
```

The server is ready when the health endpoint returns `"status":"ok"`. To follow the startup logs, run:

```bash
docker compose logs -f ztools-sync
```

The service listens on `127.0.0.1:23517` by default. In ZTools, open Settings, choose `Private deployment`, enter the server address, and sign in with the account configured in `.env`.

Docker Compose pulls `happyzxing/ztools-sync-server` from Docker Hub by default. To use GHCR instead, add the following setting to `.env`:

```env
ZTOOLS_SYNC_IMAGE=ghcr.io/ztoolscenter/ztools-sync-server
```

The first start creates the owner account specified by `ZTOOLS_USERNAME` and `ZTOOLS_PASSWORD`. Subsequent starts never reset the password of an existing account from environment variables. Public registration is disabled by default.

## Storage

SQLite is the default. All data is stored in the `ztools-data` Docker volume. The server enables WAL mode, a five-second busy timeout, and a single database connection for predictable operation on small machines.

Run only one service container when using SQLite. Multiple replicas must not share the same SQLite data volume.

MySQL is also supported. Configure these variables when running the binary or a custom container deployment:

```env
DB_DRIVER=mysql
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_DATABASE=ztools_sync
MYSQL_USER=ztools
MYSQL_PASSWORD=replace-me
```

You can also provide a complete MySQL DSN through `MYSQL_DSN`.

## Network Security

The server provides HTTP and WebSocket endpoints but does not terminate TLS. Keep the default loopback port mapping for local-only use. For public access, place Caddy, Nginx, or another TLS reverse proxy in front of the service and connect using `https://` or `wss://`.

Do not expose the unencrypted `23517` port directly to the public internet.

## Backup

Stop the container before backing up the SQLite data volume:

```bash
docker compose stop ztools-sync
```

Copying only `ztools.db` while the service is running can miss transactions still present in the WAL file. Start the service again after the backup completes:

```bash
docker compose start ztools-sync
```

## Development

Go 1.23 or later is required:

```bash
go test ./...
go run ./cmd/ztools-sync-server
```

The public packages are also consumed by the ZTools SaaS server. Changes to authentication, the synchronization protocol, or database behavior must keep both SQLite and MySQL tests passing.

## License

Licensed under the Mozilla Public License 2.0. See [LICENSE](LICENSE).
