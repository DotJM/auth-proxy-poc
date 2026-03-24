-- Auth Proxy PoC: Initial schema

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    target_host TEXT NOT NULL,
    auth_type   TEXT NOT NULL CHECK (auth_type IN ('cookie', 'bearer', 'custom_header')),
    credentials JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'expired', 'revoked')),
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE proxy_keys (
    key         TEXT PRIMARY KEY,
    session_id  UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_proxy_keys_session ON proxy_keys(session_id);
CREATE INDEX idx_sessions_status ON sessions(status);

-- Seed: example session with dummy credentials for PoC testing
INSERT INTO sessions (id, name, target_host, auth_type, credentials, status)
VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'example-api-session',
    'httpbin.org',
    'bearer',
    '{"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IlBvQyBUZXN0IiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"}',
    'active'
);

INSERT INTO proxy_keys (key, session_id)
VALUES ('apk_poc_test_key_00000000000000000000000000000001', 'a0000000-0000-0000-0000-000000000001');

-- Seed: cookie-based session example
INSERT INTO sessions (id, name, target_host, auth_type, credentials, status)
VALUES (
    'a0000000-0000-0000-0000-000000000002',
    'example-cookie-session',
    'httpbin.org',
    'cookie',
    '{"session_id": "abc123def456", "csrf_token": "xyz789"}',
    'active'
);

INSERT INTO proxy_keys (key, session_id)
VALUES ('apk_poc_test_key_00000000000000000000000000000002', 'a0000000-0000-0000-0000-000000000002');
