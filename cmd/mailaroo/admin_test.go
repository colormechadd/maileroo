package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/colormechadd/mailaroo/internal/config"
	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/pkg/auth"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
)

// executeCommand is a helper to run a cobra command and capture output
func executeCommand(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)

	err := rootCmd.Execute()
	return buf.String(), err
}

func setupCLITestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	devURL := os.Getenv("DATABASE_URL")
	if devURL == "" {
		t.Fatal("DATABASE_URL must be set for CLI tests")
	}
	devURL = os.ExpandEnv(devURL)
	testURL := strings.Replace(devURL, "/mailaroo?", "/mailaroo_test_cli?", 1)
	if !strings.Contains(testURL, "sslmode=") {
		if strings.Contains(testURL, "?") {
			testURL += "&sslmode=disable"
		} else {
			testURL += "?sslmode=disable"
		}
	}

	// 1. Create the test database if it doesn't exist
	adminURL := strings.Replace(testURL, "/mailaroo_test_cli?", "/postgres?", 1)
	adminDB, err := sqlx.Connect("postgres", adminURL)
	if err != nil {
		t.Fatalf("failed to connect to postgres for test db creation: %v", err)
	}
	_, _ = adminDB.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'mailaroo_test_cli' AND pid <> pg_backend_pid()")
	_, _ = adminDB.Exec("DROP DATABASE IF EXISTS mailaroo_test_cli")
	_, err = adminDB.Exec("CREATE DATABASE mailaroo_test_cli")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	adminDB.Close()

	// 2. Run migrations using dbmate
	cmd := exec.Command("dbmate", "-u", testURL, "--no-dump-schema", "up")
	cmd.Dir = "../../"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dbmate migration failed: %v\nOutput: %s", err, string(out))
	}

	os.Setenv("MAILAROO_DATABASE_URL", testURL)
	os.Setenv("DATABASE_URL", testURL)

	database, err := db.Connect(testURL)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	// Re-load config
	cfg, _ = config.LoadConfig()

	return database, func() {
		database.Close()
	}
}

func TestAdminUserCommands(t *testing.T) {
	database, cleanup := setupCLITestDB(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("user add", func(t *testing.T) {
		output, err := executeCommand("admin", "user", "add", "clitest", "password123")
		assert.NoError(t, err)
		assert.Contains(t, output, "User clitest created")

		user, err := database.GetUserByUsername(ctx, "clitest")
		assert.NoError(t, err)
		assert.Equal(t, "clitest", user.Username)

		match, _ := auth.ComparePassword("password123", user.PasswordHash)
		assert.True(t, match)
	})

	t.Run("user list", func(t *testing.T) {
		output, err := executeCommand("admin", "user", "list")
		assert.NoError(t, err)
		assert.Contains(t, output, "clitest")
	})
}

func TestAdminMailboxCommands(t *testing.T) {
	database, cleanup := setupCLITestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Seed user
	userID := uuid.New()
	user := &models.User{
		ID:           userID,
		Username:     "mailboxuser",
		PasswordHash: "hash",
		IsActive:     true,
	}
	err := database.CreateUser(ctx, user)
	assert.NoError(t, err)

	t.Run("mailbox add", func(t *testing.T) {
		output, err := executeCommand("admin", "mailbox", "add", "mailboxuser", "Work")
		assert.NoError(t, err)
		assert.Contains(t, output, "Mailbox Work created")

		// Verify in DB
		var count int
		err = database.GetContext(ctx, &count, "SELECT COUNT(*) FROM mailbox m JOIN mailbox_user mu ON mu.mailbox_id = m.id WHERE mu.user_id = $1 AND m.name = 'Work'", user.ID)
		assert.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("add-mapping", func(t *testing.T) {
		var mailboxID string
		err := database.GetContext(ctx, &mailboxID, "SELECT id FROM mailbox WHERE name = 'Work' LIMIT 1")
		assert.NoError(t, err)

		output, err := executeCommand("admin", "mailbox", "add-mapping", mailboxID, `.*@work.com`, "50")
		assert.NoError(t, err)
		assert.Contains(t, output, "Mapping .*@work.com created")

		// Verify in DB
		var pattern string
		err = database.GetContext(ctx, &pattern, "SELECT address_pattern FROM address_mapping WHERE mailbox_id = $1", mailboxID)
		assert.NoError(t, err)
		assert.Equal(t, `.*@work.com`, pattern)
	})
}

func TestAdminSendingAddressCommands(t *testing.T) {
	database, cleanup := setupCLITestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Seed user
	userID := uuid.New()
	user := &models.User{
		ID:           userID,
		Username:     "sauser",
		PasswordHash: "hash",
		IsActive:     true,
	}
	err := database.CreateUser(ctx, user)
	assert.NoError(t, err)

	t.Run("sending-address add", func(t *testing.T) {
		// First create a mailbox
		mbID := uuid.New()
		err := database.CreateMailbox(ctx, &models.Mailbox{ID: mbID, Name: "Inbox"}, user.ID)
		assert.NoError(t, err)

		output, err := executeCommand("admin", "sending-address", "add", "sauser", mbID.String(), "me@example.com")
		assert.NoError(t, err)
		assert.Contains(t, output, "Sending address me@example.com added")

		// Verify in DB
		var count int
		err = database.GetContext(ctx, &count, "SELECT COUNT(*) FROM sending_address WHERE user_id = $1 AND address = 'me@example.com'", user.ID)
		assert.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("sending-address list", func(t *testing.T) {
		output, err := executeCommand("admin", "sending-address", "list", "sauser")
		assert.NoError(t, err)
		assert.Contains(t, output, "me@example.com")
	})

	t.Run("sending-address deactivate", func(t *testing.T) {
		var saID string
		err := database.GetContext(ctx, &saID, "SELECT id FROM sending_address WHERE address = 'me@example.com' LIMIT 1")
		assert.NoError(t, err)

		output, err := executeCommand("admin", "sending-address", "deactivate", saID)
		assert.NoError(t, err)
		assert.Contains(t, output, "deactivated")

		// Verify in DB
		var isActive bool
		err = database.GetContext(ctx, &isActive, "SELECT is_active FROM sending_address WHERE id = $1", saID)
		assert.NoError(t, err)
		assert.False(t, isActive)
	})
}
