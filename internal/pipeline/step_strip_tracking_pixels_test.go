package pipeline

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// rawHTMLMIME wraps an HTML body in a minimal text/html MIME message.
func rawHTMLMIME(body string) []byte {
	return []byte("MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + body)
}

// rawPlainMIME wraps a plain text body in a minimal text/plain MIME message.
func rawPlainMIME(body string) []byte {
	return []byte("MIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\n" + body)
}

func newIngestionCtx(raw []byte) *IngestionContext {
	return &IngestionContext{ID: uuid.New(), RawMessage: raw}
}

// pipelineWithStorage returns a Pipeline wired up with the given MockStorage.
func pipelineWithStorage(s *MockStorage) *Pipeline {
	return &Pipeline{storage: s}
}

// expectOriginalHTMLSave configures mockStorage to expect one Save call for
// the original HTML archive and return nil (success).
func expectOriginalHTMLSave(t *testing.T, s *MockStorage, ictx *IngestionContext) {
	t.Helper()
	expectedKey := ictx.ID.String() + "/original_html"
	s.On("Save", mock.Anything, expectedKey, mock.Anything).Return(nil).Once()
}

func TestStripTrackingPixelsStep_PlainText(t *testing.T) {
	raw := rawPlainMIME("Hello World")
	ictx := newIngestionCtx(raw)

	status, details, err := StripTrackingPixels(context.Background(), &Pipeline{}, ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusSkipped, status)
	assert.Equal(t, raw, ictx.RawMessage, "plain text message should be unchanged")
	assert.Equal(t, 0, details.(map[string]any)["removed"])
}

func TestStripTrackingPixelsStep_HTMLNoPixels(t *testing.T) {
	raw := rawHTMLMIME(`<html><body><img src="https://example.com/banner.png" width="600" height="300"></body></html>`)
	ictx := newIngestionCtx(raw)

	status, details, err := StripTrackingPixels(context.Background(), &Pipeline{}, ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusSkipped, status)
	assert.Equal(t, 0, details.(map[string]any)["removed"])
}

func TestStripTrackingPixelsStep_DimensionPixel(t *testing.T) {
	mockStorage := new(MockStorage)
	raw := rawHTMLMIME(`<html><body><img src="https://ex.com/t.gif" width="1" height="1"><p>Hello</p></body></html>`)
	ictx := newIngestionCtx(raw)
	expectOriginalHTMLSave(t, mockStorage, ictx)
	original := string(ictx.RawMessage)

	status, details, err := StripTrackingPixels(context.Background(), pipelineWithStorage(mockStorage), ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.NotEqual(t, original, string(ictx.RawMessage), "raw message should be rebuilt without the pixel")
	assert.NotContains(t, string(ictx.RawMessage), "ex.com/t.gif")

	m := details.(map[string]any)
	assert.Equal(t, 1, m["removed"])
	assert.Equal(t, ictx.ID.String()+"/original_html", m["original_html"])
	mockStorage.AssertExpectations(t)
}

func TestStripTrackingPixelsStep_StyleDimensionPixel(t *testing.T) {
	mockStorage := new(MockStorage)
	raw := rawHTMLMIME(`<html><body><img src="https://ex.com/t.gif" style="width:1px;height:1px;"></body></html>`)
	ictx := newIngestionCtx(raw)
	expectOriginalHTMLSave(t, mockStorage, ictx)

	status, details, err := StripTrackingPixels(context.Background(), pipelineWithStorage(mockStorage), ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.NotContains(t, string(ictx.RawMessage), "ex.com/t.gif")
	m := details.(map[string]any)
	assert.Equal(t, 1, m["removed"])
	assert.NotEmpty(t, m["original_html"])
	mockStorage.AssertExpectations(t)
}

func TestStripTrackingPixelsStep_DomainBlocklist(t *testing.T) {
	mockStorage := new(MockStorage)
	raw := rawHTMLMIME(`<html><body><img src="https://sendgrid.net/open.gif" width="100" height="100"></body></html>`)
	ictx := newIngestionCtx(raw)
	expectOriginalHTMLSave(t, mockStorage, ictx)

	status, details, err := StripTrackingPixels(context.Background(), pipelineWithStorage(mockStorage), ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.NotContains(t, string(ictx.RawMessage), "sendgrid.net")
	assert.Equal(t, 1, details.(map[string]any)["removed"])
	mockStorage.AssertExpectations(t)
}

func TestStripTrackingPixelsStep_PreservesNonTrackingContent(t *testing.T) {
	mockStorage := new(MockStorage)
	raw := rawHTMLMIME(
		`<html><body>` +
			`<img src="https://ex.com/t.gif" width="1" height="1">` +
			`<p>Important content</p>` +
			`<img src="https://ex.com/banner.png" width="600" height="300">` +
			`</body></html>`,
	)
	ictx := newIngestionCtx(raw)
	expectOriginalHTMLSave(t, mockStorage, ictx)

	status, details, err := StripTrackingPixels(context.Background(), pipelineWithStorage(mockStorage), ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusPass, status)

	rebuilt := string(ictx.RawMessage)
	assert.NotContains(t, rebuilt, "t.gif", "tracking pixel should be removed")
	assert.Contains(t, rebuilt, "Important content", "body text should be preserved")
	assert.Contains(t, rebuilt, "banner.png", "normal image should be preserved")
	assert.Equal(t, 1, details.(map[string]any)["removed"])
	mockStorage.AssertExpectations(t)
}

func TestStripTrackingPixelsStep_MultiplePixels(t *testing.T) {
	mockStorage := new(MockStorage)
	raw := rawHTMLMIME(
		`<html><body>` +
			`<img src="https://ex.com/a.gif" width="1" height="1">` +
			`<img src="https://sendgrid.net/b.gif" width="100" height="100">` +
			`<p>Content</p>` +
			`</body></html>`,
	)
	ictx := newIngestionCtx(raw)
	expectOriginalHTMLSave(t, mockStorage, ictx)

	status, details, err := StripTrackingPixels(context.Background(), pipelineWithStorage(mockStorage), ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusPass, status)

	rebuilt := string(ictx.RawMessage)
	assert.NotContains(t, rebuilt, "a.gif")
	assert.NotContains(t, rebuilt, "sendgrid.net")
	assert.Contains(t, rebuilt, "Content")

	m := details.(map[string]any)
	assert.Equal(t, 2, m["removed"])
	assert.Len(t, m["pixels"], 2)
	assert.NotEmpty(t, m["original_html"])
	mockStorage.AssertExpectations(t)
}

func TestStripTrackingPixelsStep_StorageFailureIsNonFatal(t *testing.T) {
	mockStorage := new(MockStorage)
	raw := rawHTMLMIME(`<html><body><img src="https://ex.com/t.gif" width="1" height="1"></body></html>`)
	ictx := newIngestionCtx(raw)
	// Storage fails — the pixel should still be stripped.
	mockStorage.On("Save", mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError).Once()

	status, details, err := StripTrackingPixels(context.Background(), pipelineWithStorage(mockStorage), ictx)

	require.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.NotContains(t, string(ictx.RawMessage), "ex.com/t.gif", "pixel should still be stripped")
	m := details.(map[string]any)
	assert.Equal(t, 1, m["removed"])
	assert.Empty(t, m["original_html"], "key should be empty when storage failed")
	mockStorage.AssertExpectations(t)
}
