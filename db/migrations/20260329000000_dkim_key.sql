-- migrate:up

CREATE TABLE dkim_key (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    domain TEXT NOT NULL,
    selector TEXT NOT NULL DEFAULT 'mailaroo',
    key_data BYTEA NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    create_datetime TIMESTAMPTZ DEFAULT NOW(),
    update_datetime TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (domain, selector)
);

-- migrate:down

DROP TABLE dkim_key;
