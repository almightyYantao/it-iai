<div align="center">

<img src="docs/images/logo.svg" alt="iai" width="120" />

# iai · 爱 AI

**Internal one-click deploy platform**

Tell Claude "deploy this", get an SSO-protected HTTPS URL in 1–3 minutes.<br/>
No Dockerfile. No K8s. No DNS dance.

[简体中文](README.md) · [English](README.en.md)

<br/>

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=white)
![K3s](https://img.shields.io/badge/K3s-DaemonSet%20Traefik-FFC61C?logo=k3s&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-336791?logo=postgresql&logoColor=white)
![Keycloak](https://img.shields.io/badge/Keycloak-OIDC-4D4D4D?logo=keycloak&logoColor=white)
![License](https://img.shields.io/badge/license-Internal-blue)

<br/>

<img src="docs/images/banner.svg" alt="iai banner" width="100%" />

</div>

---

## ✨ What it does

```bash
cd <project>          # any project directory
claude                # in Claude Code, just say
> deploy this
                      # 1–3 minutes later:
                      # 🚀 https://my-app.example.com
```

Turn internal tools / demos / AI agents from "runs only on my laptop" into "a URL my teammates can open", with:

- 🔐 **Enterprise SSO** — Keycloak / OIDC integration so outsiders can't reach the app
- 🌐 **HTTPS + wildcard cert** — TLS built in, auto-redirect 80 → 443
- 🎯 **IP allow-list** — globally-named presets (admin-curated) + per-project custom
- 🪪 **Vanity subdomain** — `my-app.example.com` instead of random chars
- 🗄️ **Auto-provisioned databases** — `postgres = true` in the manifest → platform creates a DB and injects `DATABASE_URL`; users don't have to file tickets
- 🪶 **SQLite never loses data** — a Litestream sidecar streams `/data/app.db` to S3 in real time; pod restarts / node migrations auto-restore from S3
- 🔑 **Encrypted env vars** — edit secrets in the admin UI (KEK-encrypted at rest), auto-injected into the pod; nothing in the user's git
- 🤝 **Collaborators** — invite teammates to co-maintain
- 📊 **Live logs** — SSE stream, build output line by line
- 🔁 **Self-healing** — failed pods auto-recover, DB state auto-reconciled with cluster

---

## 📚 Documentation

| Audience | Read this |
| :--- | :--- |
| Business users / first-timers | [📖 Usage Guide](docs/使用手册.md) — plain-language, 15 minutes |
| Engineers / want to understand internals | [🏗️ Architecture](docs/技术架构.md) — components, lifecycle, design decisions |
| SREs / backup / scaling | [🔧 Ops Runbook](docs/运维手册.md) — backups, externalising PG/MinIO/Registry, multi-platform-node HA |
| Ops / want to run it yourselves | [ECS Deployment section below](#-deploy-on-ecs) |
| Developers / want to hack on it | [Local Development section below](#-local-development) |

> The deeper docs (`docs/`) are currently Chinese-only. Translation PRs welcome.

---

## 🚀 Quick install (developer laptop)

Copy and paste the whole line into a terminal:

```bash
rm -rf ~/iai && git clone https://github.com/almightyYantao/it-iai.git ~/iai && bash ~/iai/skill/install.sh install
```

Idempotent — paste the same line to upgrade later.

See [the Usage Guide](docs/使用手册.md) for detailed walkthroughs.

---

## 📸 Screenshots

<table>
<tr>
<td width="50%">
<img src="docs/images/screenshot-overview.png" alt="Overview" />
<p align="center"><sub>Overview — cluster health at a glance</sub></p>
</td>
<td width="50%">
<img src="docs/images/screenshot-project.png" alt="Project detail" />
<p align="center"><sub>Project detail — pod status, access control, collaborators</sub></p>
</td>
</tr>
<tr>
<td width="50%">
<img src="docs/images/screenshot-deployment.png" alt="Deployment detail" />
<p align="center"><sub>Deployment — live build logs</sub></p>
</td>
<td width="50%">
<img src="docs/images/screenshot-settings.png" alt="Settings" />
<p align="center"><sub>Settings — Keycloak / access-preset hot reload</sub></p>
</td>
</tr>
</table>

<details>
<summary><b>🎬 Watch a deploy happen end-to-end (click to expand)</b></summary>

<img src="docs/images/skill-push.gif" alt="Skill push demo" width="100%" />

</details>

---

## 🏗️ Architecture

<img src="docs/images/architecture.svg" alt="Architecture diagram" width="100%" />

```
 Developer laptop      Platform node                       Worker nodes

 ┌──────────┐        ┌──────────┐  ┌──────────┐
 │ Claude   │ ─API─▶ │ control- │──┤   PG     │           ┌────────────┐
 │ Code +   │        │ plane    │  │  MinIO   │           │ K3s agent  │
 │ Skill    │        ├──────────┤  │ Registry │           │            │
 └──────────┘        │ build-   │  │  Redis   │           │ user pods  │
                     │ service  │  │ user-PG  │ ← auto-   │ (proj-xxx) │
                     └──────────┘  └──────────┘  prov'd   └────────────┘
                          │                                       ▲
                          │       K3s server + Traefik            │
                          │       (DaemonSet, hostNetwork)        │
                          └──────────►─────────────────────────────┘
                                          ↑
                                          │  HTTPS 443
                          ┌───────────────┴────────────────┐
                          │ *.example.com    user apps │
                          │ admin.example.com  admin   │
                          │ auth.example.com   SSO     │
                          └────────────────────────────────┘
```

Full lifecycle, inter-component protocols, and "why this and not that" decisions live in [Architecture](docs/技术架构.md).

---

## 🗄️ Data isolation & auto-provisioning

Declare `postgres = true` / `redis = true` / `s3 = true` in the manifest and the platform creates the backing resource on first deploy, encrypts the credentials, and injects them into the pod. Business users don't file tickets, don't pick passwords — and **every project gets a real isolated slice**, not shared creds with a "be nice" naming convention.

<img src="docs/images/data-isolation.svg" alt="Per-project data isolation" width="100%" />

| Service | Shared backbone | Per-project | Isolation |
| :--- | :--- | :--- | :--- |
| **PostgreSQL** | `user-postgres` container (separate from the control-plane DB) | Own database `proj_<slug>` + own role + random password | SQL: `GRANT` to own DB only — can't even `\c` siblings |
| **Redis** | Shared `redis` container (Redis 6 ACL) | ACL user `proj-<slug>` + key pattern `~proj-<slug>:*` + `-@dangerous` | ACL: writing a non-prefixed key returns `NOPERM` |
| **MinIO / S3** | Shared `minio` container (reuses platform storage) | Own bucket `proj-<slug>` + own IAM user + bucket-only policy | IAM: policy scoped — listing siblings returns 403 |
| **SQLite** | Shared MinIO as the Litestream replication target (no separate backbone) | emptyDir `/data` in the pod + Litestream sidecar + init container that restores from S3 | Each project's WAL lives in its own bucket; inherits the S3 IAM isolation |

Env vars injected into the pod:

```bash
DATABASE_URL             # postgres://proj_<slug>:****@<host>:5433/proj_<slug>
REDIS_URL                # redis://proj-<slug>:****@<host>:6379/0
REDIS_KEY_PREFIX         # proj-<slug>:
S3_ENDPOINT              # <host>:9000
S3_REGION                # us-east-1
S3_ACCESS_KEY_ID         # proj-<slug>
S3_SECRET_ACCESS_KEY     # ****
S3_BUCKET                # proj-<slug>
S3_USE_SSL               # false
SQLITE_PATH              # /data/app.db    (only when needs.sqlite=true)
```

Values are KEK-encrypted at rest in the platform DB, decrypted at deploy time, and dropped into a K8s Secret — the pod just reads `os.environ["DATABASE_URL"]`.

On project deletion: PG database, Redis ACL user, and S3 user/policy are **dropped automatically**. The S3 bucket is **preserved** (so a misclick can't wipe years of objects) — admin confirms then `mc rb --force` manually.

---

## 🛠️ Deploy on ECS

Three steps: install the platform node → install workers → wire TLS + SSO. Every script is idempotent — safe to re-run.

### 1. Platform node

```bash
git clone https://github.com/almightyYantao/it-iai.git /opt/it-iai
cd /opt/it-iai

sudo BASE_DOMAIN=example.com \
     deploy/install-platform.sh
```

Installs Docker + K3s server + the docker-compose stack (PG / MinIO / Registry / Redis / control-plane / build-service / web nginx) and prints both the **bootstrap Deploy Token** and the **worker join command**.

### 2. Worker nodes (one-off per machine)

Use the join command from the platform output:

```bash
sudo K3S_URL=https://<platform-ip>:6443 \
     K3S_TOKEN=<token>                  \
     PLATFORM_IP=<platform-ip>          \
     REGISTRY_PULL_HOST=<platform-ip>:5001 \
     deploy/install-worker.sh
```

Health checks:

```bash
sudo /opt/it-iai/deploy/check-k3s.sh    # on platform
sudo bash deploy/check-agent.sh         # on each worker
```

### 3. SSO + TLS

Fill in Keycloak OIDC config on the Web Settings page → Save (hot-reloaded, no restart). Then pick **one** TLS mode:

**A. Wildcard cert (DNS-01 / bring-your-own)** — one cert covers every app subdomain.

```bash
# Drop wildcard cert into /opt/it-iai/tls/ as *.crt + *.key
sudo /opt/it-iai/deploy/install-tls.sh
```

**B. Per-project on-demand (HTTP-01)** — install cert-manager once, project owners
flip "Enable HTTPS" in Project Settings, each app gets its own Let's Encrypt cert
that renews automatically. No DNS API required.

```bash
# First-time install of cert-manager + letsencrypt-prod / letsencrypt-staging issuers
sudo ACME_EMAIL=you@example.com /opt/it-iai/deploy/install-cert-manager.sh
```

> ⚠️ HTTP-01 needs :80 reachable from the public internet and every app hostname
> resolving to the platform IP. Let's Encrypt does not support wildcards over
> HTTP-01 — use mode A or a DNS-01 setup if you need `*.<domain>` certs.

Then in either mode:

```bash
# Install oauth2-proxy + Traefik ForwardAuth middlewares
sudo /opt/it-iai/deploy/install-oauth2-proxy.sh

# Expose admin UI on admin.<base-domain>
sudo /opt/it-iai/deploy/install-admin-ui-tls.sh
```

DNS: point `*.<BASE_DOMAIN>`, `auth.<BASE_DOMAIN>`, and `admin.<BASE_DOMAIN>` to the platform node IP.

### Upgrade

```bash
cd /opt/it-iai
sudo git pull

# --build rebuilds every service with a build context
# (control-plane / build-service / web). Building only control-plane
# would miss build-service / frontend updates.
sudo docker compose up -d --build
```

Control-plane runs new migrations on boot. Pods on workers are unaffected.

### Network ports

<details>
<summary>Expand</summary>

Platform ↔ workers (**private subnet**):

| Port  | Proto | Why |
| :--- | :--- | :--- |
| 6443  | tcp | K3s API |
| 10250 | tcp | kubelet |
| 8472  | udp | flannel VXLAN |
| 5001  | tcp | image registry (workers pull) |

External (**platform node only**, but Traefik runs on every node):

| Port | Proto | Why |
| :--- | :--- | :--- |
| 80  | tcp | Traefik HTTP (auto 302 → HTTPS) |
| 443 | tcp | Traefik HTTPS (wildcard cert) |

</details>

### Uninstall

```bash
sudo deploy/uninstall.sh
```

---

## 🔧 Backup & scaling

The second-most important thing to do once it's running. Three steps, sorted by value-per-effort:

### 1. Backup (**do this**)

Bare minimum: daily `pg_dump` + `.env` + `tls/` copied off-host. One cron script, full version in [Ops Runbook §1](docs/运维手册.md#1-%E5%A4%87%E4%BB%BD).

⚠️ The `CP_KEK_BASE64` in `.env` is the root key for every encrypted field in the DB (tokens, secrets). **Lose it and every Deploy Token is unrecoverable** — store a copy somewhere besides the same disk (password manager, encrypted vault).

### 2. Externalize state (**strongly recommended**)

Before the platform node's disk fills up or you want HA, move the three stateful components out:

| Component | Move to | Effort |
|---|---|---|
| Postgres | Aliyun RDS PostgreSQL, or a dedicated ECS | Edit `CP_DATABASE_URL` in `.env` |
| MinIO | Aliyun OSS (S3-compatible) | Edit the `CP_S3_*` block |
| Registry | Aliyun ACR | Edit `CP_REGISTRY_HOST*` + each worker's `registries.yaml` |

Components already talk to each other over the network — externalising is an env edit, **not a code change**. Step-by-step migration + verification checklists: [Ops Runbook §2-§4](docs/运维手册.md#2-pg-%E5%A4%96%E7%BD%AE%E6%90%AC%E5%88%B0-rds--%E7%8B%AC%E7%AB%8B-ecs).

After this the platform node becomes **stateless** — if its disk dies, install a fresh ECS, `git pull`, `docker compose up -d --build`, drop `.env` + `tls/` back in, fully recovered.

### 3. Multi-platform node + HA (optional)

Only after externalising state. Two or three platform nodes behind an SLB, K3s server upgraded to a 3-node etcd cluster. See [Ops Runbook §5](docs/运维手册.md#5-%E5%A4%9A%E5%B9%B3%E5%8F%B0%E8%8A%82%E7%82%B9--ha).

> A single platform node holds up to ~20-person / ~50-project teams. Get §1 §2 solid before reaching for HA.

---

## 💻 Local development

For hacking on the Go / TypeScript code. Starts a k3d cluster + the docker-compose stack on your machine:

```bash
make dev
```

After it finishes:

| URL | What |
| :--- | :--- |
| `http://localhost:5173` | Admin UI |
| `http://localhost:8080` | Control Plane REST API |
| `http://localhost:9001` | MinIO console |
| `http://localhost:5001` | Local image registry |

Push a sample app:

```bash
export VIBEDEPLOY_TOKEN=<token>
export VIBEDEPLOY_API=http://localhost:8080

cd examples/hello-node
bash ../../skill/scripts/push.sh
```

Clean up: `make destroy`

---

## 📂 Repo layout

```
.
├── cmd/control-plane/       Go server entry
├── cmd/build-service/       Build worker
├── internal/
│   ├── api/                 HTTP handlers + middleware
│   ├── auth/                JWT + KEK + Deploy Token
│   ├── config/              env + runtime config (hot reload)
│   ├── k8sdriver/           client-go wrapper + Traefik Middleware CR
│   ├── model/               domain types
│   └── store/               pgxpool + per-table CRUD
├── migrations/              0001-0005 sequential SQL
├── deploy/                  ECS multi-node installers + audit scripts
├── skill/                   Claude Code Skill
├── web/                     Vite + React + Tailwind admin UI
├── docs/
│   ├── 技术架构.md          architecture (engineer-facing)
│   ├── 使用手册.md          usage guide (business-facing)
│   └── images/              README assets (drop screenshots here)
├── examples/                end-to-end smoke samples
└── docker-compose.yml       platform-node service stack
```

---

## 🤝 Contributing

Commit message style: `feat(scope): ...` / `fix(scope): ...` / `docs: ...`. See `git log --oneline` for examples.

Before pushing: `go build ./...` and `cd web && npx tsc --noEmit` should both pass.

---

## ⭐ Star History

<a href="https://star-history.com/#almightyYantao/it-iai&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=almightyYantao/it-iai&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=almightyYantao/it-iai&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=almightyYantao/it-iai&type=Date" />
  </picture>
</a>

Before pushing: `go build ./...` and `cd web && npx tsc --noEmit` should both pass.
