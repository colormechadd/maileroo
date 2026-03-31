-- migrate:up
CREATE TABLE draft (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    mailbox_id UUID NOT NULL REFERENCES mailbox(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    sending_address_id UUID REFERENCES sending_address(id) ON DELETE SET NULL,
    to_address TEXT NOT NULL DEFAULT '',
    cc_address TEXT NOT NULL DEFAULT '',
    bcc_address TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    body_html TEXT NOT NULL DEFAULT '',
    in_reply_to TEXT,
    "references" TEXT,
    create_datetime TIMESTAMPTZ DEFAULT NOW(),
    update_datetime TIMESTAMPTZ DEFAULT NOW()
);

-- migrate:down
DROP TABLE draft;
