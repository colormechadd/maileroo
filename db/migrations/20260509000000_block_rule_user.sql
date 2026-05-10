-- migrate:up
ALTER TABLE mailbox_block_rule ADD COLUMN user_id UUID REFERENCES "user"(id) ON DELETE SET NULL;

-- migrate:down
ALTER TABLE mailbox_block_rule DROP COLUMN user_id;
