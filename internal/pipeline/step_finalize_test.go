package pipeline

import (
	"context"
	"testing"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestFinalize(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}

	emailID := uuid.New()

	t.Run("no filter action → inbox", func(t *testing.T) {
		ictx := &IngestionContext{EmailID: emailID}
		mockDB.On("SetEmailFields", mock.Anything, emailID, false, false, models.StatusInbox).Return(nil).Once()
		status, _, err := Finalize(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
		mockDB.AssertExpectations(t)
	})

	t.Run("archive action", func(t *testing.T) {
		ictx := &IngestionContext{EmailID: emailID, FilterAction: models.FilterActionArchive}
		mockDB.On("SetEmailFields", mock.Anything, emailID, false, false, models.StatusArchived).Return(nil).Once()
		status, _, err := Finalize(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
		mockDB.AssertExpectations(t)
	})

	t.Run("star action", func(t *testing.T) {
		ictx := &IngestionContext{EmailID: emailID, FilterAction: models.FilterActionStar}
		mockDB.On("SetEmailFields", mock.Anything, emailID, false, true, models.StatusInbox).Return(nil).Once()
		status, _, err := Finalize(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
		mockDB.AssertExpectations(t)
	})
}
