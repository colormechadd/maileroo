package outbound

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func testEncKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}

func testKeyDER(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	return der
}

// mock

type mockDKIMDB struct {
	key *models.DKIMKey
	err error
}

func (m *mockDKIMDB) GetActiveDKIMKey(_ context.Context, _ string, _ *string) (*models.DKIMKey, error) {
	return m.key, m.err
}

// EncryptKey / DecryptKey

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	encKey := testEncKey(t)
	plaintext := []byte("some pkcs8 bytes")

	ciphertext, err := EncryptKey(plaintext, encKey)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	got, err := DecryptKey(ciphertext, encKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestDecryptKey_WrongKey(t *testing.T) {
	encKey := testEncKey(t)
	ciphertext, err := EncryptKey([]byte("secret"), encKey)
	require.NoError(t, err)

	_, err = DecryptKey(ciphertext, testEncKey(t))
	assert.Error(t, err)
}

func TestDecryptKey_TamperedCiphertext(t *testing.T) {
	encKey := testEncKey(t)
	ciphertext, err := EncryptKey([]byte("secret"), encKey)
	require.NoError(t, err)

	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err = DecryptKey(ciphertext, encKey)
	assert.Error(t, err)
}

func TestDecryptKey_TooShort(t *testing.T) {
	_, err := DecryptKey([]byte("short"), testEncKey(t))
	assert.Error(t, err)
}

func TestEncryptKey_ProducesUniqueNonces(t *testing.T) {
	encKey := testEncKey(t)
	plaintext := []byte("same plaintext")

	a, err := EncryptKey(plaintext, encKey)
	require.NoError(t, err)
	b, err := EncryptKey(plaintext, encKey)
	require.NoError(t, err)

	assert.NotEqual(t, a, b, "each encryption should use a unique nonce")
}

// DKIMSigner.Sign

var testMessage = []byte("" +
	"From: sender@example.com\r\n" +
	"To: rcpt@other.com\r\n" +
	"Subject: Test\r\n" +
	"Date: Mon, 1 Jan 2024 00:00:00 +0000\r\n" +
	"Message-ID: <test@example.com>\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"Hello\r\n")

func makeSigner(t *testing.T) (*DKIMSigner, []byte) {
	t.Helper()
	encKey := testEncKey(t)
	der := testKeyDER(t)
	encrypted, err := EncryptKey(der, encKey)
	require.NoError(t, err)

	db := &mockDKIMDB{key: &models.DKIMKey{
		Domain:   "example.com",
		Selector: "mailaroo",
		KeyData:  encrypted,
		IsActive: true,
	}}
	return NewDKIMSigner(db, encKey), encrypted
}

func TestSign_ProducesDKIMSignatureHeader(t *testing.T) {
	signer, _ := makeSigner(t)

	signed, err := signer.Sign("example.com", testMessage)
	require.NoError(t, err)
	assert.True(t, bytes.Contains(signed, []byte("DKIM-Signature:")))
}

func TestSign_SignatureContainsDomainAndSelector(t *testing.T) {
	signer, _ := makeSigner(t)

	signed, err := signer.Sign("example.com", testMessage)
	require.NoError(t, err)

	s := string(signed)
	assert.True(t, strings.Contains(s, "d=example.com"))
	assert.True(t, strings.Contains(s, "s=mailaroo"))
}

func TestSign_OriginalBodyPreserved(t *testing.T) {
	signer, _ := makeSigner(t)

	signed, err := signer.Sign("example.com", testMessage)
	require.NoError(t, err)
	assert.True(t, bytes.Contains(signed, []byte("Hello\r\n")))
}

func TestSign_DBError(t *testing.T) {
	db := &mockDKIMDB{err: errors.New("db down")}
	signer := NewDKIMSigner(db, testEncKey(t))

	_, err := signer.Sign("example.com", testMessage)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestSign_SignatureVerifies(t *testing.T) {
	encKey := testEncKey(t)
	der := testKeyDER(t)

	privKey, err := x509.ParsePKCS8PrivateKey(der)
	require.NoError(t, err)
	pubDER, err := x509.MarshalPKIXPublicKey(privKey.(*rsa.PrivateKey).Public())
	require.NoError(t, err)
	dnsValue := fmt.Sprintf("v=DKIM1; k=rsa; p=%s", base64.StdEncoding.EncodeToString(pubDER))

	encrypted, err := EncryptKey(der, encKey)
	require.NoError(t, err)

	db := &mockDKIMDB{key: &models.DKIMKey{
		Domain:   "example.com",
		Selector: "mailaroo",
		KeyData:  encrypted,
		IsActive: true,
	}}
	signer := NewDKIMSigner(db, encKey)

	signed, err := signer.Sign("example.com", testMessage)
	require.NoError(t, err)

	verifications, err := dkim.VerifyWithOptions(bytes.NewReader(signed), &dkim.VerifyOptions{
		LookupTXT: func(domain string) ([]string, error) {
			if domain == "mailaroo._domainkey.example.com" {
				return []string{dnsValue}, nil
			}
			return nil, fmt.Errorf("unexpected lookup: %s", domain)
		},
	})
	require.NoError(t, err)
	require.Len(t, verifications, 1)
	assert.NoError(t, verifications[0].Err)
}

func TestSign_WrongEncryptionKey(t *testing.T) {
	encKey := testEncKey(t)
	encrypted, err := EncryptKey(testKeyDER(t), encKey)
	require.NoError(t, err)

	db := &mockDKIMDB{key: &models.DKIMKey{
		Domain:   "example.com",
		Selector: "mailaroo",
		KeyData:  encrypted,
		IsActive: true,
	}}

	signer := NewDKIMSigner(db, testEncKey(t)) // different key
	_, err = signer.Sign("example.com", testMessage)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}
