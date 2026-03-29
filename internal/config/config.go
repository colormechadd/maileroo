package config

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type SMTPConfig struct {
	Ports          []int         `mapstructure:"PORTS"`
	TLSCertFile    string        `mapstructure:"TLS_CERT_FILE"`
	TLSKeyFile     string        `mapstructure:"TLS_KEY_FILE"`
	Domain         string        `mapstructure:"DOMAIN"`
	Relay          string        `mapstructure:"RELAY"` // Optional smarthost, e.g. "localhost:1025"
	ReadTimeout    time.Duration `mapstructure:"READ_TIMEOUT"`
	WriteTimeout   time.Duration `mapstructure:"WRITE_TIMEOUT"`
	MaxMessageSize int64         `mapstructure:"MAX_MESSAGE_SIZE"`
	MaxRecipients  int           `mapstructure:"MAX_RECIPIENTS"`
}

type LocalStorageConfig struct {
	BasePath string `mapstructure:"BASE_PATH"`
}

type S3StorageConfig struct {
	Bucket   string `mapstructure:"BUCKET"`
	Region   string `mapstructure:"REGION"`
	Endpoint string `mapstructure:"ENDPOINT"` // Optional, for Minio/Custom S3
	Prefix   string `mapstructure:"PREFIX"`
}

type GCSStorageConfig struct {
	Bucket string `mapstructure:"BUCKET"`
	Prefix string `mapstructure:"PREFIX"`
}

type Config struct {
	DatabaseURL string `mapstructure:"DATABASE_URL"`
	WebPort     int    `mapstructure:"WEB_PORT"`

	Log struct {
		Level  string `mapstructure:"LEVEL"`  // info, debug, error, warn
		Format string `mapstructure:"FORMAT"` // text, json
	} `mapstructure:"LOG"`

	Spam struct {
		RBLServers []string `mapstructure:"RBL_SERVERS"`
	} `mapstructure:"SPAM"`

	SMTP         SMTPConfig         `mapstructure:"SMTP"`

	StorageType string `mapstructure:"STORAGE_TYPE"` // "local", "s3", "gcs"
	Compression string `mapstructure:"COMPRESSION"`  // "zstd", "gzip", "none"

	DKIM struct {
		EncryptionKey string `mapstructure:"ENCRYPTION_KEY"`
	} `mapstructure:"DKIM"`

	LocalStorage LocalStorageConfig `mapstructure:"LOCAL_STORAGE"`
	S3Storage    S3StorageConfig    `mapstructure:"S3_STORAGE"`
	GCSStorage   GCSStorageConfig   `mapstructure:"GCS_STORAGE"`
}

func LoadConfig() (*Config, error) {
	viper.SetDefault("LOG.LEVEL", "info")
	viper.SetDefault("LOG.FORMAT", "text")
	viper.SetDefault("SPAM.RBL_SERVERS", []string{"zen.spamhaus.org"})

	viper.SetDefault("WEB_PORT", 8080)
	viper.SetDefault("SMTP.PORTS", []int{25})
	viper.SetDefault("SMTP.DOMAIN", "localhost")
	viper.SetDefault("SMTP.READ_TIMEOUT", 10*time.Second)
	viper.SetDefault("SMTP.WRITE_TIMEOUT", 10*time.Second)
	viper.SetDefault("SMTP.MAX_MESSAGE_SIZE", 1024*1024*50) // 50MB
	viper.SetDefault("SMTP.MAX_RECIPIENTS", 50)

	viper.SetDefault("DKIM.ENCRYPTION_KEY", "")

	viper.SetDefault("STORAGE_TYPE", "local")
	viper.SetDefault("COMPRESSION", "none")
	viper.SetDefault("LOCAL_STORAGE.BASE_PATH", "./data/emails")

	// Try to load .env file
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	if err := viper.ReadInConfig(); err != nil {
		// It's okay if config file is not found
	}

	viper.SetEnvPrefix("MAILEROO")
	viper.AutomaticEnv()
	viper.BindEnv("DATABASE_URL")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Manual override for comma-separated ports in ENV
	if envPorts := viper.GetString("SMTP_PORTS"); envPorts != "" {
		parts := strings.Split(envPorts, ",")
		var ports []int
		for _, p := range parts {
			if pi, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
				ports = append(ports, pi)
			}
		}
		if len(ports) > 0 {
			cfg.SMTP.Ports = ports
		}
	}

	// Manual override for TLS files because nested structs in viper can be tricky with prefixes
	if cert := viper.GetString("SMTP_TLS_CERT_FILE"); cert != "" {
		cfg.SMTP.TLSCertFile = cert
	}
	if key := viper.GetString("SMTP_TLS_KEY_FILE"); key != "" {
		cfg.SMTP.TLSKeyFile = key
	}
	if relay := viper.GetString("SMTP_RELAY"); relay != "" {
		cfg.SMTP.Relay = relay
	}

	return &cfg, nil
}

