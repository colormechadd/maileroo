package config

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type SMTPConfig struct {
	Ports          []int         `mapstructure:"PORTS"`
	TLSCertFile    string        `mapstructure:"TLS_CERT_FILE"`
	TLSKeyFile     string        `mapstructure:"TLS_KEY_FILE"`
	Domain         string        `mapstructure:"DOMAIN"`
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

	SMTP SMTPConfig `mapstructure:"SMTP"`

	StorageType string `mapstructure:"STORAGE_TYPE"` // "local", "s3", "gcs"
	Compression string `mapstructure:"COMPRESSION"`  // "zstd", "gzip", "none"

	LocalStorage LocalStorageConfig `mapstructure:"LOCAL_STORAGE"`
	S3Storage    S3StorageConfig    `mapstructure:"S3_STORAGE"`
	GCSStorage   GCSStorageConfig   `mapstructure:"GCS_STORAGE"`
}

func LoadConfig() (*Config, error) {
	viper.SetDefault("LOG.LEVEL", "info")
	viper.SetDefault("LOG.FORMAT", "text")
	viper.SetDefault("SPAM.RBL_SERVERS", []string{"zen.spamhaus.org"})

	viper.SetDefault("WEB_PORT", 8080)
	viper.SetDefault("SMTP.PORTS", []int{25, 587, 465, 2525})
	viper.SetDefault("SMTP.DOMAIN", "localhost")
	viper.SetDefault("SMTP.READ_TIMEOUT", 10*time.Second)
	viper.SetDefault("SMTP.WRITE_TIMEOUT", 10*time.Second)
	viper.SetDefault("SMTP.MAX_MESSAGE_SIZE", 1024*1024*50) // 50MB
	viper.SetDefault("SMTP.MAX_RECIPIENTS", 50)

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

	// Explicitly bind nested keys if needed, though AutomaticEnv + Replacer should work.
	// But let's handle the manual override for common ones if they fail.

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

	return &cfg, nil
}
