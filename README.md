# cgram-server

Relay server for [cgram](https://github.com/isalikov/cgram) — an anonymous terminal messenger.

The server acts as a stateless relay: it stores encrypted blobs and delivers them to recipients. It never sees plaintext messages, does not log IP addresses, and requires no personal information for registration.

## Quick start

```sh
cp .env.example .env
make dev
```

This starts PostgreSQL in Docker and runs the server on port `8080`.

## Development

### Prerequisites

- [Go](https://go.dev/dl/) 1.21+
- [Docker](https://docs.docker.com/get-docker/)

### Commands

```
make help
```

| Command | Description |
|---------|-------------|
| `make build` | Build binary to `./bin/cgram-server` |
| `make run` | Run without building (`go run`) |
| `make dev` | Start database and run app |
| `make dev-up` | Start development database |
| `make dev-down` | Stop development database |
| `make test` | Run tests |
| `make clean` | Remove `./bin` directory |

## Configuration

Environment variables are loaded from `.env` file. See [.env.example](.env.example) for all available options.

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | Server listen address |
| `DB_HOST` | — | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | — | PostgreSQL user |
| `DB_PASSWORD` | — | PostgreSQL password |
| `DB_NAME` | — | PostgreSQL database name |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |

Alternatively, set `DATABASE_URL` as a full connection string.

## Docker

### Build image

```sh
docker build -t cgram-server .
```

### Run with Docker Compose

```sh
docker compose -f docker-compose.dev.yml up -d
make run
```

### CI/CD

Docker images are built and pushed to GitHub Container Registry on every push to `master` and on version tags (`v*.*.*`). See [.github/workflows/docker.yml](.github/workflows/docker.yml).

```sh
docker pull ghcr.io/isalikov/cgram-server:latest
```

## Architecture

### Overview

```
Client ──WebSocket──▶ /ws ──▶ Router ──▶ Auth / KeyStore / Relay
                                              │
                                         PostgreSQL
```

A single WebSocket endpoint (`/ws`) handles all communication. Clients send and receive binary protobuf `Frame` messages. The server deserializes each frame, routes it by payload type, and responds with another frame.

### Startup sequence

1. Load configuration from `.env` file
2. Connect to PostgreSQL
3. Run database migrations (embedded SQL, executed in transactions)
4. Initialize services: Auth, KeyStore, Relay
5. Start HTTP server with WebSocket handler on `/ws`
6. Wait for SIGINT/SIGTERM for graceful shutdown

### Connection lifecycle

When a client connects via WebSocket, the server creates a `Session` object (initially unauthenticated). The server enters a read loop, waiting for binary frames. Each frame is deserialized and routed:

| Frame type | Handler | What happens |
|------------|---------|--------------|
| `RegisterRequest` | Auth | Creates account with Argon2id-hashed password, returns `user_id` |
| `LoginRequest` | Auth | Verifies password, creates session token, registers connection for delivery, flushes queued messages |
| `LogoutRequest` | Auth | Deletes session, unregisters connection |
| `UploadPreKeysRequest` | KeyStore | Stores signed pre-key and one-time pre-keys |
| `FetchPreKeyRequest` | KeyStore | Returns identity key + signed pre-key + one one-time key (consumed on fetch) |
| `Envelope` | Relay | Delivers encrypted blob to recipient (direct or queued) |

On disconnect, the connection is unregistered from the relay.

### Project structure

```
cmd/server/main.go              Entry point
internal/
  config/config.go              Load configuration from environment / .env
  store/
    store.go                    PostgreSQL connection pool
    migrate.go                  Embedded SQL migrations (no external dependencies)
    migrations/001_init.sql     Database schema
  auth/auth.go                  Registration, login, sessions (Argon2id)
  keystore/keystore.go          X3DH pre-key bundle storage and distribution
  relay/relay.go                Message delivery (online + offline queue)
  ws/
    handler.go                  WebSocket accept and read loop
    router.go                   Frame deserialization and routing
```

### Services

**Auth** — manages accounts and sessions. Passwords are hashed with Argon2id (16-byte salt, 32-byte key, 64 MB memory cost). Session tokens are 64 hex characters. Passwords are verified using constant-time comparison.

**KeyStore** — stores encryption keys for the X3DH key exchange protocol. When a user wants to start a conversation, they fetch the other user's pre-key bundle. One-time pre-keys are consumed (deleted from DB) on fetch — each key is used exactly once.

**Relay** — delivers encrypted messages. Maintains an in-memory map of online connections (`map[userID]*websocket.Conn`). If the recipient is online, the message is written directly to their WebSocket. If offline, it is stored in the `message_queue` table. When a user logs in, all queued messages are flushed and deleted.

### Database schema

```
users              id, username, password (argon2id), identity_key, created_at
sessions           token, user_id, created_at
pre_keys           user_id (PK), signed_pre_key, signed_pre_key_signature
one_time_pre_keys  id, user_id, key_data
message_queue      id, recipient_id, envelope (encrypted blob), created_at
```

Migrations are embedded into the binary via `embed.FS` and executed automatically on startup. Each migration runs in a transaction; applied migrations are tracked in the `schema_migrations` table.

### What the server does NOT do

- Does not decrypt messages — it only sees `recipient_id` and an encrypted blob
- Does not know the sender — `sender_sealed` is encrypted with the recipient's key
- Does not log IP addresses
- Does not store message history — messages are deleted after delivery

## Protocol

See [cgram-proto](https://github.com/isalikov/cgram-proto) for the wire protocol definition.

## License

[MIT](LICENSE)
