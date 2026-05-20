import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

// Minimal i18n. No external dep — a flat key → string map with placeholder
// interpolation via {{name}}. zh is the default per product intent (internal
// platform for a Chinese-speaking company); en is offered as a switch.

export type Locale = "zh" | "en";

type Dict = Record<string, string>;

const STORAGE_KEY = "iai.locale";

const zh: Dict = {
  "app.name": "iai",
  "app.tagline": "爱 AI · 内部部署平台",
  "app.subtitle": "管理控制台",

  "nav.section": "平台",
  "nav.overview": "概览",
  "nav.projects": "项目",
  "nav.audit": "审计",
  "nav.settings": "设置",
  "nav.signout": "退出登录",
  "nav.role.admin": "管理员",
  "nav.role.member": "成员",
  "nav.role.token": "Deploy Token",
  "nav.role.scopes.none": "无 scope",

  // Login
  "login.title": "管理控制台",
  "login.field.token": "Deploy Token",
  "login.button.continue": "用 Token 登录",
  "login.button.continue.busy": "验证中…",
  "login.or": "或",
  "login.button.oidc": "使用 {{brand}} 登录",
  "login.footer":
    "Token 由平台节点签发。在控制面机器上执行 {{cmd}} 可重新生成。",

  // Overview
  "overview.eyebrow": "平台",
  "overview.title": "概览",
  "overview.description": "集群快照，每 5 秒刷新一次。",
  "overview.error": "无法加载指标。当前 token 可能没有 admin scope。",
  "overview.health.label": "集群健康",
  "overview.health.running": "运行中",
  "overview.health.errored": "出错",
  "overview.health.idle": "空闲",
  "overview.health.summary": "{{running}} / {{total}} 个项目在运行",
  "overview.tile.inflight": "正在部署",
  "overview.tile.inflight.hint.active": "构建或部署中",
  "overview.tile.inflight.hint.idle": "无活跃任务",
  "overview.tile.deployments24h": "24 小时内部署次数",
  "overview.tile.failures24h": "24 小时内失败次数",

  // Projects
  "projects.eyebrow": "工作负载",
  "projects.title": "项目",
  "projects.description.all": "集群上的所有项目。",
  "projects.description.mine": "你拥有或参与的项目。",
  "projects.toggle.mine": "我的",
  "projects.toggle.all": "全部",
  "projects.error.noadmin": "当前 token 没有 admin scope；自动切换到「我的」。",
  "projects.col.project": "项目",
  "projects.col.owner": "所有者",
  "projects.col.visibility": "可见性",
  "projects.col.status": "状态",
  "projects.col.lastpush": "最近推送",
  "projects.col.url": "URL",
  "projects.empty.title": "还没有项目",
  "projects.empty.description":
    "在任意项目目录里运行 {{cmd}} 把它推上来。",

  // Project detail
  "project.pod.node": "运行节点",
  "project.pod.phase": "Pod 状态",
  "project.pod.podip": "Pod IP",
  "project.pod.name": "Pod 名称",
  "project.recent": "最近的部署",
  "project.shown": "显示 {{count}} 条 · 每 5 秒刷新",
  "project.deployments.col.id": "ID",
  "project.deployments.col.status": "状态",
  "project.deployments.col.trigger": "触发方式",
  "project.deployments.col.image": "镜像",
  "project.deployments.col.started": "开始时间",
  "project.deployments.col.deployed": "部署时间",
  "project.deployments.empty.title": "还没有部署",
  "project.deployments.empty.description":
    "在项目目录里运行 {{cmd}} 触发首次构建。",
  "project.visibility": "可见性",
  "project.lastpushed": "最近推送 {{when}}",

  // Deployment detail
  "deployment.crumb": "部署",
  "deployment.created": "创建于 {{when}}",
  "deployment.via": "来源 {{trigger}}",
  "deployment.deployed": "部署于 {{when}}",
  "deployment.failed": "部署失败",
  "deployment.stream.ended": "事件流已结束。刷新页面可重新订阅。",
  "deployment.stream.disconnected": "事件流已断开。刷新页面可重新订阅。",
  "deployment.error.notfound": "找不到该部署",

  // Log viewer
  "log.events": "{{count}} 条事件",
  "log.streaming": "实时流",
  "log.copy": "复制",
  "log.copied": "已复制",
  "log.clear": "清除过滤",
  "log.waiting": "等待事件中…",
  "log.empty.filter": "当前过滤条件下没有事件。",
  "log.latest": "最新",
  "log.new": "{{count}} 条新事件",

  // Audit
  "audit.eyebrow": "合规",
  "audit.title": "审计日志",
  "audit.description": "最近 100 条 · 每 10 秒刷新。",
  "audit.empty.title": "暂无审计记录",
  "audit.empty.description": "每次创建、部署、撤销 token 都会写入一条审计。",
  "audit.col.when": "时间",
  "audit.col.actor": "执行者",
  "audit.col.action": "动作",
  "audit.col.project": "项目",
  "audit.col.metadata": "元数据",
  "audit.error.noadmin": "当前 token 没有 admin scope；审计日志仅管理员可见。",

  // Skill bridge
  "bridge.heading": "Skill / CLI",
  "bridge.copyfull": "复制 CLI 命令",
  "bridge.copied": "已复制",
  "bridge.show": "显示",
  "bridge.hide": "隐藏",
  "bridge.copytoken": "仅复制 token",

  // Misc
  "common.loading": "加载中…",
  "common.retry": "重试",
  "common.empty": "—",
  "common.cancel": "取消",
  "common.confirm": "确认",
  "common.save": "保存",
  "common.add": "添加",
  "common.remove": "移除",
  "common.search": "搜索",

  "pagination.perpage": "页",

  // Users (admin)
  "nav.users": "用户",
  "users.eyebrow": "成员",
  "users.title": "用户",
  "users.description": "组织里所有登录过的用户。",
  "users.col.email": "邮箱",
  "users.col.name": "姓名",
  "users.col.role": "角色",
  "users.col.lastseen": "最近活跃",
  "users.col.created": "首次登录",
  "users.col.actions": "操作",
  "users.role.admin": "管理员",
  "users.role.member": "成员",
  "users.action.promote": "设为管理员",
  "users.action.demote": "取消管理员",
  "users.empty.title": "暂无用户",
  "users.empty.description": "用户首次通过 {{brand}} 登录后会自动出现在这里。",
  "users.error.noadmin": "用户管理仅管理员可见。",
  "users.confirm.promote": "确认把 {{email}} 设为管理员？管理员可以看到所有项目、所有审计日志，以及给其他人提权。",
  "users.confirm.demote": "确认取消 {{email}} 的管理员身份？他们会立刻失去访问所有项目和审计的能力。",

  // Project access (IP allow-list)
  "access.title": "访问控制",
  "access.desc":
    "选择一个全局预设，或切到「自定义」单独维护本项目的 IP 白名单。预设由管理员在 设置 → 访问预设 统一维护。",
  "access.hint":
    "白名单只对入口 Ingress 起效；workers 之间和 pod 之间不受影响。修改后立即生效，无需重新部署。",
  "access.mode.label": "访问模式",
  "access.mode.custom": "自定义（本项目独立维护）",
  "access.mode.custom.hint": "不使用预设，填写下面的 IP / CIDR 列表。留空表示对所有来源开放。",
  "access.preset.empty.cidrs": "（这个预设当前没有配 IP，等同于对所有来源开放。）",
  "access.preset.list.label": "预设包含的 IP / CIDR",
  "access.preset.manage": "管理预设 →",
  "access.custom.placeholder": "10.0.0.0/8\n192.168.1.42\n2001:db8::/32",
  "access.state.open": "对所有人开放",
  "access.state.open.hint": "目前没有 IP 限制 —— 任何能解析到 ingress 的来源都能访问。",
  "access.state.restricted": "已限制",
  "access.state.restricted.hint": "仅白名单内的 IP 可以访问。",
  "access.auth.required": "需要登录 {{brand}}",
  "access.auth.required.hint":
    "可见性不是 public —— Traefik 会通过 oauth2-proxy ForwardAuth 拦截未登录的请求并跳转到 {{brand}} 完成 SSO。",
  "access.auth.open": "免登录",
  "access.auth.open.hint": "可见性是 public，任何人都能直接访问。",
  "access.count.none.preset": "预设当前未配 IP",
  "access.count.none": "未设置任何 IP",
  "access.count.n": "{{n}} 条规则",
  "access.button.save": "保存",
  "access.button.save.busy": "保存中…",
  "access.button.reset": "撤回修改",
  "access.toast.saved": "已保存",

  // Project name (rename)
  "name.title": "项目名称",
  "name.desc": "项目在 UI、列表和审计日志里的显示名。可以随时改；slug（URL 片段）固定不变。",
  "name.placeholder": "给项目起个好记的名字",
  "name.slug.label": "URL slug",
  "name.slug.fixed": "（创建后不可修改）",
  "name.warning.defaulted":
    "当前项目名等于 slug「{{slug}}」—— 初次部署时 Skill 没拿到 name（CI/非交互模式），先用 slug 顶上。建议改成更易读的名字。",
  "name.error.empty": "项目名不能为空。",
  "name.error.too_long": "项目名最长 120 个字符。",

  // Project HTTPS / TLS toggle
  "tls.title": "HTTPS",
  "tls.desc":
    "开启后，控制面会给项目 Ingress 加上 cert-manager 注解，由 cert-manager 通过 HTTP-01 申请每个域名的 Let's Encrypt 证书并自动续期。需要平台已安装 cert-manager（deploy/install-cert-manager.sh）。",
  "tls.label.enabled": "为本项目启用 HTTPS",
  "tls.state.on": "已启用",
  "tls.state.off": "未启用",
  "tls.hint.on":
    "新增的域名会在大约 30 秒后拿到证书。期间 HTTP 仍然可达；证书签发完成后浏览器才会看到绿锁。",
  "tls.hint.off":
    "项目目前只走 HTTP。开启前确保所有域名解析已到位且 :80 公网可达 —— Let's Encrypt 的校验请求会从公网发出。",
  "tls.toast.saved": "已保存，证书将在后台签发",

  // Danger zone — project delete
  "danger.title": "危险区域",
  "danger.delete.desc":
    "删除 {{name}} 会软删数据库记录，并立即清理集群内的命名空间（部署、Service、Ingress 都会一并被销毁）。审计日志会保留。这个动作无法在 UI 撤销。",
  "danger.delete.confirm.label": "请输入项目 slug “{{slug}}” 确认删除：",
  "danger.delete.button": "永久删除该项目",
  "danger.delete.button.busy": "删除中…",

  // Env panel
  "env.heading": "环境变量",
  "env.description":
    "这里改的值会加密落库（用平台 KEK），部署时注入到 pod 的环境变量。比 .vibedeploy.toml 里写明文更安全；同 key 时这里的值覆盖 manifest。",
  "env.col.key": "Key",
  "env.col.updated": "更新时间",
  "env.col.actions": "操作",
  "env.empty": "暂无环境变量。",
  "env.system.tag": "平台管理",
  "env.system.hint": "这一项由平台自动维护（例如自动创库后的连接串），不能在这里手工删除。",
  "env.add.key.placeholder": "DATABASE_URL",
  "env.add.value.placeholder": "（值在保存时加密，离开页面后无法回看）",
  "env.add.button": "添加 / 更新",
  "env.add.busy": "保存中…",
  "env.add.hint":
    "保存后立即同步到运行中的 pod（K8s 自动滚动）。只有项目所有者或管理员可改。",
  "env.error.invalid": "Key 必须匹配 [A-Za-z_][A-Za-z0-9_]{0,127}（POSIX 环境变量规范）。",
  "env.error.system": "这个 key 是平台管理的，不能在 UI 改 / 删。",
  "env.remove.confirm": "确认删除「{{key}}」？保存后立即从 pod 撤掉，可能导致应用挂掉。",

  // Domains panel
  "domains.heading": "自定义域名",
  "domains.description":
    "把自己的域名指向平台后再加到这里，Traefik 会自动给它挂上 TLS 和登录中间件。默认子域名（{{default}}）始终可用，不需要单独添加。",
  "domains.default.tag": "默认子域名",
  "domains.col.hostname": "域名",
  "domains.col.kind": "类型",
  "domains.col.added": "添加时间",
  "domains.col.actions": "操作",
  "domains.kind.subdomain": "平台子域",
  "domains.kind.custom": "自定义",
  "domains.empty": "暂无自定义域名。",
  "domains.subdomain.heading": "自定义子域",
  "domains.subdomain.hint":
    "只填子域前缀，后缀 .{{base}} 自动补全。不用配 DNS，TLS 也自动用平台通配证书。",
  "domains.subdomain.placeholder": "my-app",
  "domains.subdomain.button": "添加子域",
  "domains.custom.heading": "自定义域名",
  "domains.custom.hint":
    "用你自己的域名时填这里。先把 DNS A 记录指到平台 IP（或 CNAME 到 {{default}}），再来添加。仅项目所有者或管理员可改。",
  "domains.add.placeholder": "app.your-domain.com",
  "domains.add.button": "添加",
  "domains.add.busy": "添加中…",
  "domains.add.hint":
    "提示：先把域名 DNS 指到平台 IP（A 记录或 CNAME 到 {{default}}），再来添加。仅项目所有者或管理员可改。",
  "domains.error.taken": "该域名已被其他项目占用。",
  "domains.error.invalid": "请输入合法的域名（如 app.example.com）。",
  "domains.error.reserved": "该域名在平台通配子域内，平台会自动管理，无需手动添加。",
  "domains.error.limit_reached": "已达本项目自定义域名上限，先移除一个再添加。",
  "domains.limit.reached": "每个项目最多 {{max}} 个自定义域名，删除现有的再来添加新的。如有特殊需要请联系管理员调整 CP_MAX_CUSTOM_DOMAINS。",
  "domains.remove.confirm": "确认移除「{{hostname}}」？该域名将立即停止解析到本项目。",

  // Collaborators panel
  "collab.heading": "协作者",
  "collab.description": "协作者能看到这个项目、推送、读日志。所有者是 {{owner}}。",
  "collab.add.placeholder": "员工邮箱，必须已经登录过一次",
  "collab.add.button": "添加",
  "collab.empty": "暂无协作者。所有者：{{owner}}。",
  "collab.col.email": "邮箱",
  "collab.col.role": "角色",
  "collab.col.added": "添加时间",
  "collab.role.editor": "编辑",
  "collab.role.admin": "管理员",
  "collab.remove.confirm": "确认移除 {{email}}？",

  // Status badge labels (lowercase to match server status strings)
  "status.running": "运行中",
  "status.queued": "排队中",
  "status.building": "构建中",
  "status.pushing": "推送中",
  "status.deploying": "部署中",
  "status.pending": "等待中",
  "status.created": "已创建",
  "status.stopped": "已停止",
  "status.superseded": "已被取代",
  "status.failed": "失败",
  "status.error": "错误",
  "status.deleting": "删除中",

  "visibility.org": "组织",
  "visibility.restricted": "受限",
  "visibility.public": "公开",

  // Skill tutorial page
  "nav.skill": "Skill 指南",
  "skill.eyebrow": "Claude Code 集成",
  "skill.title": "在 Claude Code 中部署",
  "skill.description":
    "安装 iai Skill，之后在 Claude Code 里直接说「部署一下」「看构建日志」就行 —— 不用记命令、不用写 Dockerfile。",
  "skill.prereq.title": "先决条件",
  "skill.prereq.claude": "已经装了 Claude Code（命令行 claude）",
  "skill.prereq.deps": "本机有 jq / tar / zstd / curl / git（macOS 用 brew install jq zstd 装齐）",
  "skill.prereq.repo": "本机上有 git（macOS / Linux 一般自带）",
  "skill.quickstart.label": "一键安装",
  "skill.quickstart.title": "想偷懒？一行命令搞定",
  "skill.quickstart.desc":
    "下面这条命令会克隆仓库、自动装依赖、把 Skill 链接进 ~/.claude/skills/、写好 config + token，并打 healthz 校验是否通。装完之后跳到第 3 步直接用。",
  "skill.quickstart.hint":
    "这条命令是幂等的——升级时复制同一行重新跑就能拿到最新代码。诊断现有安装：bash ~/iai/skill/install.sh check。",
  "skill.step1.label": "第 1 步",
  "skill.step1.title": "克隆仓库 + 安装 Skill",
  "skill.step1.intro": "先把仓库克隆下来，再把仓库里的 skill/ 目录暴露给 Claude Code。",
  "skill.step1.clone.title": "克隆 iai 仓库",
  "skill.step1.clone.desc": "推荐放在 ~/iai。已经克隆过可以跳过。",
  "skill.step1.link.title": "接入 Claude Code",
  "skill.step1.link.intro": "三种方式任选其一：",
  "skill.step1.symlink.tag": "推荐",
  "skill.step1.symlink.title": "符号链接 Symlink",
  "skill.step1.symlink.desc": "之后改代码不用重装，重启 Claude Code 就生效。",
  "skill.step1.copy.title": "拷贝一份",
  "skill.step1.copy.desc": "不会随仓库更新，更新时需要重复执行。",
  "skill.step1.npx.title": "npx skills install",
  "skill.step1.npx.desc": "如果你装了 skills CLI（npm i -g skills）",
  "skill.step1.verify.title": "验证装好了",
  "skill.step1.verify.desc": "看见 SKILL.md 和 scripts 子目录就对了：",
  "skill.step2.label": "第 2 步",
  "skill.step2.title": "配置访问令牌",
  "skill.step2.intro": "Skill 通过控制面 API 工作，需要一个 token。下面这两行直接复制粘到你的 ~/.zshrc 或 ~/.bashrc，然后 source ~/.zshrc 让它生效 —— 你当前登录用的 token 已经填进去了。",
  "skill.step2.reveal": "显示 token",
  "skill.step2.hide": "隐藏 token",
  "skill.step2.copy": "复制到剪贴板",
  "skill.step2.copied": "已复制",
  "skill.step2.verify.title": "验证",
  "skill.step2.verify.desc": "重开一个终端跑这条，应该返回你的身份：",
  "skill.step2.note": "Token 默认 90 天有效；重新登录 Web 控制台会 mint 一个新的，老的可以继续用到过期。CI 场景下应该用 deploy +token create 单独签一个固定 token，不要复用浏览器 token。",
  "skill.step3.label": "第 3 步",
  "skill.step3.title": "在 Claude Code 里使用",
  "skill.step3.intro": "重启 Claude Code 让它扫到新 Skill。然后 cd 到你的项目目录，启动 claude，直接用自然语言告诉它要做什么 —— 触发词都写在 SKILL.md 里，下面的说法都能调起来：",
  "skill.step3.example1": "「把这个项目部署一下」",
  "skill.step3.example2": "「deploy +push」",
  "skill.step3.example3": "「看一下最新部署的日志」",
  "skill.step3.example4": "「我都部了什么项目？」",
  "skill.step3.example5": "「给 alice@example.com 开协作权限」",
  "skill.step3.example6": "「绑定 app.example.com 这个域名」",
  "skill.commands.title": "命令清单",
  "skill.commands.intro": "你也可以直接用 deploy +xxx 的形式 —— 严格命令名 + 参数。",
  "skill.commands.col.cmd": "命令",
  "skill.commands.col.desc": "用途",
  "skill.commands.row.push": "扫描当前目录 → 打包 → 上传 → 构建 → 部署，并实时流式输出日志",
  "skill.commands.row.status": "查项目状态和 URL",
  "skill.commands.row.logs": "取构建 / 运行日志（-f 流式 follow）",
  "skill.commands.row.list": "列我能看到的项目",
  "skill.commands.row.share": "添加 / 移除协作者",
  "skill.commands.row.domain": "添加 / 移除自定义域名",
  "skill.commands.row.whoami": "显示当前身份和 scope",
  "skill.tips.title": "高级技巧",
  "skill.tips.toml.title": ".vibedeploy.toml — 给项目锁死配置",
  "skill.tips.toml.intro": "在项目根目录创建这个文件，Skill 扫描时会优先用里面的值（覆盖自动检测）：",
  "skill.tips.ignore.title": ".deployignore — 排除文件",
  "skill.tips.ignore.intro": "非 git 仓库的项目可以用 .deployignore（gitignore 语法）排除不该上传的文件。git 仓库会自动尊重 .gitignore，不需要单独写。",
  "skill.faq.title": "常见问题",
  "skill.faq.q1": "Claude 不识别 deploy 命令？",
  "skill.faq.a1": "重启 Claude Code。Skill 是启动时扫描的。",
  "skill.faq.q2": "推送报 「no token」？",
  "skill.faq.a2": "VIBEDEPLOY_TOKEN 没在当前 shell 的环境里。要么 source ~/.zshrc，要么在启动 Claude Code 前已经 export 过。",
  "skill.faq.q3": "推送很慢？",
  "skill.faq.a3": "第一次构建会拉 nixpacks 工具链 + 基础镜像，2-4 分钟正常。第二次推同一个项目会快很多。",
  "skill.cta.title": "好了，去推一个看看",
  "skill.cta.body": "回到你的终端，cd 到任一项目目录，开 Claude Code 说「部署一下」。",

  // System settings (admin)
  "settings.eyebrow": "平台",
  "settings.title": "系统设置",
  "settings.description":
    "Keycloak / OIDC 与网关认证的运行时参数。保存后立即生效，无需重启。留空表示恢复为环境变量默认值。",
  "settings.error.noadmin": "系统设置仅管理员可见。",
  "settings.section.kc.title": "Keycloak OIDC",
  "settings.section.kc.desc":
    "Web 控制台和 Skill 都用这一套 OIDC 配置走代码流登录。错填会让所有人无法登录 —— 改之前先确认。",
  "settings.section.auth.title": "网关认证（oauth2-proxy）",
  "settings.section.auth.desc":
    "用户应用走 Traefik ForwardAuth → oauth2-proxy → Keycloak。修改这里之后还要重新跑一次 deploy/install-oauth2-proxy.sh 让网关生效。",
  "settings.field.issuer": "Issuer",
  "settings.field.issuer.hint":
    "OIDC iss 字段，e.g. https://kc.example.com/realms/lb。验签和 redirect 都依赖这个。",
  "settings.field.jwks_url": "JWKS URL",
  "settings.field.jwks_url.hint":
    "通常是 issuer + /protocol/openid-connect/certs。",
  "settings.field.audience": "Audience",
  "settings.field.audience.hint":
    "Keycloak access_token 的 aud 声明。客户端没配 Audience mapper 就留空。",
  "settings.field.authorization_url": "Authorization URL",
  "settings.field.authorization_url.hint":
    "通常是 issuer + /protocol/openid-connect/auth。",
  "settings.field.token_url": "Token URL",
  "settings.field.token_url.hint":
    "通常是 issuer + /protocol/openid-connect/token。",
  "settings.field.client_id": "Client ID",
  "settings.field.client_secret": "Client Secret",
  "settings.field.client_secret.hint":
    "敏感信息。已配置时显示为 ********；想替换直接输入新值，想清空就留空。",
  "settings.field.redirect_url": "Redirect URL",
  "settings.field.redirect_url.hint":
    "Keycloak 客户端的 valid-redirect-uri 之一，e.g. https://iai.example.com/v1/auth/oidc-callback。",
  "settings.field.auth_host": "Auth host",
  "settings.field.auth_host.hint":
    "oauth2-proxy 的入口域名，e.g. auth.iai.example.com。",
  "settings.field.cookie_secret": "Cookie Secret",
  "settings.field.cookie_secret.hint":
    "32 字节 base64。生成：openssl rand -base64 32。",
  "settings.field.cookie_domain": "Cookie Domain",
  "settings.field.cookie_domain.hint":
    "用户应用所在的域，跨子域共享 cookie，e.g. .iai.example.com。",
  "settings.field.brand_name": "SSO 品牌名",
  "settings.field.brand_name.hint":
    "在登录按钮、用户管理、访问控制等界面上显示。例如 \"Longbridge Account\"、\"Acme SSO\"。留空显示 \"SSO\"。改完立即生效。",
  "settings.placeholder.secret.set": "已设置 · 改新值则覆盖",
  "settings.placeholder.secret.empty": "未设置",
  "settings.badge.env": "环境变量",
  "settings.badge.db": "数据库覆盖",
  "settings.badge.updated": "更新于 {{when}}",
  "settings.button.save": "保存",
  "settings.button.save.busy": "保存中…",
  "settings.button.reset": "撤回未保存的修改",
  "settings.toast.saved": "已保存，{{count}} 项更新。",
  "settings.toast.failed": "保存失败：{{error}}",

  // Access presets section
  "presets.section.title": "访问预设",
  "presets.section.desc":
    "全局维护的 IP 白名单，项目通过下拉选择使用。修改预设后，所有引用它的项目会自动重新挂载白名单中间件，无需重新部署。",
  "presets.col.label": "名称",
  "presets.col.cidrs": "IP / CIDR",
  "presets.col.system": "类型",
  "presets.col.actions": "操作",
  "presets.cidrs.count": "{{n}} 条",
  "presets.system.tag": "系统预设",
  "presets.system.hint": "系统预设（public / internal）不能删除，但可以修改 CIDR 列表。",
  "presets.empty.title": "暂无非系统预设",
  "presets.empty.desc": "点击下面的「新增预设」给团队加自定义白名单。",
  "presets.add.button": "新增预设",
  "presets.edit.title": "编辑「{{label}}」",
  "presets.new.title": "新增预设",
  "presets.field.name": "唯一标识（slug）",
  "presets.field.name.hint": "用于 URL / API；小写字母数字加横线，3-30 字符。创建后不可改。",
  "presets.field.label": "显示名",
  "presets.field.label.hint": "项目页下拉里看到的名字，例如「内部访问」。",
  "presets.field.description": "说明（可选）",
  "presets.field.cidrs": "IP / CIDR 列表",
  "presets.field.cidrs.hint": "每行一个，例如 10.0.0.0/8 或单个 IP。留空表示对所有来源开放。",
  "presets.button.save": "保存",
  "presets.button.save.busy": "保存中…",
  "presets.button.delete": "删除",
  "presets.button.cancel": "取消",
  "presets.confirm.delete": "确认删除「{{label}}」？正在使用这个预设的项目会落回自定义模式。",
  "presets.toast.saved": "已保存",
  "presets.toast.deleted": "已删除",
};

