package pipeline

import (
	"context"
	"testing"

	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestFinalize(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}

	emailID := uuid.New()
	ictx := &IngestionContext{
		EmailID: emailID,
	}

	t.Run("successful finalize", func(t *testing.T) {
		mockDB.On("SetEmailStatus", mock.Anything, emailID, models.StatusInbox).Return(nil).Once()
		status, _, err := Finalize(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
		mockDB.AssertExpectations(t)
	})
}
