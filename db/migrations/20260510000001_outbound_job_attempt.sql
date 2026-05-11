-- migrate:up
CREATE TABLE public.outbound_job_attempt (
    id uuid DEFAULT uuidv7() NOT NULL,
    job_id uuid NOT NULL,
    attempt_number integer NOT NULL,
    outcome text NOT NULL,
    server_response text,
    attempt_datetime timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.outbound_job_attempt
    ADD CONSTRAINT outbound_job_attempt_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.outbound_job_attempt
    ADD CONSTRAINT outbound_job_attempt_job_id_fkey FOREIGN KEY (job_id) REFERENCES public.outbound_job(id) ON DELETE CASCADE;

CREATE INDEX idx_outbound_job_attempt_job_id ON public.outbound_job_attempt USING btree (job_id);

-- migrate:down
DROP TABLE public.outbound_job_attempt;
