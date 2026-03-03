package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/colormechadd/maileroo/internal/config"
	"github.com/colormechadd/maileroo/internal/db"
	"github.com/colormechadd/maileroo/internal/mail"
	"github.com/colormechadd/maileroo/internal/outbound"
	"github.com/colormechadd/maileroo/internal/pipeline"
	"github.com/colormechadd/maileroo/internal/smtp"
	"github.com/colormechadd/maileroo/internal/storage"
	"github.com/colormechadd/maileroo/internal/web"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/spf13/cobra"
)

var (
	cfg *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "maileroo",
	Short: "Maileroo is an all-in-one email platform",
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

	slog.Info("Starting Maileroo Monolith...")

	// Initialize Database
	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// Initialize Storage
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
	mailSvc := mail.NewService(database, store, cfg.Compression)

	// Initialize Pipeline
	ingestionPipeline := pipeline.NewPipeline(cfg, database, store, hub, mailSvc)

	// Initialize MTA
	mta := outbound.NewMTA(cfg.SMTP.Domain)

	// Start SMTP servers
	smtpServers, err := smtp.StartServers(cfg.SMTP, database, ingestionPipeline)
	if err != nil {
		slog.Error("failed to initialize SMTP servers", "error", err)
		os.Exit(1)
	}

	for _, s := range smtpServers {
		go func(srv *gosmtp.Server) {
			tlsStatus := "disabled"
			if srv.TLSConfig != nil {
				tlsStatus = "enabled"
			}
			slog.Info("Starting SMTP server", "addr", srv.Addr, "starttls", tlsStatus)

			var err error
			if strings.HasSuffix(srv.Addr, ":465") || strings.HasSuffix(srv.Addr, ":4650") {
				if srv.TLSConfig == nil {
					slog.Error("Implicit TLS requested but no certificates provided. Skipping port.", "addr", srv.Addr)
					return
				}
				slog.Info("Using implicit TLS for port", "addr", srv.Addr)
				err = srv.ListenAndServeTLS()
			} else {
				err = srv.ListenAndServe()
			}

			if err != nil {
				slog.Error("SMTP server failed", "addr", srv.Addr, "error", err)
			}
		}(s)
	}

	// Initialize Web Server
	webServer := web.NewServer(*cfg, database, store, hub, mta, mailSvc)

	// Start Web server (Chi)
	go func() {
		slog.Info("Starting Webmail interface", "port", cfg.WebPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.WebPort), webServer.Routes()); err != nil {
			slog.Error("Web server failed", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down Maileroo...")

	for _, s := range smtpServers {
		s.Close()
	}
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
