package main

import (
	"context"
	"fmt"
	"log"

	"github.com/colormechadd/maileroo/internal/db"
	"github.com/colormechadd/maileroo/pkg/auth"
	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Administrative tasks",
}

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "User management",
}

var mailboxCmd = &cobra.Command{
	Use:   "mailbox",
	Short: "Mailbox management",
}

var sendingAddressCmd = &cobra.Command{
	Use:   "sending-address",
	Short: "Sending address management",
}

func init() {
	rootCmd.AddCommand(adminCmd)
	adminCmd.AddCommand(userCmd)
	adminCmd.AddCommand(mailboxCmd)
	adminCmd.AddCommand(sendingAddressCmd)

	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userListCmd)

	mailboxCmd.AddCommand(mailboxAddCmd)
	mailboxCmd.AddCommand(mailboxListCmd)
	mailboxCmd.AddCommand(mappingAddCmd)
	mailboxCmd.AddCommand(mailboxAddUserCmd)

	sendingAddressCmd.AddCommand(saAddCmd)
	sendingAddressCmd.AddCommand(saListCmd)
	sendingAddressCmd.AddCommand(saDeactivateCmd)
}

var userAddCmd = &cobra.Command{
	Use:   "add [username] [password]",
	Short: "Add a new user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		hash, err := auth.HashPassword(args[1])
		if err != nil {
			log.Fatalf("failed to hash password: %v", err)
		}

		user := &models.User{
			ID:           uuid.New(),
			Username:     args[0],
			PasswordHash: hash,
			IsActive:     true,
		}

		if err := database.CreateUser(context.Background(), user); err != nil {
			log.Fatalf("failed to create user: %v", err)
		}

		cmd.Printf("User %s created with ID %s\n", user.Username, user.ID)
	},
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		users, err := database.ListUsers(context.Background())
		if err != nil {
			log.Fatalf("failed to list users: %v", err)
		}

		cmd.Printf("%-36s | %-20s | %-6s\n", "ID", "Username", "Active")
		cmd.Println("----------------------------------------------------------------------")
		for _, u := range users {
			cmd.Printf("%-36s | %-20s | %-6v\n", u.ID, u.Username, u.IsActive)
		}
	},
}

var mailboxAddCmd = &cobra.Command{
	Use:   "add [username] [name]",
	Short: "Add a new mailbox for a user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		user, err := database.GetUserByUsername(context.Background(), args[0])
		if err != nil {
			log.Fatalf("failed to find user %s: %v", args[0], err)
		}

		mb := &models.Mailbox{
			ID:   uuid.New(),
			Name: args[1],
		}

		if err := database.CreateMailbox(context.Background(), mb, user.ID); err != nil {
			log.Fatalf("failed to create mailbox: %v", err)
		}

		cmd.Printf("Mailbox %s created with ID %s for user %s\n", mb.Name, mb.ID, user.Username)
	},
}

var mailboxListCmd = &cobra.Command{
	Use:   "list [username]",
	Short: "List all mailboxes for a user",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		user, err := database.GetUserByUsername(context.Background(), args[0])
		if err != nil {
			log.Fatalf("failed to find user %s: %v", args[0], err)
		}

		mailboxes, err := database.ListMailboxes(context.Background(), user.ID)
		if err != nil {
			log.Fatalf("failed to list mailboxes: %v", err)
		}

		cmd.Printf("%-36s | %-20s\n", "ID", "Name")
		cmd.Println("------------------------------------------------------------")
		for _, m := range mailboxes {
			cmd.Printf("%-36s | %-20s\n", m.ID, m.Name)
		}
	},
}

var mailboxAddUserCmd = &cobra.Command{
	Use:   "add-user [mailbox_id] [username]",
	Short: "Add a user to a mailbox",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		mailboxID, err := uuid.Parse(args[0])
		if err != nil {
			log.Fatalf("invalid mailbox ID: %v", err)
		}

		user, err := database.GetUserByUsername(context.Background(), args[1])
		if err != nil {
			log.Fatalf("failed to find user %s: %v", args[1], err)
		}

		if err := database.AddUserToMailbox(context.Background(), mailboxID, user.ID); err != nil {
			log.Fatalf("failed to add user to mailbox: %v", err)
		}

		cmd.Printf("User %s added to mailbox %s\n", user.Username, mailboxID)
	},
}

var mappingAddCmd = &cobra.Command{
	Use:   "add-mapping [mailbox_id] [pattern] [priority]",
	Short: "Add a new address mapping for a mailbox",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		mbID, err := uuid.Parse(args[0])
		if err != nil {
			log.Fatalf("invalid mailbox ID: %v", err)
		}

		var valid bool
		if err := database.QueryRowContext(context.Background(), `SELECT '' ~ $1`, args[1]).Scan(&valid); err != nil {
			log.Fatalf("invalid address pattern (not a valid PostgreSQL regex): %v", err)
		}

		var priority int
		fmt.Sscanf(args[2], "%d", &priority)

		am := &models.AddressMapping{
			ID:             uuid.New(),
			MailboxID:      mbID,
			AddressPattern: args[1],
			Priority:       priority,
		}

		if err := database.CreateAddressMapping(context.Background(), am); err != nil {
			log.Fatalf("failed to create mapping: %v", err)
		}

		cmd.Printf("Mapping %s created with ID %s\n", am.AddressPattern, am.ID)
	},
}

var saAddCmd = &cobra.Command{
	Use:   "add [username] [mailbox_id] [address]",
	Short: "Add an authorized sending address for a user",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		user, err := database.GetUserByUsername(context.Background(), args[0])
		if err != nil {
			log.Fatalf("failed to find user %s: %v", args[0], err)
		}

		mbID, err := uuid.Parse(args[1])
		if err != nil {
			log.Fatalf("invalid mailbox ID: %v", err)
		}

		sa := &models.SendingAddress{
			ID:        uuid.New(),
			UserID:    user.ID,
			MailboxID: mbID,
			Address:   args[2],
			IsActive:  true,
		}

		if err := database.AddSendingAddress(context.Background(), sa); err != nil {
			log.Fatalf("failed to add sending address: %v", err)
		}

		cmd.Printf("Sending address %s added for user %s\n", sa.Address, user.Username)
	},
}

var saListCmd = &cobra.Command{
	Use:   "list [username]",
	Short: "List authorized sending addresses for a user",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		user, err := database.GetUserByUsername(context.Background(), args[0])
		if err != nil {
			log.Fatalf("failed to find user %s: %v", args[0], err)
		}

		addresses, err := database.ListSendingAddresses(context.Background(), user.ID)
		if err != nil {
			log.Fatalf("failed to list sending addresses: %v", err)
		}

		cmd.Printf("%-36s | %-30s | %-6s\n", "ID", "Address", "Active")
		cmd.Println("---------------------------------------------------------------------------")
		for _, a := range addresses {
			cmd.Printf("%-36s | %-30s | %-6v\n", a.ID, a.Address, a.IsActive)
		}
	},
}

var saDeactivateCmd = &cobra.Command{
	Use:   "deactivate [id]",
	Short: "Deactivate an authorized sending address",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		saID, err := uuid.Parse(args[0])
		if err != nil {
			log.Fatalf("invalid ID: %v", err)
		}

		if err := database.DeactivateSendingAddress(context.Background(), saID); err != nil {
			log.Fatalf("failed to deactivate sending address: %v", err)
		}

		cmd.Printf("Sending address %s deactivated\n", saID)
	},
}
