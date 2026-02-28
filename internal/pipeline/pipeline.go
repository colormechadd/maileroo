package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"github.com/colormechadd/maileroo/internal/config"
	"github.com/colormechadd/maileroo/internal/db"
	"github.com/colormechadd/maileroo/internal/storage"
	"github.com/colormechadd/maileroo/pkg/models"
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

type Step func(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error)

type Pipeline struct {
	cfg     *config.Config
	db      db.PipelineDB
	storage storage.Storage
	steps   []struct {
		name string
		fn   Step
	}
}

func NewPipeline(cfg *config.Config, db db.PipelineDB, storage storage.Storage) *Pipeline {
	p := &Pipeline{
		cfg:     cfg,
		db:      db,
		storage: storage,
	}

	// Explicitly define the pipeline order
	p.steps = []struct {
		name string
		fn   Step
	}{
		{"validate_sender", ValidateSender},
		{"spam", ValidateRBL},
		{"block", CheckBlockingRules},
		{"deliver", Deliver},
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
}

func (p *Pipeline) Process(ctx context.Context, ictx *IngestionContext) error {
	slog.Info("processing ingestion", "ingestion_id", ictx.ID, "from", ictx.FromAddress, "mailbox_id", ictx.TargetMailboxID)

	// Create ingestion record
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

	// Run all steps in the defined order
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
	// Final status update
	return p.db.UpdateIngestionStatus(ctx, ictx.ID, "accepted")
}

func (p *Pipeline) runStep(ctx context.Context, ictx *IngestionContext, name string, fn Step) StepStatus {
	slog.Debug("starting pipeline step", "ingestion_id", ictx.ID, "step", name)
	start := time.Now()
	status, details, err := fn(ctx, p, ictx)
	duration := time.Since(start)

	l := slog.With("ingestion_id", ictx.ID, "step", name, "status", status, "duration_ms", duration.Milliseconds())
	if err != nil {
		l.Error("step execution error", "error", err)
	} else {
		l.Debug("step execution completed")
	}

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
