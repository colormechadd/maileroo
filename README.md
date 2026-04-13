# MAILAROO

MAILAROO is a modern, all-in-one email platform built with Go. It serves as a monolithic application providing an SMTP engine for receiving emails, a sophisticated ingestion pipeline for validation and filtering, and a sleek webmail interface for end-users.

## Core Features

-   **Monolithic Architecture**: A single binary provides the SMTP server, ingestion pipeline, and webmail UI.
-   **Intelligent Ingestion Pipeline**:
    -   **Sender Validation**: SPF, DKIM, and DMARC (passing if either SPF or DKIM is valid, or if DMARC policy is `none`).
    -   **Spam Protection**: Remote IP checking against configurable RBLs (e.g., Spamhaus).
    -   **Per-Mailbox Blocking**: Regex-based blocking rules defined per mailbox.
-   **Advanced Storage Engine**:
    -   **Flexible Backends**: Support for Local Filesystem, AWS S3 (or Minio), and Google Cloud Storage.
    -   **Compression**: Optional Zstandard (ZSTD) or GZIP compression for emails and attachments.
-   **Webmail Interface**:
    -   **Modern Tech Stack**: Built with Chi, Templ (type-safe templates), HTMX (dynamic partial updates), and Tailwind CSS.
    -   **Conversation Threading**: Automatic grouping of emails into threads based on Message-ID headers.
    -   **Secure Authentication**: Session management with Argon2id password hashing and persistent session tracking.
-   **Administrative CLI**: Built-in commands to manage users, mailboxes, and regex mapping rules.

## Tech Stack

-   **Backend**: Go (Golang)
-   **Database**: PostgreSQL (using `sqlx` for performance, `dbmate` for migrations)
-   **Frontend**: Templ, HTMX, Tailwind CSS
-   **Messaging**: `emersion/go-smtp`, `emersion/go-message`, `emersion/go-msgauth`
-   **Security**: `argon2` for passwords, `uuidv7` for identifiers
-   **Observability**: `slog` (structured logging)

## Getting Started

### Prerequisites

-   Go 1.26+
-   Docker and Docker Compose
-   `dbmate` (for migrations)
-   `templ` (for template generation)
-   `tailwindcss` (standalone CLI or via npm)

### Setup

1.  **Start the Database**:
    ```bash
    docker-compose up -d
    ```

2.  **Initialize Environment**:
    Create a `.env` file (see `.env` for examples) and ensure `DATABASE_URL` is set correctly.

3.  **Run Migrations**:
    ```bash
    dbmate up
    ```

4.  **Install Dependencies & Generate Code**:
    ```bash
    go mod tidy
    make generate
    ```

5.  **Run the Server**:
    ```bash
    # For development with live reload
    air

    # Or via the binary
    make run
    ```

### Administrative CLI Examples

**Create a user**:
```bash
./mailaroo admin user-create myuser mypassword
```

**Create a mailbox**:
```bash
./mailaroo admin mailbox-create myuser Inbox
```

**Map an email address to a mailbox**:
```bash
# Uses regex mapping
./mailaroo admin mapping-create <MAILBOX_ID> ".*@example.com"
```

## License

This project is licensed under the **AGPLv3**. See `LICENSE.md` for the full text.
