-- Optimistic locking: PATCH is a read-modify-write, and without a version
-- two concurrent updates silently overwrite each other's fields.
ALTER TABLE users ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
