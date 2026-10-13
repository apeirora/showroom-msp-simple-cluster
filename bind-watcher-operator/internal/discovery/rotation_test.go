package discovery

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	apis "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Use the same config and real client before and after a Kubernetes-style
// projected Secret update. Each request opens a new TLS connection.
func TestKCPClientReloadsProjectedCertificateFiles(t *testing.T) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Connection", "close")
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			t.Error("client certificate was not verified")
			http.Error(w, "no verified client", 401)
			return
		}
		slice := &apis.APIExportEndpointSlice{TypeMeta: metav1.TypeMeta{APIVersion: apis.SchemeGroupVersion.String(), Kind: "APIExportEndpointSlice"}}
		slice.Annotations = map[string]string{"test-client": r.TLS.PeerCertificates[0].Subject.CommonName}
		_ = json.NewEncoder(w).Encode(slice)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()

	mount := t.TempDir()
	writeClientCertificate(t, filepath.Join(mount, "..first"), "before-rotation", 2, ca, caKey)
	writeClientCertificate(t, filepath.Join(mount, "..second"), "after-rotation", 3, ca, caKey)
	if err := os.Symlink("..first", filepath.Join(mount, "..data")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tls.crt", "tls.key"} {
		if err := os.Symlink(filepath.Join("..data", name), filepath.Join(mount, name)); err != nil {
			t.Fatal(err)
		}
	}
	kubeconfig := filepath.Join(mount, "kubeconfig")
	content := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: provider\nclusters:\n- name: kcp\n  cluster:\n    server: %s\n    insecure-skip-tls-verify: true\ncontexts:\n- name: provider\n  context:\n    cluster: kcp\n    user: syncer\nusers:\n- name: syncer\n  user:\n    client-certificate: %s\n    client-key: %s\n", server.URL, filepath.Join(mount, "tls.crt"), filepath.Join(mount, "tls.key"))
	if err := os.WriteFile(kubeconfig, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadKubeconfig(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CertData) != 0 || len(cfg.KeyData) != 0 {
		t.Fatal("kubeconfig must retain file references, not embedded credentials")
	}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{apis.SchemeGroupVersion})
	mapper.Add(apis.SchemeGroupVersion.WithKind("APIExportEndpointSlice"), meta.RESTScopeRoot)
	scheme := runtime.NewScheme()
	if err := apis.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader, err := client.New(cfg, client.Options{Scheme: scheme, Mapper: mapper})
	if err != nil {
		t.Fatal(err)
	}
	readIdentity := func(ctx context.Context) (string, error) {
		slice := &apis.APIExportEndpointSlice{}
		err := reader.Get(ctx, client.ObjectKey{Name: "simple-cluster"}, slice)
		return slice.Annotations["test-client"], err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	identity, err := readIdentity(ctx)
	if err != nil || identity != "before-rotation" {
		t.Fatalf("initial connection: identity=%q error=%v", identity, err)
	}
	// Kubernetes publishes a new Secret revision by atomically replacing ..data.
	if err := os.Symlink("..second", filepath.Join(mount, "..data_tmp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(mount, "..data_tmp"), filepath.Join(mount, "..data")); err != nil {
		t.Fatal(err)
	}
	err = wait.PollUntilContextTimeout(ctx, 50*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		identity, err = readIdentity(ctx)
		return identity == "after-rotation", err
	})
	if err != nil {
		t.Fatalf("same client did not reload the certificate: identity=%q error=%v", identity, err)
	}
}

func writeClientCertificate(t *testing.T, directory, commonName string, serial int64, ca *x509.Certificate, caKey *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: commonName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for name, block := range map[string]*pem.Block{"tls.crt": {Type: "CERTIFICATE", Bytes: der}, "tls.key": {Type: "PRIVATE KEY", Bytes: keyDER}} {
		if err := os.WriteFile(filepath.Join(directory, name), pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
