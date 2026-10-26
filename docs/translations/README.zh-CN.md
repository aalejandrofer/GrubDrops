<p align="center">
  <img src="../../assets/img/logo.png" width="160" alt="GrubDrops">
</p>

<p align="center"><sub><a href="../../README.md">English</a> · <strong>简体中文</strong> · <a href="README.es.md">Español</a></sub></p>

<h3 align="center">自托管、配置后即可放手不管的 Twitch 与 Kick 掉宝挖矿工具。</h3>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-SvelteKit%20SPA-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
  <a href="https://github.com/aalejandrofer/GrubDrops/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/aalejandrofer/GrubDrops?logo=github&label=release"></a>
  <a href="https://github.com/aalejandrofer/GrubDrops/pkgs/container/grubdrops"><img alt="ghcr.io image" src="https://img.shields.io/badge/ghcr.io-grubdrops-2496ED?logo=github"></a>
  <img alt="License" src="https://img.shields.io/badge/license-MIT-green">
</p>

<p align="center">
  <img src="../screenshots/home.png" width="900" alt="GrubDrops Home page: what each account is mining across Twitch and Kick, with the up-next queue and recent claims">
</p>

---

GrubDrops 会替你观看合适的 Twitch 与 Kick 直播，累积观看时长，并领取游戏内的
掉宝，同时支持多个账户。它是一个运行在你自己机器上的小型 Web 应用：以 Docker
镜像形式发布，所有数据都保存在单个 SQLite 文件中。

## 功能特性

- 🟣🟢 **Twitch 与 Kick 并行**：多个账户，一个面板。
- 🎯 **设置后无需管理**：选好游戏，它会发现、观看、累积时长并替你领取。
- 🔒 **自托管且私密**：一个容器，你的账户，你的数据。
- ⚡ **两个平台都可靠**：登录、关联、Kick 播放这些麻烦事由它处理。
- 🛠️ **可无人值守运行**：Discord 提醒、更新检查、多语言、代理和 SSO。

## 快速上手

### 前置条件

**Docker + Docker Compose**（快速路径）或 **Go 1.26+**（从源码构建）。
你需要什么取决于你要挖哪个平台：

| | Twitch | Kick |
|---|---|---|
| **登录** | 设备码（`twitch.tv/activate`） | `cookies.txt` 导出 |
| **观看方式** | 直连 HTTP — 无需浏览器 | WebSocket；Chrome **边车容器**回退（IVS 播放） |
| **Docker** | 可选 | **强烈推荐** — 没有它 WebSocket 也能工作，但 Chrome IVS 回退需要 docker socket |
| **从源码运行，无需 Docker** | ✅ 普通的 `go build` 二进制即可 | ⚠️ WebSocket 可用；Chrome 回退需要 Docker |
| **CPU 架构** | 任意 — `amd64` + `arm64` | `amd64` + `arm64`（arm64 资源占用高 —— 见说明） |

Twitch 走直连 HTTP，所以一个普通的 Go 二进制文件在任何地方都能挖它，无需 Docker。Kick 默认通过 WebSocket 无浏览器挖矿；可靠的 Chrome IVS 回退以边车容器形式通过 docker socket 运行，因此 **Kick 强烈推荐使用 Docker**。

> **树莓派 / ARM：** 两个镜像都发布了 `arm64` 版本；边车容器在 arm64 上使用 Debian Chromium（保留解码 Kick IVS 流所需的 H.264/AAC 编解码器）。可用，但很重 —— 每个边车约 4 GB 内存，所以低内存的树莓派只能同时跑几个 Kick 账户。
>
> **Kick 观看路径：** 默认先尝试无浏览器的 WebSocket 路径（无需 Docker），仅在 WebSocket 停止累积时切换到 Chrome 边车。请保留 docker socket 挂载，回退才能生效。

### 支持的平台

| 主机 | Twitch | Kick |
|---|---|---|
| Linux `x86-64` | ✅ | ✅ |
| Linux `arm64` / 树莓派 | ✅ | ✅ —— Chromium 边车容器，每个约 4 GB 内存 |
| macOS / Windows · Docker Desktop（Intel） | ✅ | ✅ |
| macOS / Windows · Apple Silicon | ✅ | ✅ —— arm64 Chromium 边车容器 |
| 从源码 `go build`（任意操作系统） | ✅ | ✅ WS；Chrome 回退需要 Docker |

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
      GRUB_MASTER_KEY: "${GRUB_MASTER_KEY:?generate one with docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen}"
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

`GRUB_MASTER_KEY` 必须是一个 **age X25519 身份密钥**（`AGE-SECRET-KEY-1…`），它用于加密存储的
会话令牌。随机字符串无法被解析，矿工会在启动时崩溃。只用 Docker 就能生成一个有效的密钥：

```bash
docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen
# → AGE-SECRET-KEY-1...（请保存好；每次重启都要复用同一个密钥）
```

把它启动起来。`GRUB_MASTER_KEY` 用于加密存储的会话，所以请生成一个真正的密钥：

```bash
GRUB_MASTER_KEY="$(docker run --rm ghcr.io/aalejandrofer/grubdrops:latest keygen)" docker compose up -d
```

打开 **http://localhost:8080**。首次访问会要求你创建一个管理员登录账户。

- **用 Portainer 或图形界面部署？** 那里没有 shell 来设置变量，所以请在部署之前，把
  `GRUB_MASTER_KEY`（上面 `keygen` 输出的值）填到 stack 的 **Environment variables**
  一栏。请使用普通的 `docker compose`，不要用 Swarm stack，并保持 Docker Engine 为较新版本。

