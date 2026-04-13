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

	url := os.Getenv("MAILAROO_TEST_DATABASE_URL")
	if url == "" {
		devURL := os.Getenv("DATABASE_URL")
		if devURL == "" {
			t.Fatal("DATABASE_URL or MAILAROO_TEST_DATABASE_URL must be set for DB tests")
		}
		devURL = os.ExpandEnv(devURL)
		url = strings.Replace(devURL, "/mailaroo?", "/mailaroo_test?", 1)
	}

	adminURL := strings.Replace(url, "/mailaroo_test?", "/postgres?", 1)
	adminDB, err := sqlx.Connect("postgres", adminURL)
	if err != nil {
		t.Fatalf("failed to connect to postgres for test db creation: %v", err)
	}
	_, _ = adminDB.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'mailaroo_test' AND pid <> pg_backend_pid()")
	_, _ = adminDB.Exec("DROP DATABASE IF EXISTS mailaroo_test")
	if _, err = adminDB.Exec("CREATE DATABASE mailaroo_test"); err != nil {
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
