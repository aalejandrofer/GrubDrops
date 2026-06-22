<p align="center">
  <img src="../../internal/web/static/img/logo.png" width="160" alt="GrubDrops">
</p>

<p align="center"><sub><a href="../../README.md">English</a> · <strong>简体中文</strong> · <a href="README.es.md">Español</a></sub></p>

<h3 align="center">自托管、配置后即可放手不管的 Twitch 与 Kick 掉宝挖矿工具。</h3>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-HTMX%20%2B%20Go%20templates-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
  <a href="https://github.com/aalejandrofer/GrubDrops/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/aalejandrofer/GrubDrops?logo=github&label=release"></a>
  <a href="https://github.com/aalejandrofer/GrubDrops/pkgs/container/grubdrops"><img alt="ghcr.io image" src="https://img.shields.io/badge/ghcr.io-grubdrops-2496ED?logo=github"></a>
  <img alt="License" src="https://img.shields.io/badge/license-MIT-green">
</p>

<p align="center">
  <img src="../screenshots/console.png" width="900" alt="GrubDrops console: watch-time stats, per-account mining across Twitch and Kick, and a live event feed">
</p>

---

GrubDrops 会替你观看合适的 Twitch 与 Kick 直播，累积观看时长，并领取游戏内的
掉宝，同时支持多个账户。它是一个运行在你自己机器上的小型 Web 应用：以 Docker
镜像形式发布，所有数据都保存在单个 SQLite 文件中。

## 功能特性

- 🎯 **由你设定白名单**（全局或按账户）。白名单之外的内容一律不挖矿。
- 🟣🟢 **Twitch 与 Kick 一并支持**，每个平台多个账户，全都在同一个页面上。
- ✅ **它会检查游戏**，让你永远不会把观看时长浪费在错误的直播上。
- 🔗 **它了解账户关联**（Krafton、Embark 等），并提供按账户的"我已关联"手动覆盖选项。
- 🖥️ **实时控制台**：累计统计、当前挖矿、掉宝目录、领取历史。
- 🔔 **Discord 通知**，可按事件类型逐项开关。
- 🧪 **实验性的无浏览器 Kick**（设置 → Experimental）：仅用 WebSocket 的观看模式，彻底去掉 Chrome 边车 —— 无需为 Kick 装 Docker，任何树莓派都能跑。需手动开启；可能会停止累积。
- 🔒 **你的凭据始终属于你**：Twitch 使用官方的设备码登录，Kick 使用你导出的会话。不会向 GrubDrops 发送任何密码。

## 快速上手

### 前置条件

**Docker + Docker Compose**（快速路径）或 **Go 1.26+**（从源码构建）。
你需要什么取决于你要挖哪个平台：

| | Twitch | Kick |
|---|---|---|
| **登录** | 设备码（`twitch.tv/activate`） | `cookies.txt` 导出 |
| **观看方式** | 直连 HTTP — 无需浏览器 | Chrome **边车容器**（真实 IVS 播放） |
| **Docker** | 可选 | **必需** — 挖矿器通过 docker socket 启动边车容器 |
| **从源码运行，无需 Docker** | ✅ 普通的 `go build` 二进制即可 | ❌ 边车容器需要 Docker |
| **CPU 架构** | 任意 — `amd64` + `arm64` | `amd64` + `arm64`（arm64 资源占用高 —— 见说明） |

Twitch 走直连 HTTP，所以一个普通的 Go 二进制文件在任何地方都能挖它 —— 包括树莓派（Raspberry Pi），无需 Docker。**Kick 的观看时长需要一个真实的播放器**，因此挖矿器会通过 docker socket 运行一个 Chrome/Chromium 边车容器 —— 这就使得 **Kick 必须依赖 Docker**。

> **树莓派 / ARM：** 两个镜像都发布了 `arm64` 版本；边车容器在 arm64 上使用 Debian Chromium（保留解码 Kick IVS 流所需的 H.264/AAC 编解码器）。可用，但很重 —— 每个边车约 4 GB 内存，所以低内存的树莓派只能同时跑几个 Kick 账户。
>
> **无浏览器 Kick（实验性）：** 设置 → Experimental → *WebSocket only* 以无浏览器方式观看 Kick —— 无需 Chrome、无需为 Kick 装 Docker，任何树莓派都能跑。实验性：可能会停止累积。

