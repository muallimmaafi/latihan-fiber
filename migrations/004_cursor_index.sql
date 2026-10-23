-- Urutan kolom pada index HARUS sama persis dengan ORDER BY pada query,
-- termasuk arah DESC-nya. Bila tidak, PostgreSQL tidak dapat memakai index
-- untuk melompat langsung ke posisi cursor, dan seluruh keuntungan keyset
-- pagination hilang tanpa satu pun pesan kesalahan.
CREATE INDEX IF NOT EXISTS users_created_at_id_desc_idx
    ON users (created_at DESC, id DESC);