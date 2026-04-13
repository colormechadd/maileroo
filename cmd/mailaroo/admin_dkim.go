package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log"

	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/internal/outbound"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var dkimCmd = &cobra.Command{
	Use:   "dkim",
	Short: "DKIM key management",
}

func init() {
	adminCmd.AddCommand(dkimCmd)
	dkimCmd.AddCommand(dkimAddCmd)
	dkimCmd.AddCommand(dkimListCmd)
	dkimCmd.AddCommand(dkimShowDNSCmd)
	dkimCmd.AddCommand(dkimRotateCmd)
}

func parseEncKey() []byte {
	if cfg.DKIM.EncryptionKey == "" {
		log.Fatal("MAILAROO_DKIM_ENCRYPTION_KEY is not set")
	}
	encKey, err := base64.StdEncoding.DecodeString(cfg.DKIM.EncryptionKey)
	if err != nil || len(encKey) != 32 {
		log.Fatal("MAILAROO_DKIM_ENCRYPTION_KEY must be a base64-encoded 32-byte value")
	}
	return encKey
}

var dkimAddCmd = &cobra.Command{
	Use:   "add [domain]",
	Short: "Generate a DKIM key for a domain",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		domain := args[0]
		encKey := parseEncKey()

		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		der, err := generateKeyDER()
		if err != nil {
			log.Fatalf("failed to generate key: %v", err)
		}

		encrypted, err := outbound.EncryptKey(der, encKey)
		if err != nil {
			log.Fatalf("failed to encrypt key: %v", err)
		}

		key := &models.DKIMKey{
			ID:       uuid.Must(uuid.NewV7()),
			Domain:   domain,
			Selector: "mailaroo",
			KeyData:  encrypted,
			IsActive: true,
		}
		if err := database.InsertDKIMKey(context.Background(), key); err != nil {
			log.Fatalf("failed to insert dkim key: %v", err)
		}

		dnsValue, err := derToDNSValue(der)
		if err != nil {
			log.Fatalf("failed to derive dns value: %v", err)
		}

		cmd.Printf("DKIM key generated for %s\n", domain)
		cmd.Printf("  Selector: %s\n", key.Selector)
		cmd.Printf("\nAdd this TXT record to DNS:\n")
		cmd.Printf("  Name:  %s._domainkey.%s\n", key.Selector, domain)
		cmd.Printf("  Value: %s\n", dnsValue)
	},
}

var dkimListCmd = &cobra.Command{
	Use:   "list",
	Short: "List DKIM keys",
	Run: func(cmd *cobra.Command, args []string) {
		encKey := parseEncKey()

		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		keys, err := database.ListDKIMKeys(context.Background())
		if err != nil {
			log.Fatalf("failed to list dkim keys: %v", err)
		}

		cmd.Printf("%-30s | %-12s | %-6s | %s\n", "Domain", "Selector", "Active", "Fingerprint")
		cmd.Println("-------------------------------------------------------------------------------------")
		for _, k := range keys {
			fp := keyFingerprint(k.KeyData, encKey)
			cmd.Printf("%-30s | %-12s | %-6v | %s\n", k.Domain, k.Selector, k.IsActive, fp)
		}
	},
}

var dkimShowDNSCmd = &cobra.Command{
	Use:   "show-dns [domain]",
	Short: "Print the DNS TXT record value for a domain's DKIM key",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		domain := args[0]
		encKey := parseEncKey()

		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		key, err := database.GetActiveDKIMKey(context.Background(), domain, nil)
		if err != nil {
			log.Fatalf("no active dkim key for domain %s: %v", domain, err)
		}

		der, err := outbound.DecryptKey(key.KeyData, encKey)
		if err != nil {
			log.Fatalf("failed to decrypt key: %v", err)
		}

		dnsValue, err := derToDNSValue(der)
		if err != nil {
			log.Fatalf("failed to derive dns value: %v", err)
		}

		cmd.Printf("Name:  %s._domainkey.%s\n", key.Selector, domain)
		cmd.Printf("Value: %s\n", dnsValue)
	},
}

var dkimRotateCmd = &cobra.Command{
	Use:   "rotate [domain]",
	Short: "Generate a new DKIM key for a domain, replacing the existing one",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		domain := args[0]
		encKey := parseEncKey()

		database, err := db.Connect(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		defer database.Close()

		existing, err := database.GetActiveDKIMKey(context.Background(), domain, nil)
		if err != nil {
			log.Fatalf("no active dkim key for domain %s: %v", domain, err)
		}

		der, err := generateKeyDER()
		if err != nil {
			log.Fatalf("failed to generate key: %v", err)
		}

		encrypted, err := outbound.EncryptKey(der, encKey)
		if err != nil {
			log.Fatalf("failed to encrypt key: %v", err)
		}

		if err := database.UpdateDKIMKeyData(context.Background(), existing.ID, encrypted); err != nil {
			log.Fatalf("failed to update dkim key: %v", err)
		}

		dnsValue, err := derToDNSValue(der)
		if err != nil {
			log.Fatalf("failed to derive dns value: %v", err)
		}

		cmd.Printf("DKIM key rotated for %s\n", domain)
		cmd.Printf("\nUpdate your DNS TXT record:\n")
		cmd.Printf("  Name:  %s._domainkey.%s\n", existing.Selector, domain)
		cmd.Printf("  Value: %s\n", dnsValue)
	},
}

func generateKeyDER() ([]byte, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("marshal pkcs8: %w", err)
	}
	return der, nil
}

func derToDNSValue(der []byte) (string, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return "", err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("expected RSA key, got %T", key)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v=DKIM1; k=rsa; p=%s", base64.StdEncoding.EncodeToString(pubDER)), nil
}

func keyFingerprint(encrypted, encKey []byte) string {
	der, err := outbound.DecryptKey(encrypted, encKey)
	if err != nil {
		return "<decrypt error>"
	}
	sum := sha256.Sum256(der)
	return fmt.Sprintf("SHA256:%s", base64.RawStdEncoding.EncodeToString(sum[:]))
}