### 支持的平台

| 主机 | Twitch | Kick |
|---|---|---|
| Linux `x86-64` | ✅ | ✅ |
| Linux `arm64` / 树莓派 | ✅ | ✅ —— Chromium 边车容器，每个约 4 GB 内存 |
| macOS / Windows · Docker Desktop（Intel） | ✅ | ✅ |
| macOS / Windows · Apple Silicon | ✅ | ✅ —— arm64 Chromium 边车容器 |
| 从源码 `go build`（任意操作系统） | ✅ | 边车容器需要 Docker |

### 运行它

使用已发布镜像的 Docker Compose 是最快的路径 —— 只需运行
**miner**。对于 Kick 观看时长，它会按需为每个账户自动创建一个启用了编解码器的
Chrome **边车容器**（通过挂载的 docker socket），所以你
不必自己定义任何边车服务。（只用 Twitch？见下文。）

```yaml
# compose.yml
services:
  miner:
    image: ghcr.io/aalejandrofer/grubdrops:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      GRUB_MASTER_KEY: ${GRUB_MASTER_KEY:?run: head -c32 /dev/urandom | base64}
      GRUB_DB_PATH: /data/miner.db
      GRUB_SECURE_COOKIES: "0"   # plain-HTTP localhost; set 1 behind HTTPS
    volumes:
      # The container runs as nonroot (UID 65532); make ./data writable by it
      # first (see below) or use a named volume instead of a bind mount.
      - ./data:/data
      # lets the miner create/start/stop per-account browser sidecars on demand
      - /var/run/docker.sock:/var/run/docker.sock
```

**先让数据目录可写。** 挖矿器镜像以 distroless 的
`nonroot` 用户（**UID 65532**）运行。一个全新的、以绑定挂载方式挂载的 `./data` 由你的
主机用户所有，因此容器无法写入 `miner.db` —— 会话永远无法持久化，登录
会在验证成功后立即以 *"failed to persist session"* 报错失败。
在启动之前先把该目录交给容器用户：

```bash
mkdir -p data && sudo chown 65532:65532 data
```

（或者干脆不用绑定挂载，改用具名的 Docker 卷 —— Docker 创建的卷
默认就对容器可写。）

把它启动起来。`GRUB_MASTER_KEY` 用于加密存储的会话，所以请生成一个真正的密钥：

```bash
GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) docker compose up -d
```

打开 **http://localhost:8080**。首次访问会要求你创建一个管理员登录账户。

**只用 Twitch？** 去掉 docker-socket 挂载，并让 `GRUB_BROWSER_URL`
保持未设置 —— 这样就永远不会创建 Kick 边车容器（没有边车容器，Kick 根本没有累积观看
时长的途径）。

**想要每一个调节项？** 完整的参考 compose 文件（边车 profile、OIDC、每一项
设置都带注释）位于
[`deploy/docker-compose.yml`](../../deploy/docker-compose.yml)。

**想自己构建？** 运行 `docker build -f deploy/Dockerfile.miner .`，或用普通的
`go build ./cmd/miner` 得到一个本地二进制文件。

## 添加账户

进入 **Accounts**，为每个平台各添加一个账户。

**Twitch。** 点击添加，然后在 `twitch.tv/activate` 处批准显示的代码。
这就是官方的设备码流程；你的密码和 cookies 永远不会接触到
GrubDrops。

**Kick。** Kick 没有公开的登录 API，所以你需要把你现有的
kick.com 会话以从浏览器导出的 `cookies.txt` 文件形式交给 GrubDrops：

1. 安装一个 cookie 导出扩展：
   Chrome/Edge/Brave 用 [Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc)，
   或者
   Firefox 用 [cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/)。
2. 在 `kick.com` 登录，点击扩展图标，**Export** 当前站点。
3. 在 GrubDrops 中，打开该账户的 **Authorize** 页面并上传（或粘贴）导出的内容。

频道会根据每个活动的游戏自动发现，所以无需再做其它
配置。当会话失效时（发现流程会记录 Cloudflare 或 401
错误），重新导出并再次粘贴即可。

## 工作原理

- **Twitch：** 设备码登录，然后通过 GraphQL 与 PubSub 来跟踪进度并
  实时触发领取。
