CREATE TABLE user (
    id  BIGSERIAL   PRIMARY KEY,
    name  TEXT NOT NULL,
    email TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX user_name_key ON user(lower(name))