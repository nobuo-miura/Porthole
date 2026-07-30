# Porthole

[English](README.md) | [日本語](README.ja.md)

A lightweight, web-based connection testing tool that runs in Docker.
Quickly verify reachability and authentication of databases, caches, message queues, and arbitrary TCP/UDP ports — all from your browser.

> [!WARNING]
> **Porthole is a diagnostic tool, not a service. It has no authentication, and by design
> it will connect to any host and port you give it.** Anyone who can reach its port can
> probe your internal network and read the check history, including error messages.
> Never expose it to the internet or put it behind a public load balancer. Run it only as
> long as you need it, reach it over a port forward or on localhost, and shut it down
> afterwards. See [Running on AWS](#running-on-aws).

[![CI](https://github.com/nobuo-miura/Porthole/actions/workflows/ci.yml/badge.svg)](https://github.com/nobuo-miura/Porthole/actions/workflows/ci.yml) ![Go](https://img.shields.io/badge/Go-1.26.1-blue) ![Docker](https://img.shields.io/badge/Docker-ready-blue) ![License](https://img.shields.io/badge/license-MIT-green)

![Porthole screenshot](docs/screenshot.png)

## Features

- **TCP** — raw port connectivity check with latency
- **UDP** — sends a datagram and reports one of three outcomes: replied, definitively
  closed (ICMP port unreachable), or indeterminate. See [UDP results](#udp-results)
- **MySQL / MariaDB** — ping + version + authenticated user
- **PostgreSQL** — ping + version + authenticated user
- **SQL Server** — ping + version + authenticated user
- **MongoDB** — ping + authenticated user via `connectionStatus`
- **Redis** — `PING` command + password authentication
- **Elasticsearch** — `/_cluster/health` endpoint
- **RabbitMQ** — AMQP handshake
- **SMTP** — `EHLO` handshake (no email sent)
- **SSL/TLS** — configurable per protocol (`disable` / `require` / `skip-verify` / `verify-ca` / `verify-full`)
- **Batch mode** — paste a list of `host:port` entries and test them all concurrently
- **History** — last 50 checks stored in memory

## Quick Start

### Run from Docker Hub (recommended)

```bash
docker run -p 8080:8080 nobuomiura/porthole:latest
```

### Build and run with Docker Compose

```bash
docker compose up --build
```

Open **http://localhost:8080** in your browser.

### Changing the port

```bash
PORT=9090 docker compose up --build
```

### Testing services on the Docker host

Uncomment the `extra_hosts` block in `docker-compose.yml`:

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

Then use `host.docker.internal` as the hostname in the UI.

## Configuration

| Env var | Flag | Default | Description |
|---|---|---|---|
| `PORT` | `--port` | `8080` | HTTP listen port |
| `HISTORY_SIZE` | `--history` | `50` | Number of checks to keep in memory. `0` disables the history. |
| `PORTHOLE_PASSWORD` | — | — | Password for `porthole check` (see [CLI](#cli)) |

`--version` prints the build version and exits.

## CLI

`porthole check` runs checks without starting the web server and exits with a status code.
Useful where you only have a shell (ECS Exec, `kubectl exec`) and from CI/CD.

```bash
porthole check --type tcp --host db.internal --port 5432
porthole check --type postgres --host db.internal --port 5432 --username app --database app
```

Multiple checks, machine-readable output:

```bash
echo '[{"type":"tcp","host":"a","port":80},{"type":"tcp","host":"b","port":443}]' \
  | porthole check --stdin --json
```

| Exit code | Meaning |
|---|---|
| `0` | Reachability confirmed for every check |
| `1` | At least one check definitively failed |
| `2` | No failures, but at least one result is indeterminate |
| `3` | Bad arguments or input |

Prefer `PORTHOLE_PASSWORD` over `--password`: arguments are visible to other processes on
the host. Run `porthole check --help` for the full flag list.

## Running on AWS

Task definitions and step-by-step instructions live in
[deploy/ecs/](deploy/ecs/README.md). In short:

- **Sidecar in the app's own task** — most accurate. Containers in an `awsvpc` task share
  one ENI, so the sidecar sees the app's exact subnet, security groups, route table and
  DNS resolver, and can even probe the app's listener over `localhost`.
- **Standalone one-off task** — easier, but you must attach **the app's own security
  group** (not a copy: rules usually name another security group as the source) and run in
  a subnet the app actually uses.

Reach the UI without exposing anything, using SSM port forwarding through ECS Exec. For
ECS targets AWS documents the `...ToRemoteHost` document with a `host` parameter:

```bash
aws ssm start-session \
  --target ecs:<CLUSTER>_<TASK_ID>_<RUNTIME_ID> \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters '{"host":["127.0.0.1"],"portNumber":["8080"],"localPortNumber":["8080"]}'
```

Use `"portNumber":["8081"]` for the sidecar, where 8080 belongs to the app. This requires
`--enable-execute-command` on the task, so the Porthole container cannot use
`readonlyRootFilesystem` — ECS Exec's SSM agent needs a writable filesystem. See
[deploy/ecs/](deploy/ecs/README.md) for details.

## Running locally (without Docker)

```bash
go run .
# or
make run
```

Requires Go 1.26.1+.

## Development

```bash
make check      # the Go-side CI gates: gofmt, go mod tidy, build, vet, lint, tests
make check-all  # the above plus the Tailwind CSS staleness check (needs Node)
make test       # go test -race ./...
make cover      # test with a coverage summary
make tailwind   # regenerate web/tailwind.css
```

`make check` is not the whole of CI: the Docker build and its smoke test run only in the
[CI workflow](.github/workflows/ci.yml). Everything else CI checks has a Make target, and
CI invokes those targets directly so the two cannot drift.

Note that `make fmt` rewrites files while `make fmt-check` only reports — `check` uses the
latter, so it fails on unformatted code instead of quietly fixing it.

`make lint` requires [golangci-lint](https://golangci-lint.run/docs/welcome/install/local/).

### Frontend CSS

The UI ships a pre-built `web/tailwind.css`, so neither `go build` nor the Docker build
needs Node, and the UI renders in private subnets with no internet egress. If you edit
`tailwind/input.css` or any markup under `web/`, run `make tailwind` (needs Node) and
commit the regenerated CSS — CI fails if it is stale.

Custom rules live in [tailwind/input.css](tailwind/input.css) and must stay **above**
`@tailwind utilities`, otherwise utilities like `hidden` lose to the component rules.

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/check` | Run a single connection check |
| `POST` | `/api/check/batch` | Run multiple TCP checks concurrently |
| `GET`  | `/api/history` | Retrieve last N check results |
| `GET`  | `/healthz` | Health probe; reports the build version |

### Example

```bash
curl -X POST http://localhost:8080/api/check \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "postgres",
    "host": "db.example.com",
    "port": 5432,
    "username": "postgres",
    "password": "secret",
    "database": "myapp",
    "ssl_mode": "require",
    "timeout_sec": 5
  }'
```

```json
{
  "success": true,
  "outcome": "ok",
  "type": "postgres",
  "host": "db.example.com",
  "port": 5432,
  "latency_ms": 12,
  "detail": "PostgreSQL 16.2 on x86_64 | authenticated as postgres",
  "checked_at": "2026-03-21T10:00:00Z"
}
```

### Supported `type` values

`tcp`, `udp`, `mysql`, `mariadb`, `postgres`, `postgresql`, `mongodb`, `redis`, `elasticsearch`, `rabbitmq`, `smtp`, `sqlserver`, `mssql`

### SSL modes by protocol

Four behaviours are available, named the same way across protocols:

| Value | TLS | Certificate chain | Hostname |
|---|---|---|---|
| `disable`, empty | no | — | — |
| `skip-verify` | yes | not verified | not verified |
| `verify-ca` | yes | verified | **not** verified |
| `verify-full` | yes | verified | verified |

| Protocol | `disable` | `skip-verify` | `verify-ca` | `verify-full` | `require` |
|---|:-:|:-:|:-:|:-:|:-:|
| MySQL / MariaDB | ✅ | ✅ | ✅ | ✅ | = `verify-full` |
| PostgreSQL | ✅ | ✅ | ✅ | ✅ | = `skip-verify` |
| MongoDB | ✅ | ✅ | ✅ | ✅ | = `verify-full` |
| Redis | ✅ | ✅ | ✅ | ✅ | = `verify-full` |
| SQL Server | ✅ | ✅ | ❌ | ✅ | = `verify-full` |
| Elasticsearch | ✅ (`http`) | ✅ | ✅ | ✅ | = `verify-full` |

> [!IMPORTANT]
> **An unsupported or misspelled `ssl_mode` is rejected with an error, never silently
> downgraded.** Asking for verification and getting an unencrypted connection instead is
> the worst possible failure mode for this tool, so `verify-ca` on SQL Server — or a typo
> like `verify_full` — fails loudly instead of connecting in plaintext.

> [!NOTE]
> `require` means different things per protocol, and that is not something Porthole
> invents — it follows each driver. Everywhere except PostgreSQL it verifies the
> certificate, so it is equivalent to `verify-full`. For **PostgreSQL**, `lib/pq` implements
> libpq semantics where `sslmode=require` encrypts *without* verifying, making it
> equivalent to `skip-verify`.
>
> To avoid the ambiguity entirely, prefer the explicit names: `verify-full` when you want
> verification, `skip-verify` when you deliberately want none.

## Outcomes

Every result carries an `outcome` alongside `success`:

| `outcome` | `success` | Meaning |
|---|---|---|
| `ok` | `true` | Positive evidence of reachability (and authentication, where applicable) |
| `failed` | `false` | Definitive failure — connection refused, authentication rejected, … |
| `indeterminate` | `false` | No evidence either way |

`success` is exactly `outcome == "ok"`. An indeterminate result is reported as
`success: false` because there is no evidence of reachability — but it is not a failure,
and the UI shows it as **UNKNOWN** rather than red.

### UDP results

UDP has no handshake, so opening a socket proves nothing — only that the hostname
resolved. Porthole sends one datagram and classifies the response:

| Result | Condition |
|---|---|
| `ok` | The peer sent a reply. Only protocols that answer (DNS, NTP, SNMP, …) do this |
| `failed` | An ICMP port-unreachable came back — the port is definitively closed |
| `indeterminate` | Nothing came back within the timeout |

Firewalls that **drop** rather than reject — including AWS security groups — produce no
ICMP, so a blocked port is indistinguishable from an open port that does not answer. In
those environments treat `indeterminate` as "no information" and prefer a TCP check where
one exists.

## License

MIT