**只用 Twitch？** 去掉 docker-socket 挂载，并让 `GRUB_BROWSER_URL`
保持未设置 —— 这样就永远不会创建 Kick 边车容器（没有边车容器，Kick 根本没有累积观看
时长的途径）。

**想要每一个调节项？** 完整的参考 compose 文件（边车 profile、OIDC、每一项
设置都带注释）位于
[`deploy/docker-compose.yml`](../../deploy/docker-compose.yml)。

**想自己构建？** 运行 `docker build -f deploy/Dockerfile.miner .`，或用普通的
`go build ./cmd/miner` 得到一个本地二进制文件。

## 添加账户

进入 **设置 → 账户**，为每个平台各添加一个账户。

**Twitch。** 点击添加，然后打开显示的验证网址并输入代码。
这就是官方的设备码流程；你的密码和 cookies 永远不会接触到
GrubDrops。

**Kick。** Kick 没有公开的登录 API，所以你需要把你现有的
kick.com 会话以从浏览器导出的 `cookies.txt` 文件形式交给 GrubDrops：

1. 安装一个 cookie 导出扩展：
   Chrome/Edge/Brave 用 [Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc)，
   或者
   Firefox 用 [cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/)。
2. 在 `kick.com` 登录，点击扩展图标，**Export** 当前站点。
3. 在 GrubDrops 中，打开 **设置 → 账户**，添加一个 Kick 账户，并上传（或粘贴）导出的 Netscape 格式 `cookies.txt`。

频道会根据每个活动的游戏自动发现，所以无需再做其它
配置。当会话失效时（发现流程会记录 Cloudflare 或 401
错误），重新导出并再次粘贴即可。

## 选择要挖矿的内容

GrubDrops 只挖你选择的游戏。全新安装在你至少添加一个游戏之前不会挖任何东西，
在那之前每个账户都显示“尚无游戏”状态，这不是错误。

所选游戏构成一个全局优先级列表，所有账户共用。在 **游戏** 页面，输入游戏名称即可添加；之前添加过的匹配游戏会显示为建议。添加游戏不需要有正在进行的活动。
拖动列表即可改变挖矿顺序。每次更改都会在后台重新加载 runner。

## 工作原理

- **Twitch：** 设备码登录，然后通过 GraphQL 与 PubSub 来跟踪进度并
  实时触发领取。
- **Kick：** 检测与领取依托于一个使用 Chrome-TLS 指纹的 HTTP 客户端
  （`utls`），所以无需应对 Cloudflare，也无需照看任何浏览器。但观看
  时长本身需要一个真实的播放器，因此它运行在一个按需的、按账户分配的
  Chrome 边车容器中来播放 IVS 流。挖矿器会通过 docker socket 自动创建、启动并
  停止该容器（按需拉取边车镜像），所以 Chrome 仅在观看时运行，而你
  无需定义任何边车服务。一个周期性的清扫任务会移除已删除账户的容器。
- **Runner：** 每个账户一个 runner。它扫描账户的活动与库存，向规划器索取队列，
  观看排在最前的掉宝并在完成后领取。观看期间每 15 分钟重建一次计划。
  没有直播时 12 小时后再扫描；所选游戏或账户变更时立即扫描。

## 优先级逻辑

规划器按如下方式为每个账户的掉宝排序：

```
1. Game rank in your chosen-games list, top first
2. Restricted (team) campaigns ahead of open ones
3. Closest to a claim (fewest watch minutes remaining)
4. Within a campaign, the shortest drop first
```

无法推进的掉宝会进入阻塞列表并注明原因：账户未关联、需要先领取前一个掉宝，
或 runner 已放弃。没有任何直播的活动会被跳过，转而处理队列中的下一个。
你可以在当前会话内重新排列某个账户的待挖列表。

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

界面是一个 SvelteKit 静态 SPA（源码在 `web/`），嵌入在 Go 二进制文件中。

| 页面 | 上面有什么 |
|------|------|
| **主页** | 每个账户正在挖什么、待挖队列、最近事件。 |
| **游戏** | 你所选的游戏，按名称添加，按你拖动的优先级排序。 |
| **奖励** | 所有账户已领取的掉宝，可筛选。 |
| **设置** | 常规、账户、通知、更新和健康状态。 |

登录和首次设置是独立的页面。Go 发送的字符串（通知）使用 `i18n.T`；界面通过 `t()`
读取 `internal/i18n/locales` 中的同一批语言文件。

## 架构

```
cmd/miner               main daemon
cmd/browser-sidecar     chromedp sidecar binary (Kick IVS playback)
internal/platform/...   per-platform backends (twitch, kick)
internal/plan           pure planner: picks what each account mines next
internal/runner         per-account runner (watch, mine, claim) and the pool
internal/collect        runner hooks into the store (claims, minutes, scans)
internal/dockerctl      on-demand sidecar start/stop over the docker socket
internal/api            JSON API + SSE for the SPA
internal/web            embedded SPA build (dist/)
internal/store          SQLite (sqlc + goose), age-encrypted sessions
web/                    SvelteKit SPA source
```

## 开发

```bash
npm --prefix web ci && npm --prefix web run build   # SPA into internal/web/dist
web/scripts/dev-miner.sh                            # local miner on a temp DB
GRUB_DEV_DIR=<dir> web/scripts/dev-seed.sh          # admin, sample games and accounts
GRUB_DEV_API=http://127.0.0.1:18080 npm --prefix web run dev
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
