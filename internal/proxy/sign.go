package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"

	"golang.org/x/crypto/hkdf"
)

const proxyImageLabel = "maileroo-proxy-image"

// DeriveKey derives a stable 32-byte signing key from csrfKey using
// HKDF-SHA256 with a fixed info label. The result is independent of any
// specific URL and is safe to reuse across all proxy signing calls.
func DeriveKey(csrfKey []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, csrfKey, nil, []byte(proxyImageLabel))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

// SignURL returns the full relative proxy path for rawURL, including a
// 16-byte HMAC-SHA256 signature encoded as base64url.
func SignURL(key []byte, rawURL string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(rawURL))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
	encodedURL := base64.RawURLEncoding.EncodeToString([]byte(rawURL))
	return "/proxy/image?url=" + encodedURL + "&sig=" + sig
}

// VerifyURL reports whether sig is a valid HMAC-SHA256 signature for rawURL
// under key. Uses constant-time comparison to prevent timing attacks.
func VerifyURL(key []byte, rawURL, sig string) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(rawURL))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
	return hmac.Equal([]byte(sig), []byte(expected))
}
