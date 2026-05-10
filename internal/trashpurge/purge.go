package trashpurge

import (
	"context"
	"log/slog"
	"time"

	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/internal/storage"
)

func Run(database db.PurgeDB, store storage.Storage) {
	ctx := context.Background()
	cutoff := time.Now().AddDate(0, 0, -30)
	emails, err := database.GetEmailsForPurge(ctx, cutoff)
	if err != nil {
		slog.Error("trash purge: failed to list emails", "error", err)
		return
	}
	for _, email := range emails {
		attachmentKeys, err := database.GetAttachmentStorageKeysByEmailID(ctx, email.ID)
		if err != nil {
			slog.Error("trash purge: failed to get attachment keys", "email_id", email.ID, "error", err)
			continue
		}
		failed := false
		if email.StorageKey != "" {
			if err := store.Delete(ctx, email.StorageKey); err != nil {
				slog.Error("trash purge: failed to delete email body", "email_id", email.ID, "key", email.StorageKey, "error", err)
				failed = true
			}
		}
		for _, key := range attachmentKeys {
			if err := store.Delete(ctx, key); err != nil {
				slog.Error("trash purge: failed to delete attachment", "email_id", email.ID, "key", key, "error", err)
				failed = true
			}
		}
		if failed {
			continue
		}
		if err := database.MarkEmailPurged(ctx, email.ID); err != nil {
			slog.Error("trash purge: failed to mark email purged", "email_id", email.ID, "error", err)
		} else {
			slog.Info("trash purge: permanently deleted email storage", "email_id", email.ID)
		}
	}
}