- **Kick：** 检测与领取依托于一个使用 Chrome-TLS 指纹的 HTTP 客户端
  （`utls`），所以无需应对 Cloudflare，也无需照看任何浏览器。但观看
  时长本身需要一个真实的播放器，因此它运行在一个按需的、按账户分配的
  Chrome 边车容器中来播放 IVS 流。挖矿器会通过 docker socket 自动创建、启动并
  停止该容器（按需拉取边车镜像），所以 Chrome 仅在观看时运行，而你
  无需定义任何边车服务。一个周期性的清扫任务会移除已删除账户的容器。
- **发现（Discovery）** 每隔几分钟把两个平台的目录都扫入 SQLite，使
  仪表盘始终反映当前正在直播的内容。

## 优先级逻辑

每个账户一次只挖一个活动。当有多个白名单内的活动
符合条件时，GrubDrops 会按以下顺序挑选：

```
1. Campaign, by your priority mode (Settings):
   ├─ ordered (default)  → your whitelist rank, top of the list first
   ├─ ending_soonest     → soonest deadline first
   └─ low_avbl_first     → fewest available channels first
2. Tiebreak: closest to a claim (fewest watch-minutes remaining)
3. Restricted (team) campaigns ahead of open ones (both platforms)
4. Channel: a live stream confirmed on the campaign's game,
   highest viewer count first (Twitch also probes for one
   actually serving the target drop)
```

白名单与优先级是按账户设置的，并在缺省时回退到全局列表。一个
没有任何直播的活动会被跳过，而不是空等。

## 配置

所有设置都是环境变量。`GRUB_MASTER_KEY` 是唯一**必需**的
变量。下面的其它每个变量都是**可选**的：保持未设置即可采用所示的
默认值。

| 变量 | 默认值 | 用途 |
|-----|---------|---------|
| `GRUB_MASTER_KEY` | **必需** | 用于 age 加密会话存储的密钥。 |
| `GRUB_HTTP_ADDR` | `:8080` | 监听地址。 |
| `GRUB_DB_PATH` | `/data/miner.db` | SQLite 路径（在 Docker 之外可用例如 `./miner.db`）。 |
| `GRUB_KICK_SIDECAR_IMAGE` | `ghcr.io/aalejandrofer/grubdrops-browser:latest` | 挖矿器为每个自动创建的边车容器拉取并运行的镜像。Kick 观看始终通过边车容器进行（唯一的累积途径）；未配置 = Kick 无法观看。 |
| `GRUB_KICK_SIDECAR_NETWORK` | 自动检测 | 要把边车容器接入的 Docker 网络。默认为挖矿器自身所在的网络（自动检测）；设置以覆盖。 |
| `GRUB_KICK_SIDECAR_TEMPLATE` | `grubdrops-browser-{slug}` | 按账户的边车容器名称模板。 |
| `GRUB_KICK_SIDECAR_PORT` | `9090` | 边车容器的 gRPC 端口。 |
| `GRUB_BROWSER_URL` | 无 | 固定的边车地址（旧版常驻模式）。 |
| `GRUB_BROWSER_URLS` | 无 | 逗号分隔的常驻边车容器池（每个 Kick 账户一个 Chrome）。 |
| `GRUB_DISCOVERY_INTERVAL` | `60m` | 目录扫描节奏（例如 `30m`、`2h`）；也可在 Settings 中编辑。 |
| `GRUB_AUTHCHECK_INTERVAL` | `12h` | 认证健康检查的扫描节奏。 |
| `GRUB_DISCORD_WEBHOOK` | 无 | 可选的全局 Discord webhook。 |
| `GRUB_SECURE_COOKIES` | `0` | 安全会话 cookie + CSRF 同源方案。通过纯 HTTP（`http://pi:8080`）访问时保持 `0`；仅当通过 HTTPS 访问时（直接访问，或位于设置 `X-Forwarded-Proto: https` 的 TLS 终结代理之后）才设为 `1`。见下方说明。 |
| `GRUB_LOG_LEVEL` | `info` | `debug`、`info`、`warn`、`error`。 |
| `GRUB_AUTHBYPASS` | `false` | 为真值（`1`/`true`）时**禁用所有鉴权**。 |
| `GRUB_TWITCH_BROWSER` | `0` | 设为 `1` 时通过浏览器 sidecar 路由 Twitch，而非直接 HTTP。实验性；建议使用默认的直接 HTTP 路径。 |
| `GRUB_CANARY_INTERVAL` | 健康检查页的值 | 覆盖积累探针的运行周期（如 `6h`）；未设置时回退到 设置 ▸ 健康检查 的值。 |

