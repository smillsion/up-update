# 部署指南

[返回首页](../README.md) · [使用指南](USAGE.md) · [配置说明](CONFIGURATION.md)

本文介绍使用 Docker 部署、升级和维护 up-update。当前版本只支持单实例运行。

## 环境要求

- Linux 服务器，推荐 x86-64 或 ARM64
- Docker Engine 20.10+
- Docker Compose v2，可选
- Git
- Nginx 或其他反向代理，公网部署时推荐

服务器至少需要能够访问 Docker 镜像仓库、npm 依赖源和 Go 模块代理。`deploy.sh` 默认使用 `https://goproxy.cn,direct` 下载 Go 依赖。

## 获取代码

```bash
cd /usr/local
git clone -b main https://gitee.com/birdKiss/up-update.git
cd up-update
```

私有仓库或不希望输入密码时，可以为 Gitee 配置部署密钥后使用 SSH 地址克隆。

## 准备环境变量

```bash
cp .env.example .env
openssl rand -hex 32
chmod 600 .env
```

编辑 `.env`：

```dotenv
UP_UPDATE_HTTP_PORT=8080
UP_UPDATE_ADMIN_USERNAME=admin
UP_UPDATE_ADMIN_PASSWORD=replace-with-at-least-10-characters
UP_UPDATE_ENCRYPTION_KEY=replace-with-64-random-hex-characters
UP_UPDATE_SECURE_COOKIES=false
TZ=Asia/Shanghai
```

将 `openssl` 生成的 64 位十六进制字符串写入 `UP_UPDATE_ENCRYPTION_KEY`。公网 HTTPS 部署必须将 `UP_UPDATE_SECURE_COOKIES` 设置为 `true`。完整字段说明见[配置说明](CONFIGURATION.md)。

## 使用 Docker Compose

有 Docker Compose v2 时执行：

```bash
docker compose up -d --build
docker compose ps
docker compose logs --tail 100 up-update
```

默认访问地址为 `http://服务器地址:8080`，打开后会先进入项目主页，再点击“登录系统”进入管理界面。修改 `UP_UPDATE_HTTP_PORT` 后，使用新的主机端口访问。

Compose 配置默认将端口发布到服务器所有网络接口。如果同机使用 Nginx，建议将 `docker-compose.yml` 中的端口改为仅绑定本机：

```yaml
ports:
  - "127.0.0.1:${UP_UPDATE_HTTP_PORT:-8080}:8080"
```

## 没有 Docker Compose

项目根目录的 `deploy.sh` 会检查 `.env`、构建镜像、保留数据卷、替换容器并等待健康检查通过：

```bash
chmod +x deploy.sh
./deploy.sh
```

脚本默认使用以下资源：

| 资源 | 默认值 |
| --- | --- |
| 镜像 | `up-update:local` |
| 容器 | `up-update` |
| 数据卷 | `up-update-data` |
| 监听地址 | `127.0.0.1:${UP_UPDATE_HTTP_PORT}` |

需要更换 Go 模块代理时：

```bash
GOPROXY=https://其他代理地址,direct ./deploy.sh
```

## 检查运行状态

```bash
docker ps --filter name=up-update
docker logs --tail 100 up-update
curl http://127.0.0.1:8080/healthz
```

如果 `.env` 使用了其他端口，请替换健康检查地址中的 `8080`。正常响应为 HTTP 200。

持续查看日志：

```bash
docker logs -f up-update
```

## 配置 Nginx 和 HTTPS

假设：

- 域名为 `up-update.example.com`
- `UP_UPDATE_HTTP_PORT=10025`
- 证书为 `/usr/local/nginx/sslkey/up-update.example.com.crt`
- 私钥为 `/usr/local/nginx/sslkey/up-update.example.com.key`

Nginx 配置示例：

```nginx
server {
    listen 80;
    server_name up-update.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name up-update.example.com;

    ssl_certificate /usr/local/nginx/sslkey/up-update.example.com.crt;
    ssl_certificate_key /usr/local/nginx/sslkey/up-update.example.com.key;

    location / {
        proxy_pass http://127.0.0.1:10025;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
    }
}
```

检查并重载 Nginx：

```bash
nginx -t
nginx -s reload
```

只对公网开放 `80` 和 `443`，不要公开 SQLite 数据目录或容器内部端口。

## 更新部署

使用部署脚本：

```bash
cd /usr/local/up-update
git pull --ff-only
./deploy.sh
```

使用 Docker Compose：

```bash
cd /usr/local/up-update
git pull --ff-only
docker compose up -d --build
```

更新会复用原来的 `.env` 和数据卷。不要执行 `docker volume rm`，除非已经确认不再需要其中的数据。

## 数据备份

部署脚本默认使用 `up-update-data` 数据卷。为保证 SQLite 备份一致性，先停止容器：

```bash
cd /usr/local/up-update
docker stop up-update
docker run --rm \
  -v up-update-data:/data \
  -v "$PWD":/backup \
  alpine \
  tar czf /backup/up-update-data.tar.gz -C /data .
docker start up-update
```

Docker Compose 默认数据卷通常名为 `up-update_up-update-data`：

```bash
docker compose stop
docker run --rm \
  -v up-update_up-update-data:/data \
  -v "$PWD":/backup \
  alpine \
  tar czf /backup/up-update-data.tar.gz -C /data .
docker compose start
```

如果项目目录名不同，可用 `docker volume ls` 确认实际卷名。备份文件和 `.env` 应分别保存在安全位置。

## 恢复备份

推荐恢复到新数据卷，确认成功后再决定是否删除旧卷：

```bash
cd /usr/local/up-update
docker volume create up-update-data-restored
docker run --rm \
  -v up-update-data-restored:/data \
  -v "$PWD":/backup \
  alpine \
  tar xzf /backup/up-update-data.tar.gz -C /data
UP_UPDATE_VOLUME=up-update-data-restored ./deploy.sh
```

恢复时必须使用生成备份时的 `UP_UPDATE_ENCRYPTION_KEY`。启动后登录系统并验证 B 站、Bark 和订阅数据，再处理旧数据卷。

## 常见部署问题

### Go 依赖下载超时

`proxy.golang.org` 在部分网络中不可达。部署脚本默认使用 `goproxy.cn`；手工构建时可执行：

```bash
docker build --build-arg GOPROXY=https://goproxy.cn,direct -t up-update:local .
```

### 页面无法登录

- 检查容器日志和 `/healthz`。
- HTTPS 部署确认 `UP_UPDATE_SECURE_COOKIES=true`。
- 确认 Nginx 转发了 `Host` 和 `X-Forwarded-Proto`。
- 初次启动确认管理员密码至少 10 个字符。

### 更新后数据为空

确认新容器挂载了原来的数据卷。`deploy.sh` 和 Docker Compose 的默认卷名不同，不要混用。
