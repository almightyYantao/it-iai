---
name: iai
description: iai (爱 AI) 一键部署。把当前项目打包、上传、构建并部署到公司内网子域名。当用户说 `deploy +push` / `deploy +status` / `deploy +logs` / `deploy +list` / `deploy +login` / `deploy +share` 时使用。Skill 会扫描 cwd 并通过 Control Plane API 完成部署，全程不需要用户写 Dockerfile / k8s manifest。自定义域名走 Web 后台。
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

> **自定义域名管理在 Web 后台做**：项目详情页 → 自定义域名 panel。在这里加比命令行更直观，并且能立刻看到重复占用 / DNS 提示等错误。

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
