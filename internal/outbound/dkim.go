package outbound

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"io"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/emersion/go-msgauth/dkim"
)

type dkimDB interface {
	GetActiveDKIMKey(ctx context.Context, domain string, selector *string) (*models.DKIMKey, error)
}

type loadedKey struct {
	signer   crypto.Signer
	selector string
}

type DKIMSigner struct {
	db     dkimDB
	encKey []byte
}

func NewDKIMSigner(db dkimDB, encKey []byte) *DKIMSigner {
	return &DKIMSigner{db: db, encKey: encKey}
}

func (s *DKIMSigner) Sign(domain string, msg []byte) ([]byte, error) {
	ck, err := s.loadKey(domain)
	if err != nil {
		return nil, err
	}

	opts := &dkim.SignOptions{
		Domain:   domain,
		Selector: ck.selector,
		Signer:   ck.signer,
		HeaderKeys: []string{
			"From", "To", "Subject", "Date", "Message-ID", "Content-Type", "MIME-Version",
		},
	}

	var buf bytes.Buffer
	if err := dkim.Sign(&buf, bytes.NewReader(msg), opts); err != nil {
		return nil, fmt.Errorf("dkim sign: %w", err)
	}
	return buf.Bytes(), nil
}

func (s *DKIMSigner) loadKey(domain string) (*loadedKey, error) {
	record, err := s.db.GetActiveDKIMKey(context.Background(), domain, nil)
	if err != nil {
		return nil, fmt.Errorf("no active dkim key for domain %s: %w", domain, err)
	}

	der, err := DecryptKey(record.KeyData, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt dkim key for %s: %w", domain, err)
	}

	signer, err := parsePrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse dkim key for %s: %w", domain, err)
	}

	return &loadedKey{signer: signer, selector: record.Selector}, nil
}

func parsePrivateKey(der []byte) (crypto.Signer, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse pkcs8: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("key type %T does not implement crypto.Signer", key)
	}
	return signer, nil
}

// EncryptKey encrypts PKCS#8 DER key bytes using AES-256-GCM.
// The returned bytes are nonce || ciphertext.
func EncryptKey(der, encKey []byte) ([]byte, error) {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, der, nil), nil
}

// DecryptKey decrypts AES-256-GCM encrypted key bytes produced by EncryptKey.
func DecryptKey(ciphertext, encKey []byte) ([]byte, error) {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
}
