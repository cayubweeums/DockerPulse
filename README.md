# DockerPulse

> **Multi-Server Docker & Compose Fleet Manager with Native Host Directory Support**

DockerPulse is a high-performance, lightweight fleet management dashboard for Docker environments. It was engineered specifically to solve Portainer's limitations around custom host directory structures (like `~/docker/<stack>/docker-compose.yml`), making multi-server Docker ops effortless without needing to constantly SSH into your Linux nodes.

---

## 🌟 Why DockerPulse?

- **Native Host Directory Tracking**: Portainer forces stacks into its internal storage. DockerPulse directly monitors and respects your server's native directories (e.g. `~/docker/nextcloud/docker-compose.yml`).
- **1-Click Push-Button Updates**: Registry digest comparison detects image updates remotely. Click **"Pull & Up"** to execute `docker compose pull && docker compose up -d` with a real-time streaming terminal drawer.
- **In-Browser Compose & .env Editor**: Edit `docker-compose.yml` and `.env` in the browser with **automatic revision snapshots** prior to every save—enabling 1-click rollback if a configuration breaks.
- **Tri-Mode Connection Drivers**:
  1. **DockerPulse Agent**: Minimal agent container on remote nodes communicating over secure, persistent WebSockets.
  2. **Direct SSH**: Zero-install management using OpenSSH keypairs.
  3. **Docker Socket / TLS**: Direct TCP or unix socket connectivity.
- **Live Observability**: Live log streaming with follow (`-f`), regex search, tail limits, and interactive container terminal PTY shell (`xterm.js`).
- **Network & Storage Ops**: Inspect IPAM subnets and gateways, attach/detach containers, and run safe Docker prune wizards.
- **Ultra-Lightweight**: Built as a single Go binary with embedded SQLite and React UI. Uses <40MB of RAM.

---

## 🚀 Quick Start

### 1. Run DockerPulse Central Server

#### Step A: Configure Environment & Secrets
Create a `.env` file next to your `docker-compose.yml`:

```bash
# Generate secure random tokens for your secrets:
openssl rand -hex 32
```

Create `.env`:
```ini
# Port to expose on the host machine (change to 8081 if 8080 is in use)
PORT=8080

# Secret key used to sign and verify JWT authentication tokens (Required)
JWT_SECRET=replace_with_output_from_openssl_rand_hex_32

# Shared secret token used by remote DockerPulse agents to join the fleet (Required)
AGENT_SECRET=replace_with_output_from_openssl_rand_hex_32

# Optional: Reverse Proxy SSO headers (Authelia, Authentik, Cloudflare Access)
# PROXY_AUTH_HEADER=Remote-User
# PROXY_EMAIL_HEADER=Remote-Email
```

#### Step B: Run with Docker Compose

```yaml
services:
  dockerpulse:
    image: ghcr.io/farmers00/dockerpulse:latest
    container_name: dockerpulse
    restart: unless-stopped
    ports:
      - "${PORT:-8080}:8080"
    volumes:
      # Data directory for SQLite database and state
      - ./data:/data
      # Mount local Docker socket to manage this host directly
      - /var/run/docker.sock:/var/run/docker.sock
      # Mount your host's docker compose directory (e.g. ~/docker)
      - ${HOME}/docker:/root/docker
    environment:
      - DATA_DIR=/data
      - JWT_SECRET=${JWT_SECRET:-change_me_to_a_secure_random_string_in_production}
      - AGENT_SECRET=${AGENT_SECRET:-dockerpulse_agent_shared_join_token_2026}
      # Reverse proxy SSO headers (Authelia, Authentik, Cloudflare Access)
      - PROXY_AUTH_HEADER=${PROXY_AUTH_HEADER:-Remote-User}
      - PROXY_EMAIL_HEADER=${PROXY_EMAIL_HEADER:-Remote-Email}
```

Start the container:
```bash
docker compose up -d
```
Then open `http://<your-server-ip>:8080` (or your configured `PORT`) to complete the first-time admin setup wizard.

---

### 2. Connect Remote Hosts

#### Option A: 1-Line Command (Fastest &bull; Container-based)
Log into your remote Linux host and run:

```bash
curl -fsSL "http://<manager-ip>:8080/install-agent.sh?id=node-1&token=<your-agent-secret>" | bash
```

This single command automatically:
1. Creates `~/docker/dockerpulse-agent/docker-compose.yml`.
2. Launches the `dockerpulse-agent` container using `ghcr.io/farmers00/dockerpulse:latest`.
3. Connects back to your manager over WebSocket and activates the node in your dashboard.

#### Option B: Manual Docker Compose
On your remote Linux host, deploy `docker-compose.yml` into `~/docker/dockerpulse-agent/`:

```yaml
services:
  dockerpulse-agent:
    image: ghcr.io/farmers00/dockerpulse:latest
    container_name: dockerpulse-agent
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ${HOME}/docker:/root/docker
    command: >
      dockerpulse agent
      --server ws://<manager-ip>:8080/ws/agent
      --token <your-agent-secret>
      --host-id node-1
      --base-dir /root/docker
```

Start with:
```bash
docker compose up -d
```

#### Option C: Direct SSH (Zero-Install)
In the DockerPulse web dashboard:
1. Click **Add Server** -> **Direct SSH**.
2. Enter the host IP, SSH port, user, and paste your SSH Private Key.
3. DockerPulse will immediately connect and scan `~/docker` for stacks.

---

## 🛠️ Project Architecture

```
docker_mgr/
├── backend/
│   ├── cmd/dockerpulse/main.go      # Single CLI binary: server or agent
│   ├── internal/
│   │   ├── api/                     # REST, WebSockets, streaming handlers
│   │   ├── auth/                    # JWT, bcrypt, reverse proxy header SSO
│   │   ├── config/                  # Configuration loader
│   │   ├── database/                # SQLite embedded repository & models
│   │   ├── driver/                  # HostDriver interface (Agent, SSH, Socket)
│   │   ├── scanner/                 # Filesystem stack discovery (~/docker)
│   │   ├── updater/                 # Registry manifest digest comparison engine
│   │   └── agent/                   # Agent node worker implementation
├── frontend/
│   ├── src/
│   │   ├── api/client.ts            # Typed REST & WebSocket client
│   │   ├── components/
│   │   │   ├── TerminalModal.tsx    # xterm.js PTY shell
│   │   │   ├── LiveLogsModal.tsx    # Streaming logs with follow (-f)
│   │   │   ├── ComposeEditorModal.tsx # In-browser editor & revision rollback
│   │   │   ├── UpdateModal.tsx      # Push-button update stream drawer
│   │   │   ├── NetworksModal.tsx    # IPAM inspection & network controls
│   │   │   ├── StorageModal.tsx     # Disk usage & prune wizard
│   │   │   └── AddHostModal.tsx     # Host onboarding wizard
│   │   └── App.tsx                  # Fleet overview dashboard
├── deploy/
│   ├── docker-compose.yml           # Central manager compose
│   ├── docker-compose.agent.yml     # Remote node agent compose
│   └── Dockerfile                   # Multi-stage production build
└── README.md
```

---

## 🔒 Security & SSO

- **Local Admin Setup**: First-run setup wizard creates the primary administrator account using bcrypt-hashed passwords and signed JWT tokens.
- **Reverse Proxy Header Authentication**: Compatible out of the box with Authelia, Authentik, and Cloudflare Access (`Remote-User` / `Remote-Email`).
- **No-Root Required for SSH**: Use any user that is in the remote host's `docker` group.

---

## 📄 License
MIT License
