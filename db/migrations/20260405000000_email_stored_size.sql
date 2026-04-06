-- migrate:up

ALTER TABLE email ADD COLUMN stored_size BIGINT NOT NULL DEFAULT 0;

-- migrate:down

ALTER TABLE email DROP COLUMN stored_size;
