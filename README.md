# ZTools Sync Server

ZTools Sync Server is the open-source, self-hosted synchronization backend for ZTools.
It synchronizes plugin documents, revision histories, checkpoints and attachments. It does not
include the ZTools plugin market, comments, notifications, application updates, analytics or the
SaaS administration console.

## Quick Start

```bash
cp .env.example .env
# Set a strong ZTOOLS_PASSWORD in .env
docker compose up -d
```

The service listens on `127.0.0.1:23517` by default. In ZTools, open Settings, choose
`Private deployment`, enter the server address and sign in with the account configured in `.env`.

Docker Compose pulls `happyzxing/ztools-sync-server` from Docker Hub by default. To use GHCR
instead, set `ZTOOLS_SYNC_IMAGE=ghcr.io/ztoolscenter/ztools-sync-server` in `.env`.

The first start creates the configured owner account. Subsequent starts never reset an existing
password from environment variables. Public registration is disabled by default.

## Storage

SQLite is the default and stores all data in the `ztools-data` Docker volume. The server enables
WAL mode, a five-second busy timeout and one database connection for predictable operation on
small machines. Run only one container replica when using SQLite.

MySQL is also supported:

```env
DB_DRIVER=mysql
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_DATABASE=ztools_sync
MYSQL_USER=ztools
MYSQL_PASSWORD=replace-me
```

## Network Security

The server provides HTTP and WebSocket endpoints but does not terminate TLS. Keep the default
loopback port mapping for local use. For remote access, put it behind Caddy, Nginx or another TLS
reverse proxy and connect with `https://` or `wss://`.

## Backup

Stop the container before backing up the `ztools-data` volume. Copying only `ztools.db` while the
service is running can miss transactions still present in the WAL file.

## Development

```bash
go test ./...
go run ./cmd/ztools-sync-server
```

The public packages are also consumed by the private ZTools SaaS server. Changes to authentication,
the synchronization protocol or database behavior must keep both SQLite and MySQL tests passing.

## License

Licensed under the Mozilla Public License 2.0. See [LICENSE](LICENSE).
