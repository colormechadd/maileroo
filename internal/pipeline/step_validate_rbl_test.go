package pipeline

import (
	"context"
	"net"
	"testing"

	"github.com/colormechadd/mailaroo/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestValidateRBL(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.Spam.RBLServers = []string{"zen.spamhaus.org"}
	p := &Pipeline{cfg: cfg}

	t.Run("skipped if no ip", func(t *testing.T) {
		ictx := &IngestionContext{RemoteIP: nil}
		status, _, err := ValidateRBL(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusSkipped, status)
	})

	t.Run("loopback ip behavior", func(t *testing.T) {
		ictx := &IngestionContext{RemoteIP: net.ParseIP("127.0.0.1")}
		status, _, err := ValidateRBL(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Contains(t, []StepStatus{StatusPass, StatusFail}, status)
	})
}