> **自托管 / "invalid CSRF token"：** `GRUB_SECURE_COOKIES` 必须与你
> 访问该应用的方式相匹配。通过**纯 HTTP**（默认方式，例如位于
> `http://pi:8080` 的树莓派）访问时保持 `0` —— 设为 `1` 会让浏览器把会话/CSRF
> cookie 标记为 `Secure`，于是它在 HTTP 上会被悄悄丢弃，而每一次表单 POST 随后都会
> 在 CSRF 检查中失败。位于**终结 TLS 的反向代理**之后时，设为 `1`
> 并让代理转发 `X-Forwarded-Proto: https`。现在一次失败的检查会记录
> 一行 `csrf check failed`，并返回一个指向可能的
> 不匹配原因的提示。

### 单点登录（OIDC）

可选；密码登录仍作为回退保留。可与任何 OIDC 提供方配合使用
（authentik、Auth0、Keycloak、Google、Okta 等）。一旦设置了前
四个变量，SSO 即会开启：

| 变量 | 是否必需 | 用途 |
|-----|----------|---------|
| `GRUB_OIDC_ISSUER` | 是 | Issuer URL。 |
| `GRUB_OIDC_CLIENT_ID` | 是 | OAuth 客户端 ID。 |
| `GRUB_OIDC_CLIENT_SECRET` | 是 | OAuth 客户端密钥。 |
| `GRUB_OIDC_REDIRECT_URL` | 是 | `https://<host>/auth/oidc/callback`，需在 IdP 处注册。 |
| `GRUB_OIDC_PROVIDER_NAME` | 否 | 按钮标签（默认 `SSO`）。 |
| `GRUB_OIDC_ALLOWED_EMAILS` | 否 | 逗号分隔的邮箱允许名单。 |
| `GRUB_OIDC_ALLOWED_GROUPS` | 否 | `groups` claim 上要求的组。 |

> **注意：** 若未设置允许名单，任何被 IdP 认证通过的人都会成为
> 管理员。请在 IdP 中限定成员范围，或设置一个允许名单。

## 各个页面

| 页面 | 上面有什么 |
|------|------|
| **Console**（`/`） | 累计统计、按账户挖矿、实时事件流。 |
| **Drops**（`/drops`） | 过去 / 当前 / 即将到来的活动、物品、关联标签、一键加入白名单。 |
| **History**（`/history`） | 跨所有账户的领取日志。 |
| **Settings**（`/settings`） | 优先级列表、时间间隔、Discord、日志级别、密码。 |
| **Accounts** | 添加账户、按账户的白名单、重新认证、认证健康状态。 |

## 架构

```
cmd/miner               main daemon
internal/platform/...   per-platform backends (twitch, kick)
internal/watcher        per-account state machine (watch, mine, claim)
internal/dockerctl      on-demand sidecar start/stop over the docker socket
internal/discovery      catalog scraper
internal/api + web      HTMX UI and handlers
internal/store          SQLite (sqlc + goose), age-encrypted sessions
```

## 致谢

GrubDrops 站在那些率先攻克了最困难部分的项目的肩膀上：

- **[DevilXD/TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner)**：
  Twitch 的设备码流程、GraphQL 查询，以及观看时长机制。
- **[HyperBeats/KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner)**：
  最早理清了 Kick 掉宝的工作原理。

GrubDrops 是它自己的 Go 重写版本，带有 Web UI 和多账户支持，但如果
没有他们打下的基础，它根本不会存在。谢谢你们。

## 许可证

基于 [MIT License](../../LICENSE) 发布。

## 关于负责任使用的说明

自托管、单租户、持续开发中。`/healthz` 用于响应存活性
检查；在重新部署之间请保留 `/data`；如果你要对外暴露它，请把它放在反向代理之后。
请在各平台的服务条款（Terms of Service）范围内使用，针对你自己的
账户，风险自负。

---

<sub>由 <a href="https://github.com/aalejandrofer">@aalejandrofer</a> 使用 <a href="https://claude.com/claude-code">Claude Code</a> 构建。参见<a href="docs/CHANGELOG.md">更新日志</a>与<a href="docs/DESIGN.md">设计说明</a>。</sub>
