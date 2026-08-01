CREATE TABLE users (
    id  BIGSERIAL   PRIMARY KEY,
    name  TEXT NOT NULL,
    email TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_key ON users(lower(email))