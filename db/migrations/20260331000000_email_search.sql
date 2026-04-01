-- migrate:up

ALTER TABLE email ADD COLUMN body_plain TEXT;
ALTER TABLE email ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english',
            coalesce(subject, '') || ' ' ||
            coalesce(from_address, '') || ' ' ||
            coalesce(to_address, '') || ' ' ||
            coalesce(body_plain, '')
        )
    ) STORED;

CREATE INDEX idx_email_search_vector ON email USING GIN(search_vector);

-- migrate:down

DROP INDEX idx_email_search_vector;
ALTER TABLE email DROP COLUMN search_vector;
ALTER TABLE email DROP COLUMN body_plain;
