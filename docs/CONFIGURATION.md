# 配置说明

[返回首页](../README.md) · [部署指南](DEPLOYMENT.md) · [使用指南](USAGE.md)

up-update 的配置分为启动环境变量、管理员全局设置和用户个人设置。

## 环境变量

复制 `.env.example` 创建 `.env`：

```bash
cp .env.example .env
chmod 600 .env
```

| 变量 | 默认值 | 必填 | 说明 |
| --- | --- | --- | --- |
| `UP_UPDATE_HTTP_PORT` | `8080` | 否 | Docker 发布到主机的端口，不改变容器内部的 `8080` |
| `UP_UPDATE_ADMIN_USERNAME` | `admin` | 否 | 空数据库首次启动时创建的管理员用户名 |
| `UP_UPDATE_ADMIN_PASSWORD` | 无 | 是 | 初始管理员密码，至少 10 个字符；Compose 和部署脚本会在每次部署时检查 |
| `UP_UPDATE_ENCRYPTION_KEY` | 无 | 是 | 敏感配置加密密钥，推荐使用 32 字节随机值的 64 位十六进制形式 |
| `UP_UPDATE_SECURE_COOKIES` | `false` | 公网 HTTPS 必填 | 设置为 `true` 后，登录会话 Cookie 只通过 HTTPS 发送 |
| `TZ` | `Asia/Shanghai` | 否 | 容器时区；业务分时策略固定按北京时间计算 |

示例：

```dotenv
UP_UPDATE_HTTP_PORT=10025
UP_UPDATE_ADMIN_USERNAME=admin
UP_UPDATE_ADMIN_PASSWORD=change-this-password
UP_UPDATE_ENCRYPTION_KEY=replace-with-output-from-openssl-rand-hex-32
UP_UPDATE_SECURE_COOKIES=true
TZ=Asia/Shanghai
```

`.env` 不得提交到 Git。修改环境变量后需要重建或重启容器；Web 页面保存的设置会立即写入数据库，不需要重启。

## 生成加密密钥

Linux：

```bash
openssl rand -hex 32
```

PowerShell：

```powershell
$keyBytes = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Fill($keyBytes)
[Convert]::ToHexString($keyBytes).ToLower()
```

应用也支持 32 字节 Base64，或至少 32 个字符的文本，但部署脚本要求使用恰好 64 位十六进制字符串，因此推荐统一使用 `openssl rand -hex 32`。

加密密钥用于保护：

- B 站 Cookie
- B 站刷新令牌
- Bark Device Key

密钥不会保存到数据库。备份数据时必须同时安全备份 `.env` 或密钥；更换密钥后，已有敏感配置将无法解密。

## 初始管理员

`UP_UPDATE_ADMIN_USERNAME` 和 `UP_UPDATE_ADMIN_PASSWORD` 只在数据库没有任何用户时使用。

- 初次启动密码少于 10 个字符时，应用拒绝启动。
- 为通过 Compose 和 `deploy.sh` 的部署检查，数据库初始化后仍应在 `.env` 中保留该变量。
- 数据库已有用户后，修改这两个变量不会修改或重置管理员。
- 忘记密码时，由其他管理员在页面重置普通用户密码；当前版本没有通过环境变量重置既有管理员密码的功能。

## 分时轮询

管理员进入“管理”页面配置全局时间表。业务时区固定为 `Asia/Shanghai`。

### 默认时间表

| 模式 | 时段 | 默认间隔 | 行为 |
| --- | --- | --- | --- |
| 睡眠 | `00:00–08:00` | 120 分钟 | 降低轮询频率，自动通知延迟到睡眠结束 |
| 工作 | `09:00–12:00`、`14:00–18:00` | 15 分钟 | 使用工作模式间隔 |
| 空闲 | 其他时间 | 5 分钟 | 使用空闲模式间隔 |

### 校验规则

- 三种模式的间隔均为 1–1440 分钟。
- 睡眠开始和结束时间不能相同，可以跨越午夜。
- 工作时段需要设置 1–4 组。
- 每组工作时段必须在当天开始并结束，开始时间早于结束时间。
- 工作时段之间不能重叠。
- 睡眠模式优先于工作模式；其余时间属于空闲模式。

保存时间表后，未处于失败退避状态的轮询任务会重新计算下次检查时间。正在失败退避的任务继续遵循退避计划。

## B 站配置

每位用户可在“设置”页面独立配置 B 站登录；不使用关注列表导入时可以不配置。

| 方式 | 自动续期 | 推荐场景 |
| --- | --- | --- |
| 扫码登录 | 支持 | 默认方式，使用哔哩哔哩客户端确认 |
| 手工 Cookie | 不支持 | 扫码不可用时临时使用 |

扫码登录保存 Cookie 和刷新令牌；手工保存 Cookie 时会删除该用户已有的刷新令牌。后台定期验证登录状态，凭证无效后只会影响关注列表导入，UID/空间链接订阅和公开投稿匿名轮询仍会继续。

## Bark 配置

每位用户独立配置 Bark：

| 配置 | 默认值 | 说明 |
| --- | --- | --- |
| Bark Server | `https://api.day.app` | 支持官方或自建 HTTP/HTTPS Bark Server |
| Device Key | 无 | Bark App 中显示的推送密钥，加密保存 |
| 通知级别 | `active` | 支持 `active`、`timeSensitive`、`passive`、`critical` |
| 提示音 | 默认提示音 | 支持常用选项或自定义 Bark 音效名称 |
| 午休延迟补发 | 关闭 | 开启后在指定时段暂停自动通知 |
| 午休时段 | `12:00–14:00` | 开始和结束不能相同，可以跨越午夜 |

Device Key 已经保存时可以留空，其他字段仍可单独修改。测试推送优先使用表单中尚未保存的值；Device Key 留空时复用已保存值。测试不会保存设置，也不受延迟补发影响。

### 延迟规则

自动通知可能同时受到两层延迟：

1. 管理员配置的全局睡眠时段。
2. 用户配置的个人午休时段。

两个时段重叠时，使用较晚的结束时间。配置发生变化后，等待中的通知会重新判断是否仍需延迟。

## 数据与安全

| 数据 | 存储方式 |
| --- | --- |
| 用户密码 | 单向密码哈希 |
| B 站 Cookie | AES-256-GCM 加密 |
| B 站刷新令牌 | AES-256-GCM 加密 |
| Bark Device Key | AES-256-GCM 加密 |
| 用户、订阅、视频和投递记录 | SQLite 数据卷 |
| 登录会话 | SQLite；HTTPS 部署时使用 Secure Cookie |

建议：

- 将 `.env` 权限设置为 `600`。
- 公网只开放 Nginx 的 `80/443`，应用端口绑定到 `127.0.0.1`。
- 定期备份数据卷和加密密钥，并分开保存。
- 不在日志、截图、聊天和 Issue 中暴露 Cookie、刷新令牌、Device Key 或 `.env`。
- 不要让多个 up-update 实例同时挂载同一个数据卷。

部署、更新和备份命令见[部署指南](DEPLOYMENT.md)。
