# Griffino

A plugin orchestration platform that manages containerized plugins with message-driven communication.

Griffino runs plugins as Docker containers, routes messages between them via RabbitMQ, and provides a web API for management.

## Features

- **Plugin lifecycle management** — install, start, stop, and uninstall plugins via CLI or API
- **Manifest-based plugin system** — plugins declare capabilities, permissions, and configuration through a standard manifest format
- **Message routing** — automatic RabbitMQ broker provisioning with per-plugin isolation
- **Container orchestration** — Docker-based plugin execution with network and resource management
- **Task scheduling** — built-in scheduler for recurring plugin tasks
- **Multi-user auth** — session-based authentication with role management
- **Dev mode** — local plugin development workflow with hot-reload support
- **i18n** — English and Chinese language support

## Requirements

- Go 1.25+
- Docker
- RabbitMQ

## Quick Start

**Build:**

```bash
go build -o griffino ./cmd/griffino
```

**Configure:**

Create `~/.griffino/config.yaml`:

```yaml
rabbitmq:
  host: localhost
  port: 5672
  managementPort: 15672
  adminUser: guest
  adminPassword: guest
```

**Run:**

```bash
# Start the daemon
griffino daemon

# Install and start a plugin
griffino dev install ./path/to/plugin
griffino dev start <plugin-id>
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `griffino daemon` | Start the Griffino daemon |
| `griffino dev install <path>` | Install a plugin from local path |
| `griffino dev start <id>` | Start an installed plugin |
| `griffino dev stop <id>` | Stop a running plugin |
| `griffino dev uninstall <id>` | Uninstall a plugin |
| `griffino admin reset-password` | Reset the admin password |

Use `--lang` flag to override language (e.g. `--lang zh_CN`).

## Plugin Manifest

Plugins are defined by a `plugin.manifest.json` file:

```json
{
  "griffinoPluginManifestVersion": "1",
  "id": "my-plugin",
  "pluginVersion": "0.1.0",
  "name": { "default": "My Plugin" },
  "description": { "default": "A sample plugin" },
  "capabilities": [],
  "configurationFiles": {}
}
```

## Contributing

Contributions are welcome. Please read [CLA.md](CLA.md) before submitting a pull request.

## License

[Apache License 2.0](LICENSE)
