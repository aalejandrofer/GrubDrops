package store

import (
	"bytes"
	"fmt"
	"io"

	"filippo.io/age"
)

type Cryptor struct {
	identity  *age.X25519Identity
	recipient *age.X25519Recipient
}

func NewCryptor(secret string) (*Cryptor, error) {
	id, err := age.ParseX25519Identity(secret)
	if err != nil {
		return nil, fmt.Errorf("parse age identity: %w", err)
	}
	return &Cryptor{
		identity:  id,
		recipient: id.Recipient(),
	}, nil
}

func (c *Cryptor) Encrypt(plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, c.recipient)
	if err != nil {
		return nil, fmt.Errorf("age encrypt: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Cryptor) Decrypt(ct []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ct), c.identity)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}
	return io.ReadAll(r)
}
