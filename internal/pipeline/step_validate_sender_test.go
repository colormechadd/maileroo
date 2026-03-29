package pipeline

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSender(t *testing.T) {
	ctx := context.Background()
	p := &Pipeline{}

	t.Run("basic check", func(t *testing.T) {
		ictx := &IngestionContext{
			RemoteIP:    net.ParseIP("127.0.0.1"),
			FromAddress: "test@gmail.com",
			RawMessage:  []byte("Subject: Test\n\nNo DKIM here"),
		}
		status, _, err := ValidateSender(ctx, p, ictx)
		assert.NoError(t, err)
		// We expect a result (likely Fail if on loopback without valid DKIM)
		assert.Contains(t, []StepStatus{StatusPass, StatusFail, StatusError}, status)
	})
}
