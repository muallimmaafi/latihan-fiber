-- Permission untuk mengubah kuota kelas.
INSERT INTO permissions (name, description) VALUES
    ('course:update', 'Mengubah kuota kelas')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_name, permission_name) VALUES
    ('admin', 'course:update')
ON CONFLICT DO NOTHING;

-- Satu event outbox dapat menghasilkan notifikasi untuk banyak user.
-- Idempotensi harus berlaku untuk kombinasi event, user, dan channel.
ALTER TABLE notifications
    DROP CONSTRAINT IF EXISTS notifications_outbox_unique;

ALTER TABLE notifications
    ADD CONSTRAINT notifications_outbox_user_channel_unique
    UNIQUE (outbox_id, user_id, channel);