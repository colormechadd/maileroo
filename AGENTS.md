# Maileroo Project Architecture

Maileroo is an all-in-one email platform built with Go, focusing on simplicity, extensibility, and modern web patterns.

## Core Components

- **SMTP Engine (`internal/smtp`)**: Monolithic SMTP server (sending/receiving).
- **Mailbox Management (`internal/mail`)**: Address-to-mailbox mapping logic.
- **Ingestion Pipeline (`internal/pipeline`)**: Validation and filtering.
- **Web Interface (`internal/web`)**: Chi-based webmail (HTMX + Templ).

## Deployment

- **Single Binary**: Maileroo is a monolith. Run it with a single executable from `cmd/maileroo`.

## Directory Structure

- `cmd/maileroo`: Entry point for the monolith.
- `internal/`: Component logic.
- `pkg/`: Shared models.
- `migrations/`: SQL migrations.
- `templates/`: Templ source files.
- `static/`: Frontend assets.

## Database

The database schema is defined in db/postgres.sql

### Naming Conventions

* Table names are singular
* Timestamp fields should be present tense and use _datetime as suffix
* Use UUIDv7 for primary keys

## Web

Use htmx, tailwind and alpinejs
