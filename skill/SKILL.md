---
name: iai
description: iai (爱 AI) 一键部署。把当前项目打包、上传、构建并部署到公司内网子域名。当用户说 `deploy +push` / `deploy +status` / `deploy +logs` / `deploy +list` / `deploy +login` / `deploy +share` 时使用。Skill 会扫描 cwd 并通过 Control Plane API 完成部署，全程不需要用户写 Dockerfile / k8s manifest。项目名修改、HTTPS 开关、自定义域名、IP 访问控制都在 Web 后台做。
---

# iai Skill

把当前目录的项目部署到 iai 平台。命令一览：

| 命令 | 行为 |
|---|---|
| `deploy +login` | 提示如何拿到 Deploy Token；OIDC 登录走 Web UI（platform 配过 Keycloak 的话） |
| `deploy +whoami` | 显示当前身份 |
| `deploy +push` | **主命令**：扫描 → 打包 → 上传 → 构建 → 部署 → follow 日志 |
| `deploy +status` | 查项目状态和 URL |
| `deploy +logs [-f]` | 取构建/运行日志 |
| `deploy +list` | 列出我**自己**的项目（owner 或 collaborator） |
| `deploy +share list / add EMAIL / remove EMAIL` | 协作者管理（owner 或 admin 才能改） |

## 走 CLI 还是走 Web 后台

| 需求 | 入口 |
|---|---|
| 部署 / 重新部署 | `deploy +push`（CLI） |
| 查状态 / 日志 / 项目列表 | CLI（`+status` / `+logs` / `+list`） |
| 协作者增删 | CLI（`+share`）或 Web 后台「协作者」面板 |
| **改项目名** | **Web 后台 → 项目详情页「项目名称」面板**（slug 不变，链接还能用） |
| **开 / 关 HTTPS** | **Web 后台 → 项目详情页「HTTPS」面板**（cert-manager 自动签发 LE 证书，约 30 秒） |
| **加自定义域名 / 自定义子域** | **Web 后台 → 项目详情页「自定义域名」面板**（每个项目默认上限 1 个，含子域和买的域名） |
| **改 IP 访问控制 / 内网限制** | **Web 后台 → 项目详情页「访问控制」面板**（选预设或自定义 CIDR） |
| **改可见性 public / org / restricted** | Web 后台项目页 |

> Claude 收到 "怎么改项目名 / 怎么开 HTTPS / 怎么加自定义域名 / 怎么限 IP" 这类问题时，**不要尝试用 CLI 做**——这些没有对应命令，引导用户去 Web 后台对应面板即可。

## 首次推送：会被问到的 3 件事

第一次在一个项目目录跑 `deploy +push`，按顺序问到 3 件事。**每个 prompt 都会带上"去哪里拿"的提示**，新人按提示找就行：

### 1️⃣ API URL（仅首次，没填过时）

```
? Control-plane API URL
  示例: https://admin.iai.your-company.com  /  http://10.0.0.5:8080
  向 IT 同事或部署负责人要这个地址
? [http://localhost:8080]:
```

填完写进 `~/.vibedeploy/config.json` 的 `api_url`，下次不再问。

### 2️⃣ Deploy Token（找不到时）

```
? Paste your Deploy Token (vbd_live_...)
  在 Admin UI 的 /skill 页面生成（明文只显示一次）
  地址例: https://admin.iai.your-company.com/skill
? Token: ▌
```

Token 格式：`vbd_live_<43 char base64url>`。粘贴完自动写进 `~/.vibedeploy/credentials.json`（0600 权限），下次不再问。

### 3️⃣ 项目名称（仅首次创建项目时）

```
? 设置项目名称？(y/n，回车 = y，scanner 猜的是「my-app」) [Y/n]:
? Project name [my-app]:
? URL slug [my-app] (empty for auto-suffix):
```

非交互（CI / Claude tool 调用）会**跳过**这 3 个 prompt，直接用 scanner 猜的名字（一般是目录名）；同时**显式 warn** 一次告诉你：

```
⚠ 未设置项目名称 —— 使用默认值「my-app」
⚠ 稍后可在 Admin UI 修改: https://<admin>/projects/<slug> → 项目名称面板
```

后续随时改：Admin UI 项目页 → **项目名称** 面板。slug 不会变，老链接继续有效。

## 鉴权

Deploy Token 是上面 2️⃣，**全平台一个**（不是按项目隔的）。Skill 找 token 的顺序：

1. **`VIBEDEPLOY_TOKEN`** 环境变量（CI / Claude tool 调用走这条最干净）
2. **`~/.vibedeploy/credentials.json`** 里的 `token` 字段（首次粘贴后自动持久化）
3. **交互式 inline 粘贴**（见上面 2️⃣ 的 prompt）
4. 上面都拿不到 → 报 `no token. Set VIBEDEPLOY_TOKEN=vbd_... or run: deploy +login`

`deploy +login` 不接 OIDC（OIDC 走浏览器登录 Admin UI），只是把上面的 inline 粘贴搬到独立命令里，便于"先登录、稍后再 push"。

## 配置

`~/.vibedeploy/config.json`（首次跑 `install.sh` 时写入，也可手动改）：

```json
{
  "api_url": "https://admin.iai.your-company.com"
}
```

`api_url` 来源优先级（高 → 低）：`VIBEDEPLOY_API` 环境变量 > `config.json` > 交互 prompt > 默认 `http://localhost:8080`（仅本地开发）。

`VIBEDEPLOY_HOME` 可以覆盖配置目录默认值 `~/.vibedeploy`（一台机器装多套环境时用）。

## 实施细则

Skill 主体是 `scripts/` 目录里的几个 bash 脚本。每个命令都通过子命令分派。

**调用方式**：当用户在 Claude Code 里说 "deploy +push" / "部署一下" / "把当前目录推上去看看"，调用对应脚本。
脚本失败时退出码 != 0，并把错误打到 stderr。

```bash
# Push current dir
bash <skill-root>/scripts/push.sh

# Other commands
bash <skill-root>/scripts/status.sh [SLUG]
bash <skill-root>/scripts/logs.sh   [SLUG] [-f] [-n N]
bash <skill-root>/scripts/list.sh
bash <skill-root>/scripts/login.sh
bash <skill-root>/scripts/whoami.sh
```

**注意**：Skill 不要自己手动拼 curl 调 API —— 一切走脚本，便于 dev 阶段一致维护。
