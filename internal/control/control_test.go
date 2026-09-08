package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrustPersistenceAndIdentity(t *testing.T) {
	dir := t.TempDir()
	trust, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := os.ReadFile(filepath.Join(dir, "control/ca.pem"))
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !trust.ca.Equal(again.ca) {
		t.Fatal("CA replaced")
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsEdge(r.TLS) {
			http.Error(w, "edge required", 403)
			return
		}
		w.WriteHeader(204)
	}))
	server.TLS = trust.ServerTLS()
	server.StartTLS()
	defer server.Close()
	request := func(cert *tls.Certificate) (int, error) {
		cfg := &tls.Config{RootCAs: pool, ServerName: "host.docker.internal", MinVersion: tls.VersionTLS12}
		if cert != nil {
			cfg.Certificates = []tls.Certificate{*cert}
		}
		tr := &http.Transport{TLSClientConfig: cfg}
		defer tr.CloseIdleConnections()
		res, err := (&http.Client{Transport: tr}).Get(server.URL)
		if err != nil {
			return 0, err
		}
		defer res.Body.Close()
		return res.StatusCode, nil
	}
	if status, err := request(nil); err != nil || status != 403 {
		t.Fatalf("uncertified: %d %v", status, err)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "control/edge/client.pem"), filepath.Join(dir, "control/edge/client-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if status, err := request(&pair); err != nil || status != 204 {
		t.Fatalf("edge: %d %v", status, err)
	}
	otherDir := t.TempDir()
	if _, err := Open(otherDir); err != nil {
		t.Fatal(err)
	}
	other, _ := tls.LoadX509KeyPair(filepath.Join(otherDir, "control/edge/client.pem"), filepath.Join(otherDir, "control/edge/client-key.pem"))
	if _, err := request(&other); err == nil {
		t.Fatal("untrusted client accepted")
	}
	if changed, err := trust.Refresh(context.Background()); err != nil || changed {
		t.Fatalf("unnecessary renewal: %v %v", changed, err)
	}
	if err := os.Remove(filepath.Join(dir, "control/edge/client-key.pem")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("missing key silently replaced")
	}
	if err := os.RemoveAll(filepath.Join(dir, "control")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("missing trust silently replaced")
	}
}

func TestLeafRenewalKeepsTrustAndKeys(t *testing.T) {
	dir := t.TempDir()
	trust, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "control/edge/client.pem")
	keyPath := filepath.Join(dir, "control/edge/client-key.pem")
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, _ := os.ReadFile(keyPath)
	caBefore, _ := os.ReadFile(filepath.Join(dir, "control/ca.pem"))
	leaf.NotAfter = time.Now().Add(24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, trust.ca, leaf.PublicKey, trust.key)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCert(certPath, der); err != nil {
		t.Fatal(err)
	}
	if changed, err := trust.Refresh(context.Background()); err != nil || !changed {
		t.Fatalf("renewal: %v %v", changed, err)
	}
	renewed, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	nowLeaf, _ := x509.ParseCertificate(renewed.Certificate[0])
	if time.Until(nowLeaf.NotAfter) < 30*24*time.Hour {
		t.Fatal("certificate not renewed")
	}
	keyAfter, _ := os.ReadFile(keyPath)
	caAfter, _ := os.ReadFile(filepath.Join(dir, "control/ca.pem"))
	if !bytes.Equal(keyBefore, keyAfter) || !bytes.Equal(caBefore, caAfter) {
		t.Fatal("renewal replaced trust or private key")
	}
}
