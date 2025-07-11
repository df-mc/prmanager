# prmanager

Manages temporary Minecraft: Bedrock Edition test servers for pull requests to [df-mc/dragonfly](https://github.com/df-mc/dragonfly).

It allows clients to seamlessly connect to servers tied to specific pull requests using subdomain-based addressing (e.g. `123.df-mc.dev`). These servers are only started when needed and shut down automatically after inactivity, saving system resources.

---

## Features

- 🔧 **PR-aware server management** — each pull request gets its own isolated environment
- 🐳 **Docker-powered** — builds and runs containers per PR
- ⚡ **Lazy start** — servers are only launched when a player connects
- ⏲️ **Auto shutdown** — containers are stopped after 1 hour of inactivity
- 🌍 **Subdomain-based routing** — e.g. `123.df-mc.dev` connects to PR 123
- 🔒 **Optional API key protection** for deploy/remove actions

---

## How It Works

1. A pull request is opened → a CI job uploads the compiled binary.
2. The binary is stored and a corresponding Docker image is built.
3. When a Minecraft: Bedrock Edition client connects to a subdomain like `123.df-mc.dev`:
   - If the server is not running, it is started using the Docker image for PR 123 on a randomly allocated port.
   - The port is then retrieved from the running container and the client is redirected to it.
   - Clients can also connect to `df-mc.dev` (or `188.166.78.44`) as well as `plots.df-mc.dev` for official servers.
4. Servers automatically shut down after 1 hour of inactivity.
5. When a pull request is closed or merged, a cleanup job removes the associated image and files.

---

## Requirements

- Go (1.24+)
- Docker installed and running on the host
- DNS wildcard (e.g. `*.df-mc.dev`) pointing to your server
- The provided `Dockerfile` (included in this repository) must be in the same working directory as `prmanager`
- Write access to the current directory (for creating per-PR folders)

---

## API

If the `API_KEY` environment variable is set, all HTTP requests must include the following header:

```
X-API-Key: your_key_here
```

### `POST /pullrequest`

**Description:** Uploads a binary and builds a Docker image for the PR.

**Form Fields:**

- `pr`: PR number (e.g. `123`)
- `binary`: Compiled Dragonfly server binary (e.g. `dragonfly`)

**Example:**

```bash
curl -X POST https://df-mc.dev/pullrequest \
  -H "X-API-Key: your_key" \
  -F "pr=123" \
  -F "binary=@dragonfly"
```

---

### `DELETE /pullrequest/{pr}`

**Description:** Deletes the Docker image and removes all associated files for the given PR.

**Example:**

```bash
curl -X DELETE https://df-mc.dev/pullrequest/123 \
  -H "X-API-Key: your_key"
```

---

## Running

```bash
go build -o prmanager
./prmanager
```

Make sure your working directory contains:
- This repository's `Dockerfile`
- Write permissions to create `pr-<number>` folders and binaries

---

## Configuration

### Environment Variables

- `API_KEY` (optional): If set, HTTP endpoints will require the `X-API-Key` header.