func BindFlags(fs *pflag.FlagSet) {
	fs.String("database-url", "", "Database connection URL")
	viper.BindPFlag("DATABASE_URL", fs.Lookup("database-url"))

	fs.Int("web-port", 8080, "Web server port")
	viper.BindPFlag("WEB_PORT", fs.Lookup("web-port"))

	fs.String("log-level", "info", "Log level (debug, info, warn, error)")
	viper.BindPFlag("LOG.LEVEL", fs.Lookup("log-level"))

	fs.String("log-format", "text", "Log format (text, json)")
	viper.BindPFlag("LOG.FORMAT", fs.Lookup("log-format"))

	fs.StringSlice("spam-rbl-servers", []string{"zen.spamhaus.org"}, "Spam RBL servers")
	viper.BindPFlag("SPAM.RBL_SERVERS", fs.Lookup("spam-rbl-servers"))

	fs.IntSlice("smtp-ports", []int{25}, "SMTP ports to listen on")
	viper.BindPFlag("SMTP.PORTS", fs.Lookup("smtp-ports"))

	fs.String("smtp-domain", "localhost", "SMTP domain name")
	viper.BindPFlag("SMTP.DOMAIN", fs.Lookup("smtp-domain"))

	fs.Duration("smtp-read-timeout", 10*time.Second, "SMTP read timeout")
	viper.BindPFlag("SMTP.READ_TIMEOUT", fs.Lookup("smtp-read-timeout"))

	fs.Duration("smtp-write-timeout", 10*time.Second, "SMTP write timeout")
	viper.BindPFlag("SMTP.WRITE_TIMEOUT", fs.Lookup("smtp-write-timeout"))

	fs.Int64("smtp-max-message-size", 1024*1024*50, "SMTP max message size in bytes")
	viper.BindPFlag("SMTP.MAX_MESSAGE_SIZE", fs.Lookup("smtp-max-message-size"))

	fs.Int("smtp-max-recipients", 50, "SMTP max recipients per message")
	viper.BindPFlag("SMTP.MAX_RECIPIENTS", fs.Lookup("smtp-max-recipients"))

	fs.String("smtp-tls-cert-file", "", "SMTP TLS certificate file")
	viper.BindPFlag("SMTP.TLS_CERT_FILE", fs.Lookup("smtp-tls-cert-file"))

	fs.String("smtp-tls-key-file", "", "SMTP TLS key file")
	viper.BindPFlag("SMTP.TLS_KEY_FILE", fs.Lookup("smtp-tls-key-file"))

	fs.String("dkim-encryption-key", "", "Base64-encoded 32-byte AES-256 key for encrypting DKIM private keys")
	viper.BindPFlag("DKIM.ENCRYPTION_KEY", fs.Lookup("dkim-encryption-key"))

	fs.String("storage-type", "local", "Storage type (local, s3, gcs)")
	viper.BindPFlag("STORAGE_TYPE", fs.Lookup("storage-type"))

	fs.String("compression", "none", "Compression type (zstd, gzip, none)")
	viper.BindPFlag("COMPRESSION", fs.Lookup("compression"))

	fs.String("local-storage-base-path", "./data/emails", "Local storage base path")
	viper.BindPFlag("LOCAL_STORAGE.BASE_PATH", fs.Lookup("local-storage-base-path"))

	fs.String("s3-storage-bucket", "", "S3 storage bucket")
	viper.BindPFlag("S3_STORAGE.BUCKET", fs.Lookup("s3-storage-bucket"))

	fs.String("s3-storage-region", "", "S3 storage region")
	viper.BindPFlag("S3_STORAGE.REGION", fs.Lookup("s3-storage-region"))

	fs.String("s3-storage-endpoint", "", "S3 storage endpoint")
	viper.BindPFlag("S3_STORAGE.ENDPOINT", fs.Lookup("s3-storage-endpoint"))

	fs.String("s3-storage-prefix", "", "S3 storage prefix")
	viper.BindPFlag("S3_STORAGE.PREFIX", fs.Lookup("s3-storage-prefix"))

	fs.String("gcs-storage-bucket", "", "GCS storage bucket")
	viper.BindPFlag("GCS_STORAGE.BUCKET", fs.Lookup("gcs-storage-bucket"))

	fs.String("gcs-storage-prefix", "", "GCS storage prefix")
	viper.BindPFlag("GCS_STORAGE.PREFIX", fs.Lookup("gcs-storage-prefix"))
}

