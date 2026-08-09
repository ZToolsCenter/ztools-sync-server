# ZTools Sync Server

[English](./README_EN.md) | 简体中文

ZTools Sync Server 是 ZTools 开源、可自行部署的同步服务端，用于同步插件文档、文档修订历史、同步检查点和附件。

## 快速启动

### 使用 Docker 直接启动

这种方式无需下载源码。请先安装 Docker，然后执行以下命令；启动前必须将示例密码替换为你自己的强密码：

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

健康检查返回 `"status":"ok"` 即表示启动成功。如需查看启动日志：

```bash
docker logs -f ztools-sync-server
```

### 配置登录账号和密码

账号信息通过环境变量传入：

| 环境变量 | 是否必填 | 说明 |
| --- | --- | --- |
| `ZTOOLS_USERNAME` | 首次启动必填 | 所有者账号，例如 `root` 或 `admin` |
| `ZTOOLS_PASSWORD` | 首次启动必填 | 所有者密码，请使用强密码；包含特殊字符时应使用单引号包裹 |
| `ALLOW_REGISTRATION` | 否 | 是否允许其他用户注册，默认 `false` |

`ZTOOLS_USERNAME` 和 `ZTOOLS_PASSWORD` 必须同时配置。数据库中还没有用户时，服务会在首次启动时创建这个所有者账号。

账号创建后，重启容器或修改环境变量都不会重置数据库中已有账号的密码。请妥善保存第一次启动时使用的账号和密码。

### 使用 Docker Compose 启动

Compose 方式会直接拉取已发布的 Docker 镜像，不会在本地编译 Go 源码。请先安装 Docker Compose v2，然后执行：

```bash
git clone https://github.com/ZToolsCenter/ztools-sync-server.git
cd ztools-sync-server
cp .env.example .env
```

打开 `.env`，至少修改以下配置：

```env
ZTOOLS_USERNAME=root
ZTOOLS_PASSWORD=replace-with-a-strong-password
ALLOW_REGISTRATION=false
```

然后启动并检查服务：

```bash
docker compose up -d
docker compose ps
curl http://127.0.0.1:23517/health
```

Compose 方式查看日志：

```bash
docker compose logs -f ztools-sync
```

### 连接 ZTools

Docker 默认将服务映射到宿主机所有网络接口的 `23517` 端口，以便其他设备访问。在 ZTools 中打开设置，选择“私有部署”，填写服务器的局域网 IP 或域名（例如 `http://192.168.1.10:23517`），并使用上面配置的 `ZTOOLS_USERNAME` 和 `ZTOOLS_PASSWORD` 登录。

Docker Compose 默认从 Docker Hub 拉取 `happyzxing/ztools-sync-server`。如需使用 GHCR，在 `.env` 中添加：

```env
ZTOOLS_SYNC_IMAGE=ghcr.io/ztoolscenter/ztools-sync-server
```

使用 `docker run` 时，可以将命令末尾的镜像名称替换为 `ghcr.io/ztoolscenter/ztools-sync-server:latest`。

## 存储

默认使用 SQLite，所有数据保存在 Docker 的 `ztools-data` 数据卷中。服务会启用 WAL 模式、5 秒 busy timeout，并将数据库连接数限制为 1，以便在低配置设备上稳定运行。

使用 SQLite 时只能运行一个服务容器，不要启动多个副本共同访问同一个数据卷。

项目也支持 MySQL，运行二进制或自定义容器部署时可配置：

```env
DB_DRIVER=mysql
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_DATABASE=ztools_sync
MYSQL_USER=ztools
MYSQL_PASSWORD=replace-me
```

也可以通过 `MYSQL_DSN` 直接提供完整的 MySQL DSN。

## 网络安全

服务提供 HTTP 和 WebSocket 接口，但不负责 TLS 终止。快速启动示例会将 `23517` 端口映射到宿主机所有网络接口，适合受信任的局域网；请同时确认系统防火墙只允许需要访问的设备。

需要从公网访问时，应在服务前配置 Caddy、Nginx 或其他 TLS 反向代理，并使用 `https://` 或 `wss://` 连接。仅允许本机访问时，可以将端口映射改为 `127.0.0.1:23517:23517`。

不要直接将未加密的 `23517` 端口暴露到公网。

## 备份

备份 SQLite 数据卷前应先停止容器。使用 Docker 直接启动时执行：

```bash
docker stop ztools-sync-server
```

使用 Compose 启动时执行：

```bash
docker compose stop ztools-sync
```

服务运行时只复制 `ztools.db` 可能遗漏仍在 WAL 文件中的事务。备份完成后，按对应方式重新启动：

```bash
# Docker 直接启动
docker start ztools-sync-server

# Docker Compose
docker compose start ztools-sync
```

## 本地开发

需要 Go 1.23 或更高版本：

```bash
go test ./...
go run ./cmd/ztools-sync-server
```

公共包也会被 ZTools SaaS 服务端复用。修改认证、同步协议或数据库行为时，必须确保 SQLite 和 MySQL 测试均通过。

## 许可证

本项目使用 Mozilla Public License 2.0，详见 [LICENSE](LICENSE)。
