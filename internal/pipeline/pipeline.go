package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"github.com/colormechadd/mailaroo/internal/config"
	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/internal/mail"
	"github.com/colormechadd/mailaroo/internal/rspamd"
	"github.com/colormechadd/mailaroo/internal/storage"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

type StepStatus string

const (
	StatusPass    StepStatus = "pass"
	StatusFail    StepStatus = "fail"
	StatusNeutral StepStatus = "neutral"
	StatusError   StepStatus = "error"
	StatusSkipped StepStatus = "skipped"
	StatusNone    StepStatus = "none"
)

type Event struct {
	UserID    uuid.UUID
	MailboxID uuid.UUID
	Type      string
}

type Broadcaster interface {
	Broadcast(event Event)
}

type Step func(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error)

type Pipeline struct {
	cfg     *config.Config
	db      db.PipelineDB
	storage storage.Storage
	hub     Broadcaster
	mail    *mail.Service
	rspamd  *rspamd.Client
	steps   []struct {
		name string
		fn   Step
	}
}

func NewPipeline(cfg *config.Config, db db.PipelineDB, storage storage.Storage, hub Broadcaster, mailSvc *mail.Service, rspamdClient *rspamd.Client) *Pipeline {
	p := &Pipeline{
		cfg:     cfg,
		db:      db,
		storage: storage,
		hub:     hub,
		mail:    mailSvc,
		rspamd:  rspamdClient,
	}

	p.steps = []struct {
		name string
		fn   Step
	}{
		{"strip_tracking_pixels", StripTrackingPixels},
		{"deliver", Deliver},
		{"parse_dsn", ParseDSN},
		{"validate_sender", ValidateSender},
		{"spam", ValidateRBL},
		{"check_spam", CheckSpam},
		{"block", CheckBlockingRules},
		{"apply_filter_rules", ApplyFilterRules},
		{"finalize", Finalize},
		{"notify", Notify},
	}

	return p
}

type IngestionContext struct {
	ID               uuid.UUID
	RemoteIP         net.IP
	FromAddress      string
	ToAddresses      []string
	RawMessage       []byte
	TargetMailboxID  uuid.UUID
	AddressMappingID uuid.UUID
	StorageKey       string
	EmailID          uuid.UUID
	MatchedFilterRuleID *uuid.UUID
	FilterAction        string
}

func (p *Pipeline) Process(ctx context.Context, ictx *IngestionContext) error {
	slog.Info("processing ingestion", "ingestion_id", ictx.ID, "from", ictx.FromAddress, "mailbox_id", ictx.TargetMailboxID)

	ingestion := &models.Ingestion{
		ID:          ictx.ID,
		FromAddress: &ictx.FromAddress,
		Status:      "processing",
	}
	if len(ictx.ToAddresses) > 0 {
		ingestion.ToAddress = &ictx.ToAddresses[0]
	}

	if err := p.db.CreateIngestion(ctx, ingestion); err != nil {
		slog.Error("failed to create ingestion record", "ingestion_id", ictx.ID, "error", err)
		return err
	}

	for _, step := range p.steps {
		status := p.runStep(ctx, ictx, step.name, step.fn)
		if status == StatusFail {
			slog.Warn("ingestion rejected", "ingestion_id", ictx.ID, "step", step.name)
			return p.db.UpdateIngestionStatus(ctx, ictx.ID, "rejected")
		}
		if status == StatusError {
			slog.Error("ingestion failed", "ingestion_id", ictx.ID, "step", step.name)
			return p.db.UpdateIngestionStatus(ctx, ictx.ID, "failed")
		}
	}

	slog.Info("ingestion completed successfully", "ingestion_id", ictx.ID)
	return p.db.UpdateIngestionStatus(ctx, ictx.ID, "accepted")
}

func (p *Pipeline) runStep(ctx context.Context, ictx *IngestionContext, name string, fn Step) StepStatus {
	start := time.Now()
	status, details, err := fn(ctx, p, ictx)
	duration := time.Since(start)

	detailsJSON, _ := json.Marshal(details)
	if err != nil && details == nil {
		detailsJSON, _ = json.Marshal(map[string]string{"error": err.Error()})
	}

	step := &models.IngestionStep{
		ID:          uuid.New(),
		IngestionID: ictx.ID,
		StepName:    name,
		Status:      string(status),
		Details:     detailsJSON,
		DurationMS:  int(duration.Milliseconds()),
	}

	if dbErr := p.db.CreateIngestionStep(ctx, step); dbErr != nil {
		slog.Error("failed to record ingestion step", "ingestion_id", ictx.ID, "step", name, "error", dbErr)
	}

	return status
}
