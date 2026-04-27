-- migrate:up

CREATE TABLE mailbox_filter_rule (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    mailbox_id UUID NOT NULL REFERENCES mailbox(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    priority INT NOT NULL DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    match_all BOOLEAN DEFAULT TRUE,
    action TEXT NOT NULL,
    stop_processing BOOLEAN DEFAULT TRUE,
    created_by_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    updated_by_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    create_datetime TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    update_datetime TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE mailbox_filter_condition (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    rule_id UUID NOT NULL REFERENCES mailbox_filter_rule(id) ON DELETE CASCADE,
    field TEXT NOT NULL,
    operator TEXT NOT NULL,
    value TEXT,
    create_datetime TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_filter_rule_mailbox_id ON mailbox_filter_rule(mailbox_id);
CREATE INDEX idx_filter_rule_priority ON mailbox_filter_rule(mailbox_id, priority ASC);
CREATE INDEX idx_filter_condition_rule_id ON mailbox_filter_condition(rule_id);

-- migrate:down

DROP TABLE IF EXISTS mailbox_filter_condition;
DROP TABLE IF EXISTS mailbox_filter_rule;
