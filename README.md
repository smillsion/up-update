# up-update

监控哔哩哔哩 UP 主的新投稿，并通过 Bark 将原生通知推送到 iPhone。适合个人、家人或朋友共同使用的小规模自托管实例。

## 功能

- 独立用户账号、B 站 Cookie、UP 主列表和 Bark 配置
- 有效登录 Cookie 是订阅前置条件；支持 UID、空间链接以及从 B 站关注列表批量导入，只推送订阅后的新视频
- 官方 Bark 与自建 Bark Server，支持通知级别、提示音和测试推送；点击通知直接打开本次更新的视频
- 轮询限速、失败退避、Cookie 状态检查、投递重试与分页历史记录
- SQLite 本地存储，敏感配置 AES-256-GCM 加密
- 单容器部署，适配桌面与移动端并支持系统深色模式

## Docker 部署

推荐使用 Docker Engine 20.10+。有 Docker Compose v2 时可直接使用项目中的 Compose 配置：

```bash
cp .env.example .env
```

编辑 `.env`，设置初始管理员密码，并生成 32 字节随机加密密钥：

```bash
openssl rand -hex 32
docker compose up -d --build
```

打开 `http://服务器地址:8080`，使用 `.env` 中的管理员账号登录。管理员密码只在首次创建数据库时使用。

没有 Docker Compose 时，可以使用项目根目录的部署脚本。脚本会检查 `.env`、构建镜像、保留数据卷并替换容器；监听地址固定为 `.env` 中端口对应的 `127.0.0.1`，适合由同机 Nginx 反向代理：

```bash
chmod +x deploy.sh
./deploy.sh
```

脚本默认通过 `https://goproxy.cn,direct` 下载 Go 依赖。需要更换代理时可执行 `GOPROXY=https://代理地址 ./deploy.sh`。

当前服务器使用 `UP_UPDATE_HTTP_PORT=10025` 时，容器会监听 `127.0.0.1:10025`。以后更新可执行：

```bash
cd /usr/local/up-update
git pull
./deploy.sh
```

PowerShell 可用以下命令生成密钥：

```powershell
$keyBytes = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Fill($keyBytes)
[Convert]::ToHexString($keyBytes).ToLower()
```

## 使用

1. 管理员在“管理”页面创建用户，用户首次登录后修改临时密码。
2. 用户在“设置”中粘贴已登录哔哩哔哩浏览器请求携带的完整 Cookie。
3. 在 Bark App 中复制 Device Key，填写 Bark Server 并发送测试通知。
4. 使用 UID、`https://space.bilibili.com/{uid}`，或从已登录账号的关注列表中选择 UP 主添加订阅。

新增订阅时只记录当前最新投稿作为基线，不会推送旧视频。默认每 5 分钟轮询一次，可由管理员调整为 1–60 分钟。

## 反向代理

公网部署必须使用 HTTPS，并在 `.env` 中设置 `UP_UPDATE_SECURE_COOKIES=true`。示例 Nginx 配置：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

不要公开转发 SQLite 数据目录，不要提交 `.env`，也不要在 issue 或日志中粘贴 Cookie、Device Key 或加密密钥。

## 数据备份

当前版本只支持单实例。备份前停止容器，再导出命名卷：

```bash
docker compose stop
docker run --rm -v up-update_up-update-data:/data -v "$PWD":/backup alpine tar czf /backup/up-update-data.tar.gz -C /data .
docker compose start
```

恢复时必须同时使用原来的 `UP_UPDATE_ENCRYPTION_KEY`，否则已保存的 Cookie 和 Bark Key 无法解密。

## 本地开发

需要 Go 1.26+ 和 Node.js 24+。

```bash
cd web
npm ci
npm run test
npm run build
cd ..
cp -r web/dist/* internal/web/dist/
go test ./...
go run ./cmd/server
```

启动前设置 `UP_UPDATE_ENCRYPTION_KEY` 和 `UP_UPDATE_ADMIN_PASSWORD`。前端开发服务器可使用 `npm run dev`，默认代理到 `localhost:8080`。

## 数据源说明

哔哩哔哩官方开放平台不能用于监控未授权关联的任意 UP 主。本项目使用非公开 Web 接口，只用于低频个人订阅；接口、签名或风控策略可能随时变化，Cookie 也可能失效。请控制使用规模并遵守哔哩哔哩服务条款。

## GitHub 与 Gitee

仓库不绑定具体托管平台。创建两个空远程仓库后可同时维护：

```bash
git remote add github git@github.com:YOUR_NAME/up-update.git
git remote add gitee git@gitee.com:YOUR_NAME/up-update.git
git push github main
git push gitee main
```

版本使用 `vX.Y.Z` 标签。标签推送到 GitHub 后，工作流会测试项目并将多架构镜像发布到对应的 GHCR 仓库；Gitee 保持完整源码、文档和标签镜像。

## License

[MIT](LICENSE)
