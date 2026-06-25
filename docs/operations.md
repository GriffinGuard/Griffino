# Operating Griffino — reliability, security & availability

This guide covers how the daemon behaves operationally. It assumes the single-machine,
local-first scope that governs the rest of the project (no LAN/multi-node/HA).

## Reliability

### RabbitMQ auto-reconnect

The daemon's router holds the long-lived AMQP connection used for capability routing. If
RabbitMQ restarts or the connection drops, the router detects the close, **reconnects
automatically with exponential backoff** (1s → 30s cap), re-declares its exchanges, queues
and bindings, and resumes consuming — no daemon restart required. Requests in flight during
the gap time out and fall over through the normal provider-failover path. The reconnect
loop stops cleanly on daemon shutdown.

### Task watchdog & TTL

Blueprint executions are tracked by a watchdog (30s scan) that marks unresponsive plugin
nodes as timed out, so a stuck plugin never wedges a workflow. Task state lives in Redis
with a **24h TTL**; a single workflow that runs longer than 24h would lose its tracking
state. This is relevant only for unusually long-running workflows.

## Security

### At-rest encryption

Sensitive infrastructure credentials persisted by the daemon — RabbitMQ and Redis
passwords, the SMTP password, and each plugin's broker credentials — are encrypted with
**AES-256-GCM**. The 32-byte master key is generated with a CSPRNG and stored in a local
`secret.key` file (mode `0600`) in the Griffino data directory; ciphertext carries an
`enc:v1:` prefix so encrypted and legacy plaintext values migrate transparently.

- Protect the data directory with filesystem permissions; the key's security rests on them.
- **Back up `secret.key` together with the database** — without the key, encrypted fields
  cannot be decrypted.
- User-set plugin config secrets (`password`-typed fields) are additionally masked as
  `**masked**` when read back through the API, so a UI can round-trip them without exposure.

### Install trust model

By default the daemon installs plugins only from the **official plugin center**
([GriffinGuard/Griffino-Plugins](https://github.com/GriffinGuard/Griffino-Plugins)) and
enforces an **image allowlist** (`approved-images.json`): a normal plugin can only run
container images on the allowlist.

Developers install local plugins with **`griffino dev install <dir>`**, which skips the
allowlist. Such plugins are:

- marked `isDevPlugin` on their record (a UI can surface an "unverified" indicator), and
- **isolated from the Web console**: they must be started, stopped and reinstalled via
  `griffino dev start|stop|install`, never through the API/Web-UI.

This is the "safe by default, explicit developer opt-in" model. Plugin **signing** is a
planned post-v1 addition.

### Authentication

Account passwords are hashed with **bcrypt**. Repeated failed logins lock the account for a
configurable window and return HTTP 429, mitigating brute-force attacks.

### Sandboxing

Plugin containers run hardened: all Linux capabilities dropped (`CapDrop: ALL`, with a
minimal `CapAdd` for non-main services), **non-privileged**, `no-new-privileges`, optional
read-only volumes, on a dedicated per-plugin Docker network with isolated RabbitMQ/Redis
ACL credentials.

### Network exposure

The HTTP API binds to `127.0.0.1` by default. Exposing it externally is at your own risk
and is unsupported for the first major versions.

## Availability

### Container resource limits

Every plugin container runs with resource caps so a single plugin cannot exhaust the host.
Platform defaults: **512 MiB memory, 1.0 CPU, 512 PIDs**. A plugin may declare its own caps
per service in `plugin.boot.yml`; unset fields fall back to the defaults:

```yaml
services:
  main:
    image: ghcr.io/example/my-plugin:1.0.0
    resources:
      memory_mb: 1024
      cpus: 2.0
      pids_limit: 1024
```

### Restart policy

Plugin containers use the `unless-stopped` restart policy: a container that crashes is
restarted automatically by Docker.

### Health & metrics

- **Liveness** — `GET /health`.
- **Metrics** — Prometheus exposition at `GET /metrics`, covering HTTP, router, task, and
  plugin-health series.

## Backup & restore

Griffino's durable state lives in the data directory (default `~/.griffino`):

| File | Contents |
|---|---|
| `griffino.db` | BoltDB: installed plugins, blueprints, system state, cached schemas |
| `secret.key` | AES-256-GCM master key for at-rest encryption |
| `config.yaml` | Daemon configuration |

Redis holds only ephemeral, TTL'd state (tasks, sessions) and can be rebuilt.

- **Back up**: stop the daemon, then copy `griffino.db`, `secret.key`, and `config.yaml`.
- **Restore**: put the three files back and start the daemon.
- Always keep `secret.key` with the database — losing it makes encrypted fields
  unrecoverable.

## Upgrade & migration

- Encrypted fields carry an `enc:v1:` version prefix and migrate transparently; legacy
  plaintext is read as-is and re-encrypted on the next write.
- Standard interface contracts follow semantic versioning (see
  [Griffino-Schemas](https://github.com/GriffinGuard/Griffino-Schemas)); the daemon embeds a
  snapshot of the standard set per release.
- Back up the data directory before upgrading.
