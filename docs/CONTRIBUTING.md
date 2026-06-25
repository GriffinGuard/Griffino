# Contributing to Griffino

Thanks for your interest in contributing! This guide covers the essentials.

## Before you start

- Griffino is **single-machine** by design for the foreseeable major versions.
  LAN / multi-node deployment, HA, and clustering are intentionally out of scope
  for now, so please keep proposals within the current scope.
- All contributors must agree to the [Contributor License Agreement](CLA.md) before a
  pull request can be merged.

## Development setup

```bash
git clone https://github.com/GriffinGuard/Griffino.git
cd Griffino
./scripts/install.sh --build-only   # produces ./griffino
```

You'll need **Go 1.25+** and a running **Docker** engine (Docker Desktop, colima, or
podman) for the daemon and integration paths.

## Building and testing

```bash
go build ./...
go vet ./...
go test ./... -race
```

Unit tests use an in-memory Redis (miniredis) and a temporary BoltDB, so they do not
require Docker or RabbitMQ.

## Code style & checks

- **Formatting** — run `gofmt -w` on any files you touch. `gofmt` is a hard gate in CI.
- **License headers** — every `.go` / `.toml` source file carries an Apache 2.0
  header. Add or fix headers before committing; CI rejects any source file that is
  missing one:
  ```bash
  go install github.com/google/addlicense@latest
  addlicense -c "GriffinGuard" -l apache -y 2025 cmd/ internal/ pkg/ tests/
  ```
- **Linting** — `golangci-lint run` (currently advisory in CI).
- **API docs** — if you change HTTP handlers, regenerate the OpenAPI spec; CI fails if
  the checked-in `docs/api/` spec drifts from the handler annotations:
  ```bash
  go install github.com/swaggo/swag/cmd/swag@v1.16.4
  swag init -g internal/api/doc.go -o ./docs/api --parseDependency --parseInternal
  ```

CI runs build, `go vet`, race-enabled tests, license-header checks, `gofmt`, and
OpenAPI-drift checks on every pull request.

## Plugin interface contracts

Capabilities are wired by typed **ports**; the contracts live in two places:

- **Standard interfaces** are defined in the separate
  [Griffino-Schemas](https://github.com/GriffinGuard/Griffino-Schemas) repository. The
  daemon embeds a port-spec snapshot of the standard set at
  `internal/taskscheduler/schemaseed/standard.json`, loaded into the schema store at
  startup (`SeedStandardSchemas`). When you add or change a standard interface in
  Griffino-Schemas, update this seed so blueprint port validation resolves it.
- **Inline custom interfaces** are declared by a plugin in its manifest
  (`capability.interfaceSpec`) and need no seed.

Port `type` values must come from the canonical vocabulary in `pkg/manifest/ports.go`
(`IsValidPortType`); `internal/taskscheduler/portcheck.go` decides edge compatibility.
The router treats two providers of a capability as interchangeable only when their
`interfaceRef` **major** versions match (`internal/router/router.go`).

Two manifest fields are part of the plugin-facing contract and must stay in sync with
the plugin SDKs (Griffino-Go, Griffino-Python): `capability.interfaceSpec` and the
top-level `emits` trigger declarations. Changing their shape is a breaking change —
update the SDK generators (and the Griffino-Schemas docs) in the same change.

## Commit & PR conventions

- Use [Conventional Commits](https://www.conventionalcommits.org/) for commit and PR
  titles, e.g. `feat(api): ...`, `fix(plugin): ...`, `docs: ...`, `ci: ...`.
- Open a pull request against `main`. Keep changes focused; large features should land
  as their own branch + PR.
- Make sure CI is green and include a short note on how you verified your change.

## Reporting issues

Open a GitHub issue with steps to reproduce, expected vs. actual behavior, your OS,
and the Griffino + Docker versions.
