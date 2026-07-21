# up-update

监控哔哩哔哩 UP 主的新投稿，并通过 Bark 将原生通知推送到 iPhone。适合个人、家人或朋友共同使用的小规模自托管实例。

## 功能

- 独立用户账号、B 站扫码登录与 Cookie 自动续期、UP 主列表和 Bark 配置
- 有效登录 Cookie 是订阅前置条件；支持 UID、空间链接以及从 B 站关注列表批量导入，只推送订阅后的新视频
- 官方 Bark 与自建 Bark Server，支持通知级别、提示音、午休静默和使用未保存配置测试推送；点击通知直接打开本次更新的视频
- 睡眠、工作和空闲时间分级轮询，支持失败退避、Cookie 状态检查、投递重试与分页历史记录
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
2. 用户在“设置”中选择“扫码登录”，使用哔哩哔哩客户端扫码并在手机上确认。
3. 在 Bark App 中复制 Device Key，填写 Bark Server 并发送测试通知。
4. 使用 UID、`https://space.bilibili.com/{uid}`，或从已登录账号的关注列表中选择 UP 主添加订阅。关注列表导入会立即初始化最新投稿；个别请求失败时订阅仍会创建，并由后台自动补齐。

新增订阅时只记录当前最新投稿作为基线，不会推送旧视频。管理员可在“管理”页面调整北京时间表；默认睡眠时间 `00:00–08:00` 每 120 分钟检查，工作时间 `09:00–12:00`、`14:00–18:00` 每 15 分钟检查，其余空闲时间每 5 分钟检查。

Bark Device Key 已保存后，修改通知级别、提示音或午休时段时不需要再次填写。发送测试会使用页面上的当前值但不会保存；每个用户可独立开启午休静默，默认时段为 `12:00–14:00`。

## B 站登录与自动续期

推荐直接使用设置页中的“扫码登录”。扫码成功后，up-update 会加密保存 B 站返回的 Cookie 和刷新令牌；后台定期检查凭证状态，并在 B 站要求刷新时自动保存新凭证。正常情况下无需再从浏览器反复复制 Cookie。

自动续期不能绕过 B 站的账号安全策略。主动退出账号、修改密码、账号风控或服务器撤销会话后，仍需重新扫码。二维码登录和自动续期使用 B 站 Web 客户端的非公开接口，接口变化也可能导致功能暂时不可用。

### 手工 Cookie 备用方式

无法扫码时，可以展开设置页中的“手动填写 Cookie”并粘贴浏览器请求携带的完整 Cookie。手工 Cookie 没有配套刷新令牌，因此不会自动续期。

推荐使用电脑上的 Chrome 或 Edge 获取完整 Cookie。不要使用 `document.cookie`，它可能无法读取带有 `HttpOnly` 属性的登录 Cookie。

1. 打开 [哔哩哔哩网页版](https://www.bilibili.com/)并登录需要用于监控的账号。
2. 按 `F12` 打开开发者工具，进入“网络（Network）”面板。
3. 在过滤框输入 `x/web-interface/nav`，然后刷新页面。
4. 点击地址为 `https://api.bilibili.com/x/web-interface/nav` 的请求。
5. 打开“标头（Headers）”，在“请求标头（Request Headers）”中找到 `cookie`。
6. 复制 `cookie:` 后面的完整内容，在 up-update 设置页展开“手动填写 Cookie”，粘贴并点击“验证并保存”。

Cookie 通常类似：

```text
buvid3=...; b_nut=...; SESSDATA=...; bili_jct=...; DedeUserID=...
```

如果没有找到 `nav` 请求，可在 Network 中过滤 `api.bilibili.com`，选择任意发往该域名的请求，再从 Request Headers 中复制完整 Cookie。不要修改其中的 `%2C`、`%2F` 等编码，也不要包含开头的 `cookie:` 字样。

Cookie 和刷新令牌都相当于账号登录凭证。不要将它们发送到聊天、截图、Issue、日志或 Git 仓库，建议使用单独的 B 站账号。每位用户应登录自己的 B 站账号并独立保存凭证。

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