const en: Dict = {
  "app.name": "iai",
  "app.tagline": "Love AI · Internal deploy platform",
  "app.subtitle": "Admin console",

  "nav.section": "Platform",
  "nav.overview": "Overview",
  "nav.projects": "Projects",
  "nav.audit": "Audit",
  "nav.settings": "Settings",
  "nav.signout": "Sign out",
  "nav.role.admin": "Admin",
  "nav.role.member": "Member",
  "nav.role.token": "Deploy token",
  "nav.role.scopes.none": "no scopes",

  "login.title": "Admin console",
  "login.field.token": "Deploy token",
  "login.button.continue": "Continue with token",
  "login.button.continue.busy": "Verifying…",
  "login.or": "or",
  "login.button.oidc": "Sign in with {{brand}}",
  "login.footer":
    "Tokens are issued from the platform host. Run {{cmd}} there to mint one.",

  "overview.eyebrow": "Platform",
  "overview.title": "Overview",
  "overview.description": "Cluster snapshot, refreshes every 5 seconds.",
  "overview.error": "Couldn't load metrics. Your token may not have admin scope.",
  "overview.health.label": "Cluster health",
  "overview.health.running": "Running",
  "overview.health.errored": "Errored",
  "overview.health.idle": "Idle",
  "overview.health.summary": "{{running}} of {{total}} projects running",
  "overview.tile.inflight": "In flight now",
  "overview.tile.inflight.hint.active": "building or deploying",
  "overview.tile.inflight.hint.idle": "no active builds",
  "overview.tile.deployments24h": "Deployments · last 24h",
  "overview.tile.failures24h": "Failures · last 24h",

  "projects.eyebrow": "Workloads",
  "projects.title": "Projects",
  "projects.description.all": "Every project across the cluster.",
  "projects.description.mine": "Projects you own or collaborate on.",
  "projects.toggle.mine": "Mine",
  "projects.toggle.all": "All",
  "projects.error.noadmin": "This token doesn't have admin scope; viewing \"Mine\" instead.",
  "projects.col.project": "Project",
  "projects.col.owner": "Owner",
  "projects.col.visibility": "Visibility",
  "projects.col.status": "Status",
  "projects.col.lastpush": "Last push",
  "projects.col.url": "URL",
  "projects.empty.title": "No projects yet",
  "projects.empty.description":
    "From any project directory, run {{cmd}} with your token exported.",

  "project.pod.node": "Running on",
  "project.pod.phase": "Phase",
  "project.pod.podip": "Pod IP",
  "project.pod.name": "Pod name",
  "project.recent": "Recent deployments",
  "project.shown": "{{count}} shown · refreshes every 5s",
  "project.deployments.col.id": "ID",
  "project.deployments.col.status": "Status",
  "project.deployments.col.trigger": "Trigger",
  "project.deployments.col.image": "Image",
  "project.deployments.col.started": "Started",
  "project.deployments.col.deployed": "Deployed",
  "project.deployments.empty.title": "No deployments yet",
  "project.deployments.empty.description":
    "Run {{cmd}} from a project directory to start the first build.",
  "project.visibility": "Visibility",
  "project.lastpushed": "last pushed {{when}}",

  "deployment.crumb": "Deployment",
  "deployment.created": "created {{when}}",
  "deployment.via": "via {{trigger}}",
  "deployment.deployed": "deployed {{when}}",
  "deployment.failed": "Deployment failed",
  "deployment.stream.ended": "Stream ended. Reload to re-subscribe.",
  "deployment.stream.disconnected": "Stream disconnected. Reload to re-subscribe.",
  "deployment.error.notfound": "Deployment not found",

  "log.events": "{{count}} events",
  "log.streaming": "streaming",
  "log.copy": "Copy",
  "log.copied": "Copied",
  "log.clear": "Clear",
  "log.waiting": "Waiting for events…",
  "log.empty.filter": "No events match the current filter.",
  "log.latest": "Latest",
  "log.new": "{{count}} new",

  "audit.eyebrow": "Compliance",
  "audit.title": "Audit",
  "audit.description": "Last 100 entries · refreshes every 10s.",
  "audit.empty.title": "No audit entries yet",
  "audit.empty.description":
    "Entries are written every time something is created, deployed, or revoked.",
  "audit.col.when": "When",
  "audit.col.actor": "Actor",
  "audit.col.action": "Action",
  "audit.col.project": "Project",
  "audit.col.metadata": "Metadata",
  "audit.error.noadmin": "This token doesn't have admin scope; the audit log is admin-only.",

  "bridge.heading": "Skill / CLI",
  "bridge.copyfull": "Copy CLI",
  "bridge.copied": "Copied",
  "bridge.show": "Show",
  "bridge.hide": "Hide",
  "bridge.copytoken": "Copy token only",

  "common.loading": "Loading…",
  "common.retry": "Retry",
  "common.empty": "—",
  "common.cancel": "Cancel",
  "common.confirm": "Confirm",
  "common.save": "Save",
  "common.add": "Add",
  "common.remove": "Remove",
  "common.search": "Search",

  "pagination.perpage": "page",

  "nav.users": "Users",
  "users.eyebrow": "Members",
  "users.title": "Users",
  "users.description": "Everyone who has signed in at least once.",
  "users.col.email": "Email",
  "users.col.name": "Name",
  "users.col.role": "Role",
  "users.col.lastseen": "Last seen",
  "users.col.created": "First seen",
  "users.col.actions": "Actions",
  "users.role.admin": "Admin",
  "users.role.member": "Member",
  "users.action.promote": "Make admin",
  "users.action.demote": "Revoke admin",
  "users.empty.title": "No users yet",
  "users.empty.description": "Users appear here after they sign in via {{brand}} for the first time.",
  "users.error.noadmin": "User management is admin-only.",
  "users.confirm.promote": "Promote {{email}} to admin? Admins see every project, every audit entry, and can promote others.",
  "users.confirm.demote": "Revoke admin from {{email}}? They lose access to all projects and audit immediately.",

  "access.title": "Access control",
  "access.desc":
    "Pick a globally-maintained preset, or switch to “Custom” to keep a per-project allow-list. Admins maintain presets in Settings → Access presets.",
  "access.hint":
    "Applies at the ingress only; pod-to-pod and worker-to-worker traffic is unaffected. Saves take effect immediately — no redeploy needed.",
  "access.mode.label": "Access mode",
  "access.mode.custom": "Custom (per-project)",
  "access.mode.custom.hint": "Skip presets and edit the IP / CIDR list below. Empty = open to everyone.",
  "access.preset.empty.cidrs": "(this preset has no IPs configured yet — equivalent to open access.)",
  "access.preset.list.label": "Preset CIDRs",
  "access.preset.manage": "Manage presets →",
  "access.custom.placeholder": "10.0.0.0/8\n192.168.1.42\n2001:db8::/32",
  "access.state.open": "Open to all",
  "access.state.open.hint": "No IP restriction — anyone who can resolve the ingress can reach the app.",
  "access.state.restricted": "Restricted",
  "access.state.restricted.hint": "Only the listed IPs can reach the app.",
  "access.auth.required": "{{brand}} login required",
  "access.auth.required.hint":
    "Visibility isn't public — Traefik intercepts unauthenticated requests via oauth2-proxy ForwardAuth and bounces them through {{brand}} SSO.",
  "access.auth.open": "No login",
  "access.auth.open.hint": "Visibility is public — anyone can reach the app directly.",
  "access.count.none.preset": "preset has no IPs configured",
  "access.count.none": "No IPs configured",
  "access.count.n": "{{n}} rule(s)",
  "access.button.save": "Save",
  "access.button.save.busy": "Saving…",
  "access.button.reset": "Discard changes",
  "access.toast.saved": "Saved",

  "name.title": "Project name",
  "name.desc": "The display name shown in the UI, project lists, and audit log. Editable any time; the slug (URL fragment) is fixed.",
  "name.placeholder": "Give the project a friendly name",
  "name.slug.label": "URL slug",
  "name.slug.fixed": "(fixed after creation)",
  "name.warning.defaulted":
    "This project's name is the same as its slug \"{{slug}}\" — the Skill couldn't get a name on first push (CI / non-interactive). Pick a more readable name.",
  "name.error.empty": "Name can't be empty.",
  "name.error.too_long": "Name must be 120 characters or fewer.",

  "tls.title": "HTTPS",
  "tls.desc":
    "When enabled, the control plane adds a cert-manager annotation to the project's Ingress; cert-manager solves an HTTP-01 challenge per hostname and provisions a Let's Encrypt cert that renews automatically. Requires deploy/install-cert-manager.sh on the platform.",
  "tls.label.enabled": "Enable HTTPS for this project",
  "tls.state.on": "Enabled",
  "tls.state.off": "Disabled",
  "tls.hint.on":
    "New hostnames typically receive their cert within ~30s. HTTP keeps working in the meantime; the browser shows the lock once issuance completes.",
  "tls.hint.off":
    "The project is served over plain HTTP. Before flipping this on make sure DNS for every hostname resolves to the platform and :80 is reachable from the public internet — Let's Encrypt's validators come from outside.",
  "tls.toast.saved": "Saved — cert issuance runs in the background",

  "danger.title": "Danger zone",
  "danger.delete.desc":
    "Deleting {{name}} soft-deletes the DB record and tears down its cluster namespace (Deployment, Service, Ingress all destroyed). Audit history is preserved. This cannot be undone from the UI.",
  "danger.delete.confirm.label": "Type the project slug \"{{slug}}\" to confirm:",
  "danger.delete.button": "Delete this project permanently",
  "danger.delete.button.busy": "Deleting…",

  "env.heading": "Environment variables",
  "env.description":
    "Values here are encrypted at rest with the platform KEK and injected into the pod on deploy. Safer than putting them in .vibedeploy.toml; same key set here overrides the manifest.",
  "env.col.key": "Key",
  "env.col.updated": "Updated",
  "env.col.actions": "Actions",
  "env.empty": "No env variables set.",
  "env.system.tag": "platform-managed",
  "env.system.hint": "This entry is provisioned automatically (e.g. auto-created DATABASE_URL). Can't be edited or deleted manually.",
  "env.add.key.placeholder": "DATABASE_URL",
  "env.add.value.placeholder": "(encrypted on save; can't be read back from here)",
  "env.add.button": "Add / update",
  "env.add.busy": "Saving…",
  "env.add.hint":
    "Saves apply to the running pod immediately (K8s rolls automatically). Owner or admin only.",
  "env.error.invalid": "Key must match [A-Za-z_][A-Za-z0-9_]{0,127} (POSIX env-var rules).",
  "env.error.system": "This key is platform-managed and can't be edited or deleted from the UI.",
  "env.remove.confirm": "Remove “{{key}}”? It's removed from the running pod immediately, which may crash the app.",

  "domains.heading": "Custom domains",
  "domains.description":
    "Point a DNS record at the platform first, then add the hostname here. Traefik auto-attaches the wildcard TLS cert and the SSO middleware. The default subdomain ({{default}}) is always served — no need to add it explicitly.",
  "domains.default.tag": "Default subdomain",
  "domains.col.hostname": "Hostname",
  "domains.col.kind": "Kind",
  "domains.col.added": "Added",
  "domains.col.actions": "Actions",
  "domains.kind.subdomain": "Platform subdomain",
  "domains.kind.custom": "Custom",
  "domains.empty": "No custom domains yet.",
  "domains.subdomain.heading": "Custom subdomain",
  "domains.subdomain.hint":
    "Just the subdomain prefix — .{{base}} is appended automatically. No DNS setup, TLS is served by the platform's wildcard cert.",
  "domains.subdomain.placeholder": "my-app",
  "domains.subdomain.button": "Add subdomain",
  "domains.custom.heading": "Custom domain",
  "domains.custom.hint":
    "Use your own domain. Set DNS first (A record to platform IP, or CNAME to {{default}}), then add it here. Owner or admin only.",
  "domains.add.placeholder": "app.your-domain.com",
  "domains.add.button": "Add",
  "domains.add.busy": "Adding…",
  "domains.add.hint":
    "Tip: set DNS first (A record or CNAME to {{default}}), then add it here. Owner or admin only.",
  "domains.error.taken": "This hostname is already bound to another project.",
  "domains.error.invalid": "Enter a valid hostname (e.g. app.example.com).",
  "domains.error.reserved": "This hostname is on the platform's wildcard domain — managed automatically, no need to add it.",
  "domains.error.limit_reached": "This project is at its custom-domain limit. Remove one before adding another.",
  "domains.limit.reached": "Each project can have at most {{max}} custom domain(s). Delete an existing one to add a new one, or ask an admin to raise CP_MAX_CUSTOM_DOMAINS.",
  "domains.remove.confirm": "Remove “{{hostname}}”? It will stop resolving to this project immediately.",

  "collab.heading": "Collaborators",
  "collab.description": "Collaborators can see this project, push, and read logs. Owner is {{owner}}.",
  "collab.add.placeholder": "Teammate email — they must have signed in once",
  "collab.add.button": "Add",
  "collab.empty": "No collaborators yet. Owner: {{owner}}.",
  "collab.col.email": "Email",
  "collab.col.role": "Role",
  "collab.col.added": "Added",
  "collab.role.editor": "Editor",
  "collab.role.admin": "Admin",
  "collab.remove.confirm": "Remove {{email}}?",

  "status.running": "running",
  "status.queued": "queued",
  "status.building": "building",
  "status.pushing": "pushing",
  "status.deploying": "deploying",
  "status.pending": "pending",
  "status.created": "created",
  "status.stopped": "stopped",
  "status.superseded": "superseded",
  "status.failed": "failed",
  "status.error": "error",
  "status.deleting": "deleting",

  "visibility.org": "org",
  "visibility.restricted": "restricted",
  "visibility.public": "public",

  "nav.skill": "Skill guide",
  "skill.eyebrow": "Claude Code integration",
  "skill.title": "Deploy from Claude Code",
  "skill.description":
    "Install the iai Skill once, then just talk to Claude Code — \"deploy this\", \"show me the build log\". No commands to memorise, no Dockerfile to write.",
  "skill.prereq.title": "Prerequisites",
  "skill.prereq.claude": "Claude Code already installed (the `claude` CLI)",
  "skill.prereq.deps": "jq / tar / zstd / curl / git on your machine (macOS: `brew install jq zstd`)",
  "skill.prereq.repo": "git installed (standard on macOS / Linux)",
  "skill.quickstart.label": "One-liner install",
  "skill.quickstart.title": "Lazy path: one command",
  "skill.quickstart.desc":
    "Clones the repo, installs deps via your package manager, symlinks the skill into ~/.claude/skills/, writes the config and token, and probes the platform. Skip to step 3 once it's green.",
  "skill.quickstart.hint":
    "Idempotent — paste the same line to upgrade. To diagnose an existing install: bash ~/iai/skill/install.sh check.",
  "skill.step1.label": "Step 1",
  "skill.step1.title": "Clone the repo + install the Skill",
  "skill.step1.intro": "Clone the repo, then expose its skill/ directory to Claude Code.",
  "skill.step1.clone.title": "Clone the iai repo",
  "skill.step1.clone.desc": "Recommended location: ~/iai. Skip if you've cloned it before.",
  "skill.step1.link.title": "Wire it into Claude Code",
  "skill.step1.link.intro": "Pick one method:",
  "skill.step1.symlink.tag": "Recommended",
  "skill.step1.symlink.title": "Symlink",
  "skill.step1.symlink.desc": "Edits to the skill apply on next Claude Code restart — no re-install.",
  "skill.step1.copy.title": "Copy",
  "skill.step1.copy.desc": "Frozen copy. You have to re-run this when the upstream skill changes.",
  "skill.step1.npx.title": "npx skills install",
  "skill.step1.npx.desc": "If you've installed the skills CLI (npm i -g skills).",
  "skill.step1.verify.title": "Verify",
  "skill.step1.verify.desc": "You should see SKILL.md and a scripts/ directory:",
  "skill.step2.label": "Step 2",
  "skill.step2.title": "Configure your access token",
  "skill.step2.intro": "The Skill talks to the control plane API and needs a token. Copy these two lines into your ~/.zshrc or ~/.bashrc, then run source ~/.zshrc — your current session's token is pre-filled.",
  "skill.step2.reveal": "Show token",
  "skill.step2.hide": "Hide token",
  "skill.step2.copy": "Copy",
  "skill.step2.copied": "Copied",
  "skill.step2.verify.title": "Verify",
  "skill.step2.verify.desc": "Open a fresh terminal and run this — should return your identity:",
  "skill.step2.note": "The token is valid for 90 days. Logging back in mints a new one; the old one keeps working until it expires. For CI use `deploy +token create` to mint a dedicated token — don't reuse the browser one.",
  "skill.step3.label": "Step 3",
  "skill.step3.title": "Use it from Claude Code",
  "skill.step3.intro": "Restart Claude Code so it picks up the new Skill. Then cd into any project, run claude, and say what you want in natural language. Any of these triggers the Skill:",
  "skill.step3.example1": "\"Deploy this project\"",
  "skill.step3.example2": "\"deploy +push\"",
  "skill.step3.example3": "\"Show me the latest deployment's logs\"",
  "skill.step3.example4": "\"What have I deployed?\"",
  "skill.step3.example5": "\"Add alice@example.com as a collaborator\"",
  "skill.step3.example6": "\"Bind app.example.com to this project\"",
  "skill.commands.title": "Command reference",
  "skill.commands.intro": "You can also type `deploy +<verb>` literally — strict command + args.",
  "skill.commands.col.cmd": "Command",
  "skill.commands.col.desc": "What it does",
  "skill.commands.row.push": "Scan cwd, pack, upload, build, deploy, and stream the log",
  "skill.commands.row.status": "Show project status + URL",
  "skill.commands.row.logs": "Tail build / runtime logs (use -f for follow)",
  "skill.commands.row.list": "List projects you can see",
  "skill.commands.row.share": "Add / remove collaborators",
  "skill.commands.row.domain": "Add / remove custom domains",
  "skill.commands.row.whoami": "Show current identity and scopes",
  "skill.tips.title": "Power moves",
  "skill.tips.toml.title": ".vibedeploy.toml — pin the project's config",
  "skill.tips.toml.intro": "Drop this file at your project's root. Skill reads it first and overrides auto-detection:",
  "skill.tips.ignore.title": ".deployignore — exclude files",
  "skill.tips.ignore.intro": "For non-git projects, use .deployignore (gitignore syntax) to exclude files that shouldn't ship. Git repos honour .gitignore automatically — you don't need a separate file.",
  "skill.faq.title": "FAQ",
  "skill.faq.q1": "Claude doesn't recognise the deploy command?",
  "skill.faq.a1": "Restart Claude Code. Skills are scanned on startup.",
  "skill.faq.q2": "Push reports \"no token\"?",
  "skill.faq.a2": "VIBEDEPLOY_TOKEN isn't in the shell environment your Claude Code is running in. `source ~/.zshrc` or export it before launching claude.",
  "skill.faq.q3": "First push is slow?",
  "skill.faq.a3": "First-time builds fetch the nixpacks toolchain + base image; 2-4 minutes is normal. Subsequent pushes on the same project are much faster.",
  "skill.cta.title": "Now go push something",
  "skill.cta.body": "Drop into a terminal, cd to a project, launch Claude Code and say \"deploy this\".",

  "settings.eyebrow": "Platform",
  "settings.title": "System settings",
  "settings.description":
    "Runtime parameters for Keycloak / OIDC and gateway auth. Saves take effect immediately, no restart needed. Leave a field blank to fall back to the env-default.",
  "settings.error.noadmin": "System settings are admin-only.",
  "settings.section.kc.title": "Keycloak OIDC",
  "settings.section.kc.desc":
    "Web console and the Skill both use this OIDC client for the code flow. Misconfigure these and nobody can sign in — double-check before saving.",
  "settings.section.auth.title": "Gateway auth (oauth2-proxy)",
  "settings.section.auth.desc":
    "User apps go through Traefik ForwardAuth → oauth2-proxy → Keycloak. After editing these, re-run deploy/install-oauth2-proxy.sh to roll the change into the cluster.",
  "settings.field.issuer": "Issuer",
  "settings.field.issuer.hint":
    "OIDC iss claim, e.g. https://kc.example.com/realms/lb. Used for signature verification and redirect.",
  "settings.field.jwks_url": "JWKS URL",
  "settings.field.jwks_url.hint":
    "Usually issuer + /protocol/openid-connect/certs.",
  "settings.field.audience": "Audience",
  "settings.field.audience.hint":
    "The aud claim Keycloak puts in access tokens. Leave blank if your client doesn't add an Audience mapper.",
  "settings.field.authorization_url": "Authorization URL",
  "settings.field.authorization_url.hint":
    "Usually issuer + /protocol/openid-connect/auth.",
  "settings.field.token_url": "Token URL",
  "settings.field.token_url.hint":
    "Usually issuer + /protocol/openid-connect/token.",
  "settings.field.client_id": "Client ID",
  "settings.field.client_secret": "Client secret",
  "settings.field.client_secret.hint":
    "Sensitive. Shown as ******** when configured — type a new value to replace, or leave empty to clear.",
  "settings.field.redirect_url": "Redirect URL",
  "settings.field.redirect_url.hint":
    "One of the Keycloak client's valid-redirect-uris, e.g. https://iai.example.com/v1/auth/oidc-callback.",
  "settings.field.auth_host": "Auth host",
  "settings.field.auth_host.hint":
    "The hostname of the oauth2-proxy entry, e.g. auth.iai.example.com.",
  "settings.field.cookie_secret": "Cookie secret",
  "settings.field.cookie_secret.hint":
    "32 bytes base64. Generate with: openssl rand -base64 32.",
  "settings.field.cookie_domain": "Cookie domain",
  "settings.field.cookie_domain.hint":
    "Domain user apps live under, so the proxy cookie is shared across subdomains, e.g. .iai.example.com.",
  "settings.field.brand_name": "SSO brand name",
  "settings.field.brand_name.hint":
    "Shown on the login button, user list, access-control labels, etc. e.g. \"Longbridge Account\", \"Acme SSO\". Leave blank to show \"SSO\". Hot-reloaded.",
  "settings.placeholder.secret.set": "set · type to replace",
  "settings.placeholder.secret.empty": "not set",
  "settings.badge.env": "from env",
  "settings.badge.db": "DB override",
  "settings.badge.updated": "updated {{when}}",
  "settings.button.save": "Save",
  "settings.button.save.busy": "Saving…",
  "settings.button.reset": "Discard unsaved changes",
  "settings.toast.saved": "Saved. {{count}} field(s) updated.",
  "settings.toast.failed": "Save failed: {{error}}",

  "presets.section.title": "Access presets",
  "presets.section.desc":
    "Globally-maintained IP allow-lists that projects pick from. Editing a preset re-syncs every project pointing at it — no redeploys needed.",
  "presets.col.label": "Name",
  "presets.col.cidrs": "IP / CIDR",
  "presets.col.system": "Type",
  "presets.col.actions": "Actions",
  "presets.cidrs.count": "{{n}} entries",
  "presets.system.tag": "system",
  "presets.system.hint": "System presets (public / internal) can't be deleted, but their CIDR list can be edited.",
  "presets.empty.title": "No custom presets yet",
  "presets.empty.desc": "Hit “Add preset” to define one for your team.",
  "presets.add.button": "Add preset",
  "presets.edit.title": "Edit “{{label}}”",
  "presets.new.title": "New preset",
  "presets.field.name": "Slug",
  "presets.field.name.hint": "Used as the API key. Lowercase letters/digits/hyphens, 3-30 chars. Immutable after creation.",
  "presets.field.label": "Display name",
  "presets.field.label.hint": "Shown in the project's dropdown, e.g. “Internal access”.",
  "presets.field.description": "Description (optional)",
  "presets.field.cidrs": "IP / CIDR list",
  "presets.field.cidrs.hint": "One per line — e.g. 10.0.0.0/8 or a bare IP. Empty = open to everyone.",
  "presets.button.save": "Save",
  "presets.button.save.busy": "Saving…",
  "presets.button.delete": "Delete",
  "presets.button.cancel": "Cancel",
  "presets.confirm.delete": "Delete preset “{{label}}”? Projects currently using it fall back to custom mode.",
  "presets.toast.saved": "Saved",
  "presets.toast.deleted": "Deleted",
};

