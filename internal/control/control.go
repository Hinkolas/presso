// Package control owns the installation-local trust used between edge, engine,
// and site callbacks. Private keys are files, never environment variables.
package control

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const EdgeIdentity = "presso-edge"

type Trust struct {
	dir  string
	mu   sync.Mutex
	ca   *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func Open(dataDir string) (*Trust, error) {
	dir := filepath.Join(dataDir, "control")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(dataDir, "control.initialized")); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("control: trust directory missing; restore installation trust files")
		}
		tmp, err := os.MkdirTemp(dataDir, ".control-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		ca := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "presso control CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		ca, _ = x509.ParseCertificate(der)
		if err = os.MkdirAll(filepath.Join(tmp, "edge"), 0700); err != nil {
			return nil, err
		}
		for _, path := range []string{"ca.pem", "edge/ca.pem"} {
			if err = writeCert(filepath.Join(tmp, path), der); err != nil {
				return nil, err
			}
		}
		if err = writeKey(filepath.Join(tmp, "ca-key.pem"), key); err != nil {
			return nil, err
		}
		t := &Trust{dir: tmp, ca: ca, key: key}
		if err = t.issue("server", false, nil); err != nil {
			return nil, err
		}
		if err = t.issue("edge/client", true, nil); err != nil {
			return nil, err
		}
		if err = os.Rename(tmp, dir); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(dataDir, "control.initialized"), []byte("presso control trust\n"), 0600); err != nil {
		return nil, err
	}
	t := &Trust{dir: dir}
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem"))
	if err != nil {
		return nil, fmt.Errorf("control: existing CA material invalid or missing: %w", err)
	}
	t.ca, err = x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	var ok bool
	t.key, ok = pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || !t.ca.IsCA || time.Now().Before(t.ca.NotBefore) || !time.Now().Before(t.ca.NotAfter) {
		return nil, errors.New("control: invalid or expired CA; restore the installation trust files")
	}
	t.pool = x509.NewCertPool()
	t.pool.AddCert(t.ca)
	// Every previously generated leaf must exist and match its private key.
	if _, err = t.Refresh(context.Background()); err != nil {
		return nil, err
	}
	return t, nil
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(err)
	}
	return n
}
func writeCert(path string, der []byte) error {
	return atomicWrite(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644)
}
func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return atomicWrite(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0600)
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".trust-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func (t *Trust) issue(name string, client bool, key *ecdsa.PrivateKey) error {
	if key == nil {
		var err error
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		if err = writeKey(filepath.Join(t.dir, name+"-key.pem"), key); err != nil {
			return err
		}
	}
	now := time.Now()
	c := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "presso-engine"}, DNSNames: []string{"host.docker.internal"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if client {
		c.Subject.CommonName = EdgeIdentity
		c.DNSNames = nil
		c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, c, t.ca, &key.PublicKey, t.key)
	if err != nil {
		return err
	}
	return writeCert(filepath.Join(t.dir, name+".pem"), der)
}

// Refresh renews leaf certificates using their existing keys. Missing or altered
// files are an error, not permission to create replacement trust.
func (t *Trust) Refresh(ctx context.Context) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	changed := false
	if time.Now().Before(t.ca.NotBefore) || !time.Now().Before(t.ca.NotAfter) {
		return false, errors.New("control: CA expired or not yet valid")
	}
	caBytes, err := os.ReadFile(filepath.Join(t.dir, "edge", "ca.pem"))
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(caBytes)
	if block == nil || !bytes.Equal(block.Bytes, t.ca.Raw) {
		return false, errors.New("control: edge CA missing or changed")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	for _, name := range []string{"server", "edge/client"} {
		pair, err := tls.LoadX509KeyPair(filepath.Join(t.dir, name+".pem"), filepath.Join(t.dir, name+"-key.pem"))
		if err != nil {
			return false, fmt.Errorf("control: invalid existing %s: %w", name, err)
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return false, err
		}
		if err = leaf.CheckSignatureFrom(t.ca); err != nil {
			return false, err
		}
		if time.Now().Before(leaf.NotBefore) {
			return false, errors.New("control: leaf is not yet valid")
		}
		if name == "edge/client" {
			if leaf.Subject.CommonName != EdgeIdentity || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
				return false, errors.New("control: invalid edge identity")
			}
		} else if err := leaf.VerifyHostname("host.docker.internal"); err != nil {
			return false, err
		}
		key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
		if !ok {
			return false, errors.New("control: invalid leaf key")
		}
		if time.Until(leaf.NotAfter) < 7*24*time.Hour {
			if err = t.issue(name, name == "edge/client", key); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}
func (t *Trust) ServerTLS() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: t.pool, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		pair, err := tls.LoadX509KeyPair(filepath.Join(t.dir, "server.pem"), filepath.Join(t.dir, "server-key.pem"))
		return &pair, err
	}}
}
func IsEdge(cs *tls.ConnectionState) bool {
	if cs == nil || len(cs.VerifiedChains) == 0 || len(cs.PeerCertificates) == 0 {
		return false
	}
	return cs.PeerCertificates[0].Subject.CommonName == EdgeIdentity
}
