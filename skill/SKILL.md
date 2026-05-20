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

## 鉴权

两条路径：
- **环境变量** `VIBEDEPLOY_TOKEN=vbd_live_...`（CI 用、Skill 用）
- **`~/.vibedeploy/credentials.json`**（M2 起接 Keycloak device flow）

Skill 端读取顺序：`VIBEDEPLOY_TOKEN` > 凭证文件 > 提示运行 `deploy +login`。

## 配置

`~/.vibedeploy/config.json`（首次运行时由 `deploy +login` 写入；也可手动）：

```json
{
  "api_url": "http://localhost:8080",
  "app_base_domain": "lab.localhost"
}
```

## 实施细则

Skill 主体是 `scripts/` 目录里的几个 bash 脚本。每个命令都通过子命令分派。

**调用方式**：当用户在 Claude Code 里说 "deploy +push" / "部署一下" / "把当前目录推上去看看"，调用对应脚本。
脚本失败时退出码 != 0，并把错误打到 stderr。

**首次推送的项目名**：`push.sh` 在交互终端会先问 "设置项目名称？(Y/n)"，回车默认 Y 然后再问名字；选 n 或没 TTY（CI / Claude 工具调用）就用 scanner 猜的（通常是目录名）。无论哪种情况，**接受默认值时会显式 warn 一次**并给出 Admin UI 的修改路径，避免业务方部署完看到名字是 slug 不知道在哪改。Admin UI 项目页有 "项目名称" 面板可以随时改。

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
