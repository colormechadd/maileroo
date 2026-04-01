-- migrate:up
ALTER TABLE public.sending_address ADD COLUMN display_name text;

-- migrate:down
ALTER TABLE public.sending_address DROP COLUMN display_name;
