-- migrate:up

CREATE TYPE email_direction AS ENUM (
    'INBOUND',
    'OUTBOUND'
);

CREATE TYPE email_status AS ENUM (
    'QUARANTINED',
    'DELETED',
    'INBOX',
    'ARCHIVED'
);

CREATE TYPE outbound_status AS ENUM (
    'QUEUED',
    'SENDING',
    'DELIVERED',
    'DEFERRED',
    'FAILED'
);

CREATE TABLE "user" (
    id uuid DEFAULT uuidv7() NOT NULL,
    username text NOT NULL,
    password_hash text NOT NULL,
    is_active boolean DEFAULT true,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE mailbox (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE mailbox_user (
    id uuid DEFAULT uuidv7() NOT NULL,
    mailbox_id uuid NOT NULL,
    user_id uuid NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE address_mapping (
    id uuid DEFAULT uuidv7() NOT NULL,
    address_pattern text NOT NULL,
    mailbox_id uuid NOT NULL,
    priority integer DEFAULT 0,
    is_active boolean DEFAULT true,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE contact (
    id uuid DEFAULT uuidv7() NOT NULL,
    mailbox_id uuid NOT NULL,
    first_name text DEFAULT '' NOT NULL,
    last_name text DEFAULT '' NOT NULL,
    email text NOT NULL,
    phone text DEFAULT '' NOT NULL,
    street text DEFAULT '' NOT NULL,
    city text DEFAULT '' NOT NULL,
    state text DEFAULT '' NOT NULL,
    postal_code text DEFAULT '' NOT NULL,
    country text DEFAULT '' NOT NULL,
    notes text DEFAULT '' NOT NULL,
    is_favorite boolean DEFAULT false NOT NULL,
    create_datetime timestamp with time zone DEFAULT now() NOT NULL,
    update_datetime timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE dkim_key (
    id uuid DEFAULT uuidv7() NOT NULL,
    domain text NOT NULL,
    selector text DEFAULT 'mailaroo' NOT NULL,
    key_data bytea NOT NULL,
    is_active boolean DEFAULT true,
    create_datetime timestamp with time zone DEFAULT now(),
    update_datetime timestamp with time zone DEFAULT now()
);

CREATE TABLE sending_address (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    mailbox_id uuid NOT NULL,
    address text NOT NULL,
    is_active boolean DEFAULT true,
    display_name text,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE draft (
    id uuid DEFAULT uuidv7() NOT NULL,
    mailbox_id uuid NOT NULL,
    user_id uuid NOT NULL,
    sending_address_id uuid,
    to_address text DEFAULT '' NOT NULL,
    cc_address text DEFAULT '' NOT NULL,
    bcc_address text DEFAULT '' NOT NULL,
    subject text DEFAULT '' NOT NULL,
    body text DEFAULT '' NOT NULL,
    body_html text DEFAULT '' NOT NULL,
    in_reply_to text,
    "references" text,
    create_datetime timestamp with time zone DEFAULT now(),
    update_datetime timestamp with time zone DEFAULT now()
);

CREATE TABLE ingestion (
    id uuid DEFAULT uuidv7() NOT NULL,
    message_id text,
    from_address text,
    to_address text,
    status text NOT NULL,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ingestion_step (
    id uuid DEFAULT uuidv7() NOT NULL,
    ingestion_id uuid NOT NULL,
    step_name text NOT NULL,
    status text NOT NULL,
    details jsonb,
    duration_ms integer,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE thread (
    id uuid DEFAULT uuidv7() NOT NULL,
    mailbox_id uuid NOT NULL,
    subject text,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE email (
    id uuid DEFAULT uuidv7() NOT NULL,
    mailbox_id uuid NOT NULL,
    address_mapping_id uuid,
    ingestion_id uuid,
    thread_id uuid,
    sending_address_id uuid,
    user_id uuid,
    message_id text NOT NULL,
    subject text,
    from_address text NOT NULL,
    to_address text NOT NULL,
    reply_to_address text,
    in_reply_to text,
    "references" text,
    storage_key text NOT NULL,
    size bigint NOT NULL,
    stored_size bigint DEFAULT 0 NOT NULL,
    body_plain text,
    search_vector tsvector GENERATED ALWAYS AS (
        to_tsvector('english',
            COALESCE(subject, '') || ' ' ||
            COALESCE(from_address, '') || ' ' ||
            COALESCE(to_address, '') || ' ' ||
            COALESCE(body_plain, '')
        )
    ) STORED,
    receive_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    read_datetime timestamp with time zone,
    star_datetime timestamp with time zone,
    is_read boolean DEFAULT false,
    is_star boolean DEFAULT false,
    direction email_direction NOT NULL,
    status email_status DEFAULT 'INBOX' NOT NULL,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE email_attachment (
    id uuid DEFAULT uuidv7() NOT NULL,
    email_id uuid NOT NULL,
    filename text NOT NULL,
    content_type text NOT NULL,
    size bigint NOT NULL,
    storage_key text NOT NULL,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE outbound_job (
    id uuid DEFAULT uuidv7() NOT NULL,
    email_id uuid,
    from_address text NOT NULL,
    recipients jsonb NOT NULL,
    raw_message bytea NOT NULL,
    status outbound_status DEFAULT 'QUEUED' NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    last_error text,
    next_attempt_datetime timestamp with time zone DEFAULT now() NOT NULL,
    delivery_datetime timestamp with time zone,
    create_datetime timestamp with time zone DEFAULT now(),
    update_datetime timestamp with time zone DEFAULT now()
);

CREATE TABLE greylist_entry (
    id uuid DEFAULT uuidv7() NOT NULL,
    ip_address inet NOT NULL,
    from_address text NOT NULL,
    to_address text NOT NULL,
    first_seen timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    last_seen timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    pass_count integer DEFAULT 0 NOT NULL
);

CREATE TABLE ip_block (
    id uuid DEFAULT uuidv7() NOT NULL,
    ip_address inet NOT NULL,
    reason text,
    blocked_until timestamp with time zone,
    is_permanent boolean DEFAULT false NOT NULL,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE mailbox_block_rule (
    id uuid DEFAULT uuidv7() NOT NULL,
    mailbox_id uuid NOT NULL,
    user_id uuid,
    address_pattern text NOT NULL,
    is_active boolean DEFAULT true,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE mailbox_filter_rule (
    id uuid DEFAULT uuidv7() NOT NULL,
    mailbox_id uuid NOT NULL,
    name text NOT NULL,
    priority integer DEFAULT 0 NOT NULL,
    is_active boolean DEFAULT true,
    match_all boolean DEFAULT true,
    action text NOT NULL,
    stop_processing boolean DEFAULT true,
    created_by_user_id uuid,
    updated_by_user_id uuid,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    update_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE mailbox_filter_condition (
    id uuid DEFAULT uuidv7() NOT NULL,
    rule_id uuid NOT NULL,
    field text NOT NULL,
    operator text NOT NULL,
    value text,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webmail_session (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    token text NOT NULL,
    remote_ip text,
    user_agent text,
    expires_datetime timestamp with time zone NOT NULL,
    create_datetime timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

-- Primary keys
ALTER TABLE ONLY "user" ADD CONSTRAINT user_pkey PRIMARY KEY (id);
ALTER TABLE ONLY mailbox ADD CONSTRAINT mailbox_pkey PRIMARY KEY (id);
ALTER TABLE ONLY mailbox_user ADD CONSTRAINT mailbox_user_pkey PRIMARY KEY (id);
ALTER TABLE ONLY address_mapping ADD CONSTRAINT address_mapping_pkey PRIMARY KEY (id);
ALTER TABLE ONLY contact ADD CONSTRAINT contact_pkey PRIMARY KEY (id);
ALTER TABLE ONLY dkim_key ADD CONSTRAINT dkim_key_pkey PRIMARY KEY (id);
ALTER TABLE ONLY sending_address ADD CONSTRAINT sending_address_pkey PRIMARY KEY (id);
ALTER TABLE ONLY draft ADD CONSTRAINT draft_pkey PRIMARY KEY (id);
ALTER TABLE ONLY ingestion ADD CONSTRAINT ingestion_pkey PRIMARY KEY (id);
ALTER TABLE ONLY ingestion_step ADD CONSTRAINT ingestion_step_pkey PRIMARY KEY (id);
ALTER TABLE ONLY thread ADD CONSTRAINT thread_pkey PRIMARY KEY (id);
ALTER TABLE ONLY email ADD CONSTRAINT email_pkey PRIMARY KEY (id);
ALTER TABLE ONLY email_attachment ADD CONSTRAINT email_attachment_pkey PRIMARY KEY (id);
ALTER TABLE ONLY outbound_job ADD CONSTRAINT outbound_job_pkey PRIMARY KEY (id);
ALTER TABLE ONLY greylist_entry ADD CONSTRAINT greylist_entry_pkey PRIMARY KEY (id);
ALTER TABLE ONLY ip_block ADD CONSTRAINT ip_block_pkey PRIMARY KEY (id);
ALTER TABLE ONLY mailbox_block_rule ADD CONSTRAINT mailbox_block_rule_pkey PRIMARY KEY (id);
ALTER TABLE ONLY mailbox_filter_rule ADD CONSTRAINT mailbox_filter_rule_pkey PRIMARY KEY (id);
ALTER TABLE ONLY mailbox_filter_condition ADD CONSTRAINT mailbox_filter_condition_pkey PRIMARY KEY (id);
ALTER TABLE ONLY webmail_session ADD CONSTRAINT webmail_session_pkey PRIMARY KEY (id);

-- Unique constraints
ALTER TABLE ONLY "user" ADD CONSTRAINT user_username_key UNIQUE (username);
ALTER TABLE ONLY dkim_key ADD CONSTRAINT dkim_key_domain_selector_key UNIQUE (domain, selector);
ALTER TABLE ONLY sending_address ADD CONSTRAINT sending_address_user_id_address_key UNIQUE (user_id, address);
ALTER TABLE ONLY contact ADD CONSTRAINT contact_mailbox_id_email_key UNIQUE (mailbox_id, email);
ALTER TABLE ONLY greylist_entry ADD CONSTRAINT greylist_entry_ip_address_from_address_to_address_key UNIQUE (ip_address, from_address, to_address);
ALTER TABLE ONLY webmail_session ADD CONSTRAINT webmail_session_token_key UNIQUE (token);

-- Foreign keys
ALTER TABLE ONLY mailbox_user ADD CONSTRAINT mailbox_user_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY mailbox_user ADD CONSTRAINT mailbox_user_user_id_fkey FOREIGN KEY (user_id) REFERENCES "user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY address_mapping ADD CONSTRAINT address_mapping_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY contact ADD CONSTRAINT contact_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY sending_address ADD CONSTRAINT sending_address_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY sending_address ADD CONSTRAINT sending_address_user_id_fkey FOREIGN KEY (user_id) REFERENCES "user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY draft ADD CONSTRAINT draft_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY draft ADD CONSTRAINT draft_user_id_fkey FOREIGN KEY (user_id) REFERENCES "user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY draft ADD CONSTRAINT draft_sending_address_id_fkey FOREIGN KEY (sending_address_id) REFERENCES sending_address(id) ON DELETE SET NULL;
ALTER TABLE ONLY ingestion_step ADD CONSTRAINT ingestion_step_ingestion_id_fkey FOREIGN KEY (ingestion_id) REFERENCES ingestion(id) ON DELETE CASCADE;
ALTER TABLE ONLY thread ADD CONSTRAINT thread_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY email ADD CONSTRAINT email_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY email ADD CONSTRAINT email_address_mapping_id_fkey FOREIGN KEY (address_mapping_id) REFERENCES address_mapping(id) ON DELETE SET NULL;
ALTER TABLE ONLY email ADD CONSTRAINT email_ingestion_id_fkey FOREIGN KEY (ingestion_id) REFERENCES ingestion(id) ON DELETE SET NULL;
ALTER TABLE ONLY email ADD CONSTRAINT email_thread_id_fkey FOREIGN KEY (thread_id) REFERENCES thread(id) ON DELETE SET NULL;
ALTER TABLE ONLY email ADD CONSTRAINT email_sending_address_id_fkey FOREIGN KEY (sending_address_id) REFERENCES sending_address(id) ON DELETE SET NULL;
ALTER TABLE ONLY email ADD CONSTRAINT email_user_id_fkey FOREIGN KEY (user_id) REFERENCES "user"(id) ON DELETE SET NULL;
ALTER TABLE ONLY email_attachment ADD CONSTRAINT email_attachment_email_id_fkey FOREIGN KEY (email_id) REFERENCES email(id) ON DELETE CASCADE;
ALTER TABLE ONLY outbound_job ADD CONSTRAINT outbound_job_email_id_fkey FOREIGN KEY (email_id) REFERENCES email(id) ON DELETE SET NULL;
ALTER TABLE ONLY mailbox_block_rule ADD CONSTRAINT mailbox_block_rule_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY mailbox_block_rule ADD CONSTRAINT mailbox_block_rule_user_id_fkey FOREIGN KEY (user_id) REFERENCES "user"(id) ON DELETE SET NULL;
ALTER TABLE ONLY mailbox_filter_rule ADD CONSTRAINT mailbox_filter_rule_mailbox_id_fkey FOREIGN KEY (mailbox_id) REFERENCES mailbox(id) ON DELETE CASCADE;
ALTER TABLE ONLY mailbox_filter_rule ADD CONSTRAINT mailbox_filter_rule_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES "user"(id) ON DELETE SET NULL;
ALTER TABLE ONLY mailbox_filter_rule ADD CONSTRAINT mailbox_filter_rule_updated_by_user_id_fkey FOREIGN KEY (updated_by_user_id) REFERENCES "user"(id) ON DELETE SET NULL;
ALTER TABLE ONLY mailbox_filter_condition ADD CONSTRAINT mailbox_filter_condition_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES mailbox_filter_rule(id) ON DELETE CASCADE;
ALTER TABLE ONLY webmail_session ADD CONSTRAINT webmail_session_user_id_fkey FOREIGN KEY (user_id) REFERENCES "user"(id) ON DELETE CASCADE;

-- Indexes
CREATE INDEX idx_address_mapping_pattern ON address_mapping USING btree (address_pattern);
CREATE INDEX idx_contact_mailbox_id ON contact USING btree (mailbox_id);
CREATE INDEX idx_contact_mailbox_email ON contact USING btree (mailbox_id, email);
CREATE INDEX idx_contact_mailbox_name ON contact USING btree (mailbox_id, last_name, first_name);
CREATE INDEX idx_email_mailbox_id ON email USING btree (mailbox_id);
CREATE INDEX idx_email_address_mapping_id ON email USING btree (address_mapping_id);
CREATE INDEX idx_email_ingestion_id ON email USING btree (ingestion_id);
CREATE INDEX idx_email_thread_id ON email USING btree (thread_id);
CREATE INDEX idx_email_sending_address_id ON email USING btree (sending_address_id);
CREATE INDEX idx_email_user_id ON email USING btree (user_id);
CREATE INDEX idx_email_message_id ON email USING btree (message_id);
CREATE INDEX idx_email_receive_datetime ON email USING btree (receive_datetime DESC);
CREATE INDEX idx_email_direction ON email USING btree (direction);
CREATE INDEX idx_email_status ON email USING btree (status);
CREATE INDEX idx_email_is_read ON email USING btree (is_read);
CREATE INDEX idx_email_is_star ON email USING btree (is_star);
CREATE INDEX idx_email_search_vector ON email USING gin (search_vector);
CREATE INDEX idx_greylist_lookup ON greylist_entry USING btree (ip_address, from_address, to_address);
CREATE INDEX idx_ingestion_step_ingestion_id ON ingestion_step USING btree (ingestion_id);
CREATE INDEX idx_ip_block_ip ON ip_block USING btree (ip_address);
CREATE INDEX idx_ip_block_until ON ip_block USING btree (blocked_until) WHERE (is_permanent = false);
CREATE INDEX idx_mailbox_block_rule_mailbox_id ON mailbox_block_rule USING btree (mailbox_id);
CREATE UNIQUE INDEX idx_mailbox_user_active ON mailbox_user USING btree (mailbox_id, user_id) WHERE (is_active = true);
CREATE INDEX idx_mailbox_user_mailbox_id ON mailbox_user USING btree (mailbox_id);
CREATE INDEX idx_mailbox_user_user_id ON mailbox_user USING btree (user_id);
CREATE INDEX idx_filter_rule_mailbox_id ON mailbox_filter_rule USING btree (mailbox_id);
CREATE INDEX idx_filter_rule_priority ON mailbox_filter_rule USING btree (mailbox_id, priority);
CREATE INDEX idx_filter_condition_rule_id ON mailbox_filter_condition USING btree (rule_id);
CREATE INDEX idx_outbound_job_status_next ON outbound_job USING btree (status, next_attempt_datetime) WHERE (status = ANY (ARRAY['QUEUED'::outbound_status, 'DEFERRED'::outbound_status]));
CREATE INDEX idx_sending_address_user_id ON sending_address USING btree (user_id);
CREATE INDEX idx_thread_mailbox_id ON thread USING btree (mailbox_id);
CREATE INDEX idx_webmail_session_token ON webmail_session USING btree (token);

-- migrate:down

DROP TABLE IF EXISTS webmail_session CASCADE;
DROP TABLE IF EXISTS mailbox_filter_condition CASCADE;
DROP TABLE IF EXISTS mailbox_filter_rule CASCADE;
DROP TABLE IF EXISTS mailbox_block_rule CASCADE;
DROP TABLE IF EXISTS ip_block CASCADE;
DROP TABLE IF EXISTS greylist_entry CASCADE;
DROP TABLE IF EXISTS outbound_job CASCADE;
DROP TABLE IF EXISTS email_attachment CASCADE;
DROP TABLE IF EXISTS email CASCADE;
DROP TABLE IF EXISTS thread CASCADE;
DROP TABLE IF EXISTS ingestion_step CASCADE;
DROP TABLE IF EXISTS ingestion CASCADE;
DROP TABLE IF EXISTS draft CASCADE;
DROP TABLE IF EXISTS sending_address CASCADE;
DROP TABLE IF EXISTS dkim_key CASCADE;
DROP TABLE IF EXISTS contact CASCADE;
DROP TABLE IF EXISTS address_mapping CASCADE;
DROP TABLE IF EXISTS mailbox_user CASCADE;
DROP TABLE IF EXISTS mailbox CASCADE;
DROP TABLE IF EXISTS "user" CASCADE;

DROP TYPE IF EXISTS outbound_status;
DROP TYPE IF EXISTS email_status;
DROP TYPE IF EXISTS email_direction;

