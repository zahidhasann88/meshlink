package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/zahidhasann88/meshlink/internal/fsperm"
)

type Identity struct{ key ed25519.PrivateKey }

func (i *Identity) Valid() bool { return i != nil && len(i.key) == ed25519.PrivateKeySize }

func (i *Identity) String() string {
	if !i.Valid() {
		return "Identity(invalid)"
	}
	return "Identity(" + i.ID() + ")"
}
func (i *Identity) GoString() string { return i.String() }

func Generate() (*Identity, error) {
	_, k, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	return &Identity{key: k}, nil
}
func (i *Identity) ID() string { return ID(i.PublicKey()) }
func (i *Identity) PublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), i.key.Public().(ed25519.PublicKey)...)
}

func (i *Identity) PrivateKey() ed25519.PrivateKey { return append(ed25519.PrivateKey(nil), i.key...) }
func ID(k ed25519.PublicKey) string                { h := sha256.Sum256(k); return hex.EncodeToString(h[:]) }
func ValidID(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == hex.EncodeToString(b)
}
func (i *Identity) MarshalPEM() ([]byte, error) {
	if !i.Valid() {
		return nil, errors.New("invalid identity")
	}
	b, e := x509.MarshalPKCS8PrivateKey(i.key)
	if e != nil {
		return nil, e
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}), nil
}
func Parse(b []byte) (*Identity, error) {
	if len(b) > 4096 {
		return nil, errors.New("identity exceeds 4096 bytes")
	}
	p, rest := pem.Decode(b)
	if p == nil || p.Type != "PRIVATE KEY" || len(rest) != 0 {
		return nil, errors.New("expected one PKCS8 PRIVATE KEY PEM block")
	}
	k, e := x509.ParsePKCS8PrivateKey(p.Bytes)
	if e != nil {
		return nil, fmt.Errorf("parse identity: %w", e)
	}
	key, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("identity must use Ed25519")
	}
	return &Identity{key: key}, nil
}

func (i *Identity) Save(path string) error {
	b, e := i.MarshalPEM()
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	ok = true
	return nil
}
func Load(path string) (*Identity, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if e = fsperm.PrivateFile(s); e != nil {
		return nil, fmt.Errorf("identity %s must be an owner-only regular file (chmod 600): %w", path, e)
	}
	if s.Size() > 4096 {
		return nil, errors.New("identity file too large")
	}
	b := make([]byte, s.Size())
	if _, e = f.ReadAt(b, 0); e != nil {
		return nil, e
	}
	return Parse(b)
}
