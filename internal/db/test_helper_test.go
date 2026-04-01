package db

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()

	url := os.Getenv("MAILEROO_TEST_DATABASE_URL")
	if url == "" {
		devURL := os.Getenv("DATABASE_URL")
		if devURL == "" {
			t.Fatal("DATABASE_URL or MAILEROO_TEST_DATABASE_URL must be set for DB tests")
		}
		devURL = os.ExpandEnv(devURL)
		url = strings.Replace(devURL, "/maileroo?", "/maileroo_test?", 1)
	}

	adminURL := strings.Replace(url, "/maileroo_test?", "/postgres?", 1)
	adminDB, err := sqlx.Connect("postgres", adminURL)
	if err != nil {
		t.Fatalf("failed to connect to postgres for test db creation: %v", err)
	}
	_, _ = adminDB.Exec("DROP DATABASE IF EXISTS maileroo_test")
	if _, err = adminDB.Exec("CREATE DATABASE maileroo_test"); err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	adminDB.Close()

	cmd := exec.Command("psql", url, "-f", "db/postgres.sql")
	cmd.Dir = "../../"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to apply schema: %v\nOutput: %s", err, out)
	}

	db, err := Connect(url)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	return db, func() { db.Close() }
}
