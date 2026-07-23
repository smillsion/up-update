<p align="center">
  <img src="web/public/app-icon.png" width="144" alt="up-update Logo">
</p>

<h1 align="center">up-update</h1>

<p align="center">只关注你选择的 UP 主，新投稿通过 Bark 直接推送到 iPhone。</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
  <img src="https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white" alt="Vue 3">
  <img src="https://img.shields.io/badge/Docker-20.10%2B-2496ED?logo=docker&logoColor=white" alt="Docker 20.10+">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green.svg" alt="MIT License"></a>
</p>

<p align="center">
  <a href="#主要特性">主要特性</a> ·
  <a href="#快速开始">快速开始</a> ·
  <a href="docs/DEPLOYMENT.md">部署指南</a> ·
  <a href="docs/USAGE.md">使用指南</a> ·
  <a href="docs/CONFIGURATION.md">配置说明</a>
</p>

up-update 最重要的作用，是让你自己决定哪些 UP 主更新时需要提醒。无需开启 B 站 App 的消息通知，也能通过 Bark 收到选定 UP 主的新投稿，避免其他不感兴趣的内容频繁打扰。

项目适合个人、家人和朋友共同使用的小规模自托管场景。每位用户独立登录系统，配置自己的 UP 主订阅和 Bark 推送；B 站登录不是订阅前提，但可以导入关注列表并提升投稿轮询稳定性。Cookie 和推送密钥始终加密保存，不会展示给其他用户。

## 主要特性

| 能力 | 说明 |
| --- | --- |
| 精准订阅 | 只推送用户主动选择的 UP 主新投稿，无需开启 B 站 App 的消息通知 |
| 多用户隔离 | 管理员创建、停用或永久删除普通用户，每位用户独立维护 B 站登录、订阅和 Bark 配置 |
| 可选 B 站登录 | UID 和空间链接可直接订阅；扫码登录后可导入关注列表，并提升投稿轮询稳定性 |
| 灵活订阅 | 后台优先使用该 UP 订阅者的有效 Cookie，缺少凭证时匿名兜底，并显示最新投稿时间 |
| 原生推送 | 支持官方或自建 Bark Server、通知级别、提示音和测试推送，点击通知直达视频 |
| 分时轮询 | 按北京时间配置睡眠、工作和空闲时段，分别控制检查频率 |
| 延迟补发 | 睡眠时段或用户午休时段暂停自动通知，时段结束后按队列逐条补发 |
| 可靠投递 | 轮询失败退避、推送重试、分页投递记录和待发送通知取消 |
| 本地存储 | SQLite 单实例存储，Cookie、刷新令牌和 Bark Device Key 使用 AES-256-GCM 加密 |

## 工作流程

| 1. 登录 | 2. 订阅 | 3. 监控 | 4. 推送 |
| --- | --- | --- | --- |
| 用户登录系统并配置 Bark | 直接添加 UID、空间链接，或可选登录 B 站后从关注列表导入 | 后台优先使用有效登录凭证检查，缺少凭证时匿名兜底 | 发现新视频后通过 Bark 推送到 iPhone |

新增订阅只会将当前最新投稿作为基线，不会补推订阅前的历史视频。

## 快速开始

需要 Docker Engine 20.10+。克隆项目并准备环境变量：

```bash
git clone https://gitee.com/birdKiss/up-update.git
cd up-update
cp .env.example .env
openssl rand -hex 32
```

编辑 `.env`，设置至少 10 个字符的管理员密码，并将 `openssl` 输出写入 `UP_UPDATE_ENCRYPTION_KEY`，然后启动：

```bash
docker compose up -d --build
```

打开 `http://服务器地址:8080` 即可看到项目主页，点击“登录系统”后使用 `.env` 中的管理员账号登录。没有 Docker Compose、需要配置 HTTPS 或准备生产环境时，请阅读[部署指南](docs/DEPLOYMENT.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [部署指南](docs/DEPLOYMENT.md) | Docker Compose、部署脚本、升级、Nginx、日志、备份与恢复 |
| [使用指南](docs/USAGE.md) | 用户管理、B 站登录、Bark、订阅、通知记录与常见问题 |
| [配置说明](docs/CONFIGURATION.md) | 环境变量、加密密钥、分时轮询和个人通知设置 |
| [参与开发](CONTRIBUTING.md) | 本地测试、提交要求与敏感信息规范 |
| [更新记录](CHANGELOG.md) | 尚未发布和历史版本的功能变更 |

## 技术栈

- Go + chi：API、后台任务与静态资源服务
- Vue 3 + TypeScript + Vite：响应式 Web 管理界面
- SQLite：单实例本地数据存储
- Docker：多阶段构建和容器化部署
- Bark：iOS 系统通知推送

## 安全与限制

- 公网部署必须使用 HTTPS，并设置 `UP_UPDATE_SECURE_COOKIES=true`。
- `.env`、数据库、B 站 Cookie、刷新令牌和 Bark Device Key 都不得提交到 Git 或粘贴到 Issue、日志和聊天中。
- 恢复数据时必须使用原来的 `UP_UPDATE_ENCRYPTION_KEY`，否则敏感配置无法解密。
- 当前版本只支持单实例运行，不要让多个容器同时读写同一个 SQLite 数据卷。
- 项目使用哔哩哔哩非公开 Web 接口，只适合低频个人订阅。接口和风控策略可能变化，请控制使用规模并遵守相关服务条款。

## 参与开发

本地开发需要 Go 1.26+ 和 Node.js 24+。提交前请运行后端测试、前端测试和构建，具体要求见[参与开发](CONTRIBUTING.md)。

项目可同时维护 GitHub 与 Gitee 远程仓库。版本标签使用 `vX.Y.Z`；标签推送到 GitHub 后，现有工作流会测试项目并构建 `linux/amd64`、`linux/arm64` 镜像。

## License

[MIT](LICENSE)
