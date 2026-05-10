package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	gosmtp "github.com/emersion/go-smtp"

	"github.com/colormechadd/mailaroo/internal/config"
	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/internal/mail"
	"github.com/colormechadd/mailaroo/internal/outbound"
	"github.com/colormechadd/mailaroo/internal/pipeline"
	"github.com/colormechadd/mailaroo/internal/proxy"
	"github.com/colormechadd/mailaroo/internal/rspamd"
	"github.com/colormechadd/mailaroo/internal/smtp"
	"github.com/colormechadd/mailaroo/internal/storage"
	"github.com/colormechadd/mailaroo/internal/trashpurge"
	"github.com/colormechadd/mailaroo/internal/web"
	"github.com/spf13/cobra"
)

var (
	cfg *config.Config
)

func init() {
	config.BindFlags(rootCmd.PersistentFlags())

	// Hide config flags from the brief usage shown on errors.
	config.HideFlags(rootCmd.PersistentFlags())

	// When --help is explicitly requested, show all flags including the hidden ones.
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		config.UnhideFlags(cmd.PersistentFlags())
		defaultHelp(cmd, args)
	})
}

var rootCmd = &cobra.Command{
	Use:   "mailaroo",
	Short: "MAILAROO is an all-in-one email platform",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		var err error
		cfg, err = config.LoadConfig()
		if err != nil {
			slog.Error("failed to load configuration", "error", err)
			os.Exit(1)
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Default behavior is to start the server
		runServe()
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		slog.Error("command execution failed", "error", err)
		os.Exit(1)
	}
}

func runServe() {
	initLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting MAILAROO Monolith...")

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	var store storage.Storage
	switch cfg.StorageType {
	case "local":
		store, err = storage.NewLocalStorage(cfg.LocalStorage)
	case "s3":
		store, err = storage.NewS3Storage(ctx, cfg.S3Storage)
	case "gcs":
		store, err = storage.NewGCSStorage(ctx, cfg.GCSStorage)
	default:
		slog.Error("unknown storage type", "type", cfg.StorageType)
		os.Exit(1)
	}
	if err != nil {
		slog.Error("failed to initialize storage", "error", err)
		os.Exit(1)
	}

	hub := web.NewHub()

	csrfKey, err := base64.StdEncoding.DecodeString(cfg.Web.CSRFAuthKey)
	if err != nil || len(csrfKey) != 32 {
		slog.Error("WEB_CSRF_AUTH_KEY must be a base64-encoded 32-byte key")
		os.Exit(1)
	}
	proxyKey, err := proxy.DeriveKey(csrfKey)
	if err != nil {
		slog.Error("failed to derive proxy signing key", "error", err)
		os.Exit(1)
	}
	signURL := func(rawURL string) string { return proxy.SignURL(proxyKey, rawURL) }

	mailSvc := mail.NewService(database, store, cfg.Compression, signURL)
	rspamdClient := rspamd.NewClient(cfg.Spam.RspamdURL)
	ingestionPipeline := pipeline.NewPipeline(cfg, database, store, hub, mailSvc, rspamdClient)

	var dkimSigner *outbound.DKIMSigner
	if cfg.DKIM.EncryptionKey != "" {
		encKey, err := base64.StdEncoding.DecodeString(cfg.DKIM.EncryptionKey)
		if err != nil || len(encKey) != 32 {
			slog.Error("MAILAROO_DKIM_ENCRYPTION_KEY must be a base64-encoded 32-byte value")
			os.Exit(1)
		}
		dkimSigner = outbound.NewDKIMSigner(database, encKey)
	}
	mta := outbound.NewMTA(cfg.SMTP.Domain, cfg.SMTP.Relay, dkimSigner)

	smtpServers, err := smtp.CreateServers(cfg.SMTP, cfg.RateLimit, database, database, ingestionPipeline)
	if err != nil {
		slog.Error("failed to initialize SMTP servers", "error", err)
		os.Exit(1)
	}

	webServer := web.NewServer(web.ServerConfig{
		Config:      *cfg,
		DB:          database,
		RateLimitDB: database,
		Storage:     store,
		Hub:         hub,
		Sender:      mta,
		Mail:        mailSvc,
		Rspamd:      rspamdClient,
	})

	// Services
	go runSmtp(ctx, smtpServers)
	go runOutboundQueue(ctx, database, mta)
	go runRateLimitCleaner(ctx, database)
	go runTrashPurge(ctx, database, store)
	go runWebServer(ctx, cfg, webServer)

	<-ctx.Done()
	slog.Info("Shutting down MAILAROO...")
}

func runSmtp(ctx context.Context, servers []*gosmtp.Server) {
	slog.Info("Starting SMTP servers")
	for _, s := range servers {
		go func(s *gosmtp.Server) {
			smtp.StartServer(s)
		}(s)
	}
	<-ctx.Done()

	for _, s := range servers {
		s.Close()
	}
	slog.Info("Stopped SMTP servers")

}

func runOutboundQueue(ctx context.Context, database *db.DB, mta *outbound.MTA) {
	slog.Info("Starting outbound queue")
	outbound.NewQueue(database, mta).Start(ctx)
	<-ctx.Done()
	slog.Info("Stopped outbound queue")
}

func runRateLimitCleaner(ctx context.Context, database *db.DB) {
	slog.Info("Starting rate limit cleaner")
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := database.PurgeExpiredRateLimitData(context.Background()); err != nil {
				slog.Error("failed to purge expired rate limit data", "error", err)
			}
		case <-ctx.Done():
			slog.Info("Stopped rate limit cleaner")
			return
		}
	}
}

func runTrashPurge(ctx context.Context, database *db.DB, store storage.Storage) {
	slog.Info("Starting trash purge")
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	trashpurge.Run(database, store)
	for {
		select {
		case <-ticker.C:
			trashpurge.Run(database, store)
		case <-ctx.Done():
			slog.Info("Stopped rate limit cleaner")
			return
		}
	}
}

func runWebServer(ctx context.Context, cfg *config.Config, webServer *web.Server) {
	slog.Info("Starting web server")

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.WebPort),
		Handler: webServer.Routes(),
	}

	go func() {
		var err error
		if len(cfg.Web.CertFile) > 0 && len(cfg.Web.CertKeyFile) > 0 {
			slog.Info("Starting Secure Webmail interface", "port", cfg.WebPort)
			err = srv.ListenAndServeTLS(cfg.Web.CertFile, cfg.Web.CertKeyFile)
		} else {
			slog.Info("Starting Webmail interface", "port", cfg.WebPort)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			slog.Error("Web server failed", "error", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Web server shutdown failed", "error", err)
	}
	slog.Info("Stopped web server")

}

func initLogger(cfg *config.Config) {
	var level slog.Level
	switch strings.ToLower(cfg.Log.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var handler slog.Handler
	if strings.ToLower(cfg.Log.Format) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
}
