CREATE TABLE users (
    id          TEXT PRIMARY KEY,
    username    TEXT UNIQUE NOT NULL,
    password    BYTEA NOT NULL,       -- Argon2id hash
    identity_key BYTEA NOT NULL,      -- Ed25519 public key
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE sessions (
    token       TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE pre_keys (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    signed_pre_key          BYTEA NOT NULL,
    signed_pre_key_signature BYTEA NOT NULL
);

CREATE TABLE one_time_pre_keys (
    id          SERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_data    BYTEA NOT NULL
);

CREATE TABLE message_queue (
    id              SERIAL PRIMARY KEY,
    recipient_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    envelope        BYTEA NOT NULL,    -- serialized protobuf Envelope
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_message_queue_recipient ON message_queue(recipient_id);
CREATE INDEX idx_one_time_pre_keys_user ON one_time_pre_keys(user_id);
