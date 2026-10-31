
-- Migration 008: transactional outbox dan catatan notifikasi

-- Outbox menyimpan event yang harus diproses secara asynchronous.
CREATE TABLE IF NOT EXISTS outbox (
    id           BIGSERIAL PRIMARY KEY,
    event_type   VARCHAR(50) NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ NULL
);

-- Relay hanya mencari event yang belum dipublikasikan.
CREATE INDEX IF NOT EXISTS outbox_unpublished_idx
    ON outbox (id)
    WHERE published_at IS NULL;

-- Notifications menyimpan riwayat notifikasi yang berhasil dikirim.
CREATE TABLE IF NOT EXISTS notifications (
    id         BIGSERIAL PRIMARY KEY,
    outbox_id  BIGINT NOT NULL,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel    VARCHAR(20) NOT NULL,
    subject    VARCHAR(200) NOT NULL,
    body       TEXT NOT NULL,
    sent_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT notifications_outbox_unique UNIQUE (outbox_id)
);

CREATE INDEX IF NOT EXISTS notifications_user_idx
    ON notifications (user_id, outbox_id DESC);

-- Permission untuk memantau kondisi queue.
INSERT INTO permissions (name, description) VALUES
    ('queue:monitor', 'Melihat keadaan outbox dan queue task')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_name, permission_name) VALUES
    ('admin', 'queue:monitor')
ON CONFLICT DO NOTHING;
