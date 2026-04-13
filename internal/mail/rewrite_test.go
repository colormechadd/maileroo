package mail

import (
	"strings"
	"testing"
)

func fakeSign(rawURL string) string {
	return "/proxy/image?url=" + rawURL + "&sig=FAKE"
}

// ---- rewriteForPrivacy (URL proxying) ----

func TestRewriteForPrivacy_ProxiesSrc(t *testing.T) {
	input := `<html><body><img src="https://example.com/banner.png"></body></html>`
	got := rewriteForPrivacy(input, fakeSign)
	if !strings.Contains(got, "/proxy/image?url=https://example.com/banner.png") {
		t.Errorf("expected proxied src, got: %s", got)
	}
}

func TestRewriteForPrivacy_LeavesDataURI(t *testing.T) {
	input := `<html><body><img src="data:image/png;base64,abc123"></body></html>`
	got := rewriteForPrivacy(input, fakeSign)
	if !strings.Contains(got, `src="data:image/png;base64,abc123"`) {
		t.Errorf("data URI should be unchanged, got: %s", got)
	}
}

func TestRewriteForPrivacy_LeavesCIDUntouched(t *testing.T) {
	input := `<html><body><img src="cid:part1@example.com"></body></html>`
	got := rewriteForPrivacy(input, fakeSign)
	if !strings.Contains(got, `src="cid:part1@example.com"`) {
		t.Errorf("cid: src should be unchanged, got: %s", got)
	}
}

func TestRewriteForPrivacy_RewritesSrcset(t *testing.T) {
	input := `<html><body><img src="https://example.com/img.png" srcset="https://example.com/img.png 1x, https://example.com/img@2x.png 2x"></body></html>`
	got := rewriteForPrivacy(input, fakeSign)
	if !strings.Contains(got, "/proxy/image?url=https://example.com/img.png") {
		t.Errorf("srcset 1x entry should be rewritten, got: %s", got)
	}
	if !strings.Contains(got, "/proxy/image?url=https://example.com/img@2x.png") {
		t.Errorf("srcset 2x entry should be rewritten, got: %s", got)
	}
}

func TestRewriteForPrivacy_RewritesInlineStyleURL(t *testing.T) {
	input := `<html><body><div style="background-image: url(https://example.com/bg.png)">content</div></body></html>`
	got := rewriteForPrivacy(input, fakeSign)
	if !strings.Contains(got, "/proxy/image?url=https://example.com/bg.png") {
		t.Errorf("inline style url() should be rewritten, got: %s", got)
	}
}

func TestRewriteForPrivacy_RewritesStyleBlock(t *testing.T) {
	input := `<html><head><style>.foo { background: url(https://example.com/bg.png); }</style></head><body></body></html>`
	got := rewriteForPrivacy(input, fakeSign)
	if !strings.Contains(got, "/proxy/image?url=https://example.com/bg.png") {
		t.Errorf("<style> block url() should be rewritten, got: %s", got)
	}
}

func TestRewriteForPrivacy_LeavesNonHTTPCSSURLUntouched(t *testing.T) {
	input := `<html><head><style>.foo { background: url(data:image/png;base64,abc); }</style></head><body></body></html>`
	got := rewriteForPrivacy(input, fakeSign)
	if strings.Contains(got, "/proxy/image") {
		t.Errorf("non-http CSS url() should be unchanged, got: %s", got)
	}
}

// ---- StripTrackingPixelsFromHTML (ingest-time pixel removal) ----

func TestStripTrackingPixels_ByHTMLDimension(t *testing.T) {
	input := `<html><body><img src="https://example.com/t.gif" width="1" height="1"><p>hello</p></body></html>`
	got, pixels := StripTrackingPixelsFromHTML(input)
	if strings.Contains(got, "t.gif") {
		t.Errorf("1x1 pixel should be removed, got: %s", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("surrounding content should be preserved, got: %s", got)
	}
	if len(pixels) != 1 || pixels[0].Method != "dimension" {
		t.Errorf("expected one dimension pixel, got: %v", pixels)
	}
}

func TestStripTrackingPixels_ByInlineStyle(t *testing.T) {
	input := `<html><body><img src="https://example.com/t.gif" style="width:1px;height:1px;"></body></html>`
	got, pixels := StripTrackingPixelsFromHTML(input)
	if strings.Contains(got, "t.gif") {
		t.Errorf("style-sized pixel should be removed, got: %s", got)
	}
	if len(pixels) != 1 || pixels[0].Method != "style_dimension" {
		t.Errorf("expected one style_dimension pixel, got: %v", pixels)
	}
}

func TestStripTrackingPixels_ByDomain(t *testing.T) {
	input := `<html><body><img src="https://sendgrid.net/open.gif" width="100" height="100"></body></html>`
	got, pixels := StripTrackingPixelsFromHTML(input)
	if strings.Contains(got, "sendgrid.net") {
		t.Errorf("blocked domain pixel should be removed, got: %s", got)
	}
	if len(pixels) != 1 || pixels[0].Method != "domain" {
		t.Errorf("expected one domain pixel, got: %v", pixels)
	}
}

func TestStripTrackingPixels_NormalImageUntouched(t *testing.T) {
	input := `<html><body><img src="https://example.com/banner.png" width="600" height="200"></body></html>`
	got, pixels := StripTrackingPixelsFromHTML(input)
	if !strings.Contains(got, "banner.png") {
		t.Errorf("normal image should be preserved, got: %s", got)
	}
	if len(pixels) != 0 {
		t.Errorf("expected no pixels removed, got: %v", pixels)
	}
}

func TestStripTrackingPixels_ReturnsNoneWhenClean(t *testing.T) {
	input := `<html><body><p>Hello World</p></body></html>`
	_, pixels := StripTrackingPixelsFromHTML(input)
	if len(pixels) != 0 {
		t.Errorf("expected no pixels, got: %v", pixels)
	}
}
