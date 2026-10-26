package tcp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/transport"
)

var ErrAuthentication = errors.New("peer authentication failed")

type Transport struct{}

func (Transport) Name() string { return "tcp+tls" }
func (Transport) Listen(ctx context.Context, a string) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(ctx, "tcp", a)
}
func (Transport) Dial(ctx context.Context, a string) (net.Conn, error) {
	return (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", a)
}

type conn struct {
	*tls.Conn
	id string
}

func (c *conn) PeerID() string { return c.id }
func (Transport) Authenticate(ctx context.Context, raw net.Conn, c transport.Credentials, expected string) (transport.Conn, error) {
	if !c.Identity.Valid() {
		return nil, fmt.Errorf("%w: invalid local identity", ErrAuthentication)
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return nil, e
	}
	now := time.Now()
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "MeshLink"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, c.Identity.PublicKey(), c.Identity.PrivateKey())
	if e != nil {
		return nil, e
	}
	peer := ""
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, NextProtos: []string{"meshlink/1"}, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: c.Identity.PrivateKey()}}, SessionTicketsDisabled: true}
	cfg.VerifyConnection = func(s tls.ConnectionState) error {
		fail := func(reason string) error { return fmt.Errorf("%w: %s", ErrAuthentication, reason) }
		if s.NegotiatedProtocol != "meshlink/1" || len(s.PeerCertificates) != 1 {
			return fail("ALPN or certificate count")
		}
		cert := s.PeerCertificates[0]
		if len(cert.UnhandledCriticalExtensions) != 0 {
			return fail("unhandled critical certificate extension")
		}
		pub, ok := cert.PublicKey.(ed25519.PublicKey)
		if !ok {
			return fail("Ed25519 required")
		}
		if now := time.Now(); now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return fail("certificate expired or not yet valid")
		}
		if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
			return fail("digital signature usage required")
		}
		requiredUsage := x509.ExtKeyUsageClientAuth
		if expected != "" {
			requiredUsage = x509.ExtKeyUsageServerAuth
		}
		usageOK := false
		for _, usage := range cert.ExtKeyUsage {
			if usage == requiredUsage {
				usageOK = true
			}
		}
		if !usageOK {
			return fail("incorrect extended key usage")
		}
		if e := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); e != nil {
			return fail("invalid self-signature")
		}
		peer = identity.ID(pub)
		if peer == c.Identity.ID() {
			return fail("self connection")
		}
		if expected != "" && peer != expected {
			return fail("identity pin mismatch")
		}
		if !c.AllowedPeers[peer] {
			return fail("identity not authorized")
		}
		return nil
	}
	var t *tls.Conn
	if expected != "" {
		cfg.InsecureSkipVerify = true
		t = tls.Client(raw, cfg)
	} else {
		cfg.ClientAuth = tls.RequireAnyClientCert
		t = tls.Server(raw, cfg)
	}
	if e = t.HandshakeContext(ctx); e != nil {
		var networkErr net.Error
		if errors.As(e, &networkErr) && networkErr.Timeout() || ctx.Err() != nil {
			return nil, fmt.Errorf("TLS handshake: %w", e)
		}
		return nil, fmt.Errorf("%w: TLS handshake: %w", ErrAuthentication, e)
	}
	return &conn{t, peer}, nil
}
