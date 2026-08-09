# ZTools Sync Server

English | [简体中文](./README.md)

ZTools Sync Server is the open-source, self-hosted synchronization backend for ZTools. It synchronizes plugin documents, document revision histories, checkpoints, and attachments.

## Quick Start

### Start Directly with Docker

This method does not require downloading the source code. Install Docker, replace the example password with your own strong password, and run:

```bash
docker volume create ztools-data

docker run -d \
  --name ztools-sync-server \
  --restart unless-stopped \
  -p 23517:23517 \
  -e TZ=Asia/Shanghai \
  -e ZTOOLS_USERNAME=root \
  -e ZTOOLS_PASSWORD='replace-with-a-strong-password' \
  -e ALLOW_REGISTRATION=false \
  -v ztools-data:/data \
  --read-only \
  --tmpfs /tmp:size=16m,mode=1777 \
  --security-opt no-new-privileges:true \
  happyzxing/ztools-sync-server:latest

docker ps --filter name=ztools-sync-server
curl http://127.0.0.1:23517/health
```

The server is ready when the health endpoint returns `"status":"ok"`. To follow the startup logs, run:

```bash
docker logs -f ztools-sync-server
```

### Configure the Login Account

Login credentials are passed through environment variables:

| Environment variable | Required | Description |
| --- | --- | --- |
| `ZTOOLS_USERNAME` | On first start | Owner username, for example `root` or `admin` |
| `ZTOOLS_PASSWORD` | On first start | Owner password; use a strong password and single quotes when it contains special characters |
| `ALLOW_REGISTRATION` | No | Whether other users may register; defaults to `false` |

`ZTOOLS_USERNAME` and `ZTOOLS_PASSWORD` must be configured together. If the database contains no users, the service creates this owner account on its first start.

Once the account has been created, restarting the container or changing these environment variables does not reset the password stored in the database. Keep the credentials used for the first start in a safe place.

### Start with Docker Compose

The Compose setup pulls the published Docker image directly and does not compile the Go source code locally. Install Docker Compose v2, then run:

```bash
git clone https://github.com/ZToolsCenter/ztools-sync-server.git
cd ztools-sync-server
cp .env.example .env
```

Open `.env` and configure at least these values:

```env
ZTOOLS_USERNAME=root
ZTOOLS_PASSWORD=replace-with-a-strong-password
ALLOW_REGISTRATION=false
```

Then start and check the service:

```bash
docker compose up -d
docker compose ps
curl http://127.0.0.1:23517/health
```

To follow logs for the Compose deployment:

```bash
docker compose logs -f ztools-sync
```

### Connect ZTools

Docker maps the service to port `23517` on all host network interfaces by default so that other devices can reach it. In ZTools, open Settings, choose `Private deployment`, enter the server's LAN address or domain name (for example, `http://192.168.1.10:23517`), and sign in with the `ZTOOLS_USERNAME` and `ZTOOLS_PASSWORD` configured above.

Docker Compose pulls `happyzxing/ztools-sync-server` from Docker Hub by default. To use GHCR instead, add the following setting to `.env`:

```env
ZTOOLS_SYNC_IMAGE=ghcr.io/ztoolscenter/ztools-sync-server
```

When using `docker run`, replace the image name at the end of the command with `ghcr.io/ztoolscenter/ztools-sync-server:latest`.

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

The server provides HTTP and WebSocket endpoints but does not terminate TLS. The quick-start configuration maps port `23517` on all host network interfaces for trusted LAN access. Configure the host firewall so that only devices that need the service can reach this port.

For public access, place Caddy, Nginx, or another TLS reverse proxy in front of the service and connect using `https://` or `wss://`. For local-only access, change the port mapping to `127.0.0.1:23517:23517`.

Do not expose the unencrypted `23517` port directly to the public internet.

## Backup

Stop the container before backing up the SQLite data volume. For a direct Docker deployment, run:

```bash
docker stop ztools-sync-server
```

For a Compose deployment, run:

```bash
docker compose stop ztools-sync
```

Copying only `ztools.db` while the service is running can miss transactions still present in the WAL file. After the backup completes, use the corresponding command to start the service again:

```bash
# Direct Docker deployment
docker start ztools-sync-server

# Docker Compose
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
