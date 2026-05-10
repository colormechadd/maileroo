-- migrate:up
ALTER TABLE email ADD COLUMN purged_datetime timestamp with time zone;

-- migrate:down
ALTER TABLE email DROP COLUMN purged_datetime;