const DICTS: Record<Locale, Dict> = { zh, en };

type I18nCtx = {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (key: string, vars?: Record<string, string | number>) => string;
};

const Ctx = createContext<I18nCtx | null>(null);

export function I18nProvider({
  children,
  globalVars,
}: {
  children: React.ReactNode;
  // globalVars are auto-merged into every t() call's vars. Used to inject
  // {{brand}} without threading it through every t() call site. Per-call
  // vars (passed to t() as the second arg) win on key collision so a
  // template can still override the global when it needs to.
  globalVars?: Record<string, string | number>;
}) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    if (typeof window === "undefined") return "zh";
    const stored = window.localStorage.getItem(STORAGE_KEY) as Locale | null;
    return stored === "en" || stored === "zh" ? stored : "zh";
  });

  useEffect(() => {
    document.documentElement.lang = locale === "zh" ? "zh-Hans" : "en";
  }, [locale]);

  const setLocale = useCallback((l: Locale) => {
    window.localStorage.setItem(STORAGE_KEY, l);
    setLocaleState(l);
  }, []);

  const t = useCallback(
    (key: string, vars?: Record<string, string | number>) => {
      const dict = DICTS[locale];
      let s = dict[key];
      if (s == null) {
        // Fall back to English, then to the key itself — never blank.
        s = DICTS.en[key] ?? key;
      }
      const merged = { ...(globalVars ?? {}), ...(vars ?? {}) };
      for (const k of Object.keys(merged)) {
        s = s.replace(new RegExp(`\\{\\{${k}\\}\\}`, "g"), String(merged[k]));
      }
      return s;
    },
    [locale, globalVars],
  );

  const value = useMemo(() => ({ locale, setLocale, t }), [locale, setLocale, t]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18nCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useI18n called outside I18nProvider");
  return ctx;
}
