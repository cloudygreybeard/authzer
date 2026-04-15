// Copyright 2026 cloudygreybeard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Phase 1: DomainVerifier
// ---------------------------------------------------------------------------

func TestDomainVerifier_Trusted(t *testing.T) {
	v := &DomainVerifier{TrustedDomains: []string{"example.com", "raw.githubusercontent.com"}}

	id, err := v.Verify("https://example.com/site-pack.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "trusted domain: example.com" {
		t.Errorf("identity = %q, want %q", id, "trusted domain: example.com")
	}
}

func TestDomainVerifier_CaseInsensitive(t *testing.T) {
	v := &DomainVerifier{TrustedDomains: []string{"Example.COM"}}

	_, err := v.Verify("https://example.com/path", nil)
	if err != nil {
		t.Fatalf("case-insensitive match should succeed: %v", err)
	}
}

func TestDomainVerifier_Untrusted(t *testing.T) {
	v := &DomainVerifier{TrustedDomains: []string{"trusted.com"}}

	_, err := v.Verify("https://evil.com/site-pack.yaml", nil)
	if err == nil {
		t.Fatal("expected error for untrusted domain")
	}
}

func TestDomainVerifier_EmptyList(t *testing.T) {
	v := &DomainVerifier{}

	_, err := v.Verify("https://anything.com/pack.yaml", nil)
	if err == nil {
		t.Fatal("expected error with empty trust list")
	}
}

func TestDomainVerifier_Name(t *testing.T) {
	v := &DomainVerifier{}
	if v.Name() != "domain" {
		t.Errorf("Name() = %q, want %q", v.Name(), "domain")
	}
}

// ---------------------------------------------------------------------------
// Phase 2: SigstoreVerifier
// ---------------------------------------------------------------------------

func TestSigstoreVerifier_NoBundleFallsThrough(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	v := &SigstoreVerifier{
		TrustedIdentities: []TrustedIdentity{{Subject: "test@example.com"}},
	}
	_, err := v.Verify(ts.URL+"/site-pack.yaml", []byte("test"))

	if _, ok := err.(*errNoSignature); !ok {
		t.Fatalf("expected errNoSignature, got %T: %v", err, err)
	}
}

func TestSigstoreVerifier_Name(t *testing.T) {
	v := &SigstoreVerifier{}
	if v.Name() != "sigstore" {
		t.Errorf("Name() = %q, want %q", v.Name(), "sigstore")
	}
}

// ---------------------------------------------------------------------------
// Phase 3: SSHVerifier
// ---------------------------------------------------------------------------

func TestSSHVerifier_NoSigFallsThrough(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	v := &SSHVerifier{
		TrustedKeys: []TrustedKey{{PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:xxx"}},
	}
	_, err := v.Verify(ts.URL+"/site-pack.yaml", []byte("test"))

	if _, ok := err.(*errNoSignature); !ok {
		t.Fatalf("expected errNoSignature, got %T: %v", err, err)
	}
}

func TestSSHVerifier_Name(t *testing.T) {
	v := &SSHVerifier{}
	if v.Name() != "ssh" {
		t.Errorf("Name() = %q, want %q", v.Name(), "ssh")
	}
}

func TestSSHVerifier_Integration(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "test_key")
	genCmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyFile, "-N", "", "-C", "test-authzer-key")
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generating test key: %v\n%s", err, out)
	}

	pubKeyData, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		t.Fatalf("reading public key: %v", err)
	}
	pubKey, fingerprint, comment, err := parseSSHPublicKey(string(pubKeyData))
	if err != nil {
		t.Fatalf("parsing public key: %v", err)
	}

	manifest := []byte("apiVersion: authzer/v1alpha1\nkind: SitePack\nmetadata:\n  name: test\n")
	manifestFile := filepath.Join(tmpDir, "site-pack.yaml")
	if err := os.WriteFile(manifestFile, manifest, 0644); err != nil {
		t.Fatal(err)
	}

	signCmd := exec.Command("ssh-keygen", "-Y", "sign", "-f", keyFile, "-n", "authzer", manifestFile)
	if out, err := signCmd.CombinedOutput(); err != nil {
		t.Fatalf("signing: %v\n%s", err, out)
	}
	sigData, err := os.ReadFile(manifestFile + ".sig")
	if err != nil {
		t.Fatalf("reading signature: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/site-pack.yaml.sig":
			w.Write(sigData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	v := &SSHVerifier{
		TrustedKeys: []TrustedKey{{PublicKey: pubKey, Fingerprint: fingerprint, Comment: comment}},
	}
	id, err := v.Verify(ts.URL+"/site-pack.yaml", manifest)
	if err != nil {
		t.Fatalf("SSH verification failed: %v", err)
	}
	if !strings.Contains(id, "ssh key:") {
		t.Errorf("identity %q should contain 'ssh key:'", id)
	}
	t.Logf("verified: %s", id)
}

func TestSSHVerifier_UntrustedKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}

	tmpDir := t.TempDir()

	// Generate signing key
	signingKey := filepath.Join(tmpDir, "signing_key")
	exec.Command("ssh-keygen", "-t", "ed25519", "-f", signingKey, "-N", "", "-C", "signer").Run()

	// Generate a different trusted key
	trustedKey := filepath.Join(tmpDir, "trusted_key")
	exec.Command("ssh-keygen", "-t", "ed25519", "-f", trustedKey, "-N", "", "-C", "trusted").Run()

	manifest := []byte("test manifest")
	manifestFile := filepath.Join(tmpDir, "site-pack.yaml")
	os.WriteFile(manifestFile, manifest, 0644)

	// Sign with signing key
	exec.Command("ssh-keygen", "-Y", "sign", "-f", signingKey, "-n", "authzer", manifestFile).Run()
	sigData, _ := os.ReadFile(manifestFile + ".sig")

	// Trust a different key
	trustedPubData, _ := os.ReadFile(trustedKey + ".pub")
	pubKey, fp, comment, _ := parseSSHPublicKey(string(trustedPubData))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/site-pack.yaml.sig" {
			w.Write(sigData)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	v := &SSHVerifier{
		TrustedKeys: []TrustedKey{{PublicKey: pubKey, Fingerprint: fp, Comment: comment}},
	}
	_, err := v.Verify(ts.URL+"/site-pack.yaml", manifest)
	if err == nil {
		t.Fatal("expected verification failure with untrusted signing key")
	}
	if _, ok := err.(*errNoSignature); ok {
		t.Fatal("expected hard failure, not errNoSignature")
	}
}

// ---------------------------------------------------------------------------
// Verification chain
// ---------------------------------------------------------------------------

func TestVerifySource_DomainFallback(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	reg := &ContextRegistry{
		TrustedIdentities: []TrustedIdentity{{Subject: "test@example.com"}},
		TrustedSources:    []string{"127.0.0.1"},
	}

	id, err := verifySource(ts.URL+"/site-pack.yaml", []byte("test"), reg)
	if err != nil {
		t.Fatalf("expected domain fallback to succeed: %v", err)
	}
	if !strings.Contains(id, "[domain]") {
		t.Errorf("identity %q should contain '[domain]'", id)
	}
}

func TestVerifySource_NoTrustConfigured(t *testing.T) {
	_, err := verifySource("https://example.com/pack.yaml", []byte("test"), nil)
	if err == nil {
		t.Fatal("expected error with no trust configured")
	}
	if !strings.Contains(err.Error(), "no trusted sources configured") {
		t.Errorf("error should mention 'no trusted sources configured': %v", err)
	}
}

func TestVerifySource_AllFail(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	reg := &ContextRegistry{
		TrustedSources: []string{"not-this-host.example.com"},
	}
	_, err := verifySource(ts.URL+"/site-pack.yaml", []byte("test"), reg)
	if err == nil {
		t.Fatal("expected error when all verifiers fail")
	}
}

// ---------------------------------------------------------------------------
// SSH key parsing
// ---------------------------------------------------------------------------

func TestParseSSHPublicKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "test_key")
	exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyFile, "-N", "", "-C", "test@example.com").Run()

	data, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		t.Fatal(err)
	}

	pubKey, fp, comment, err := parseSSHPublicKey(string(data))
	if err != nil {
		t.Fatalf("parseSSHPublicKey: %v", err)
	}
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("pubKey should start with 'ssh-ed25519 '")
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("fingerprint should start with 'SHA256:', got %q", fp)
	}
	if comment != "test@example.com" {
		t.Errorf("comment = %q, want %q", comment, "test@example.com")
	}
}

func TestParseSSHPublicKey_InvalidFormat(t *testing.T) {
	_, _, _, err := parseSSHPublicKey("not-a-key")
	if err == nil {
		t.Fatal("expected error for invalid key format")
	}
}

func TestParseSSHPublicKey_UnsupportedType(t *testing.T) {
	_, _, _, err := parseSSHPublicKey("rsa AAAAB3NzaC comment")
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

// ---------------------------------------------------------------------------
// URL utilities
// ---------------------------------------------------------------------------

func TestIsURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://example.com/pack.yaml", true},
		{"http://example.com/pack.yaml", true},
		{"./site-pack.yaml", false},
		{"/absolute/path.yaml", false},
		{"site-pack.yaml", false},
		{"ftp://example.com/pack.yaml", false},
	}
	for _, tc := range tests {
		if got := isURL(tc.input); got != tc.want {
			t.Errorf("isURL(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestFetchURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "kind: SitePack\nmetadata:\n  name: test\n")
	}))
	defer ts.Close()

	data, err := fetchURL(ts.URL + "/site-pack.yaml")
	if err != nil {
		t.Fatalf("fetchURL: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty response")
	}
}

func TestFetchURL_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	_, err := fetchURL(ts.URL + "/missing.yaml")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// ---------------------------------------------------------------------------
// Trust persistence: domains
// ---------------------------------------------------------------------------

func TestTrustDomainPersistence(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmp)
	defer os.Setenv("XDG_CONFIG_HOME", origHome)

	os.MkdirAll(filepath.Join(tmp, "authzer"), 0755)

	if err := addTrustedDomain("example.com"); err != nil {
		t.Fatalf("addTrustedDomain: %v", err)
	}

	domains, err := loadTrustedDomains()
	if err != nil {
		t.Fatalf("loadTrustedDomains: %v", err)
	}
	if len(domains) != 1 || domains[0] != "example.com" {
		t.Errorf("domains = %v, want [example.com]", domains)
	}

	if err := addTrustedDomain("other.com"); err != nil {
		t.Fatalf("addTrustedDomain: %v", err)
	}
	domains, _ = loadTrustedDomains()
	if len(domains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(domains))
	}

	err = addTrustedDomain("example.com")
	if err == nil {
		t.Error("expected error for duplicate domain")
	}

	if err := removeTrustedDomain("example.com"); err != nil {
		t.Fatalf("removeTrustedDomain: %v", err)
	}
	domains, _ = loadTrustedDomains()
	if len(domains) != 1 || domains[0] != "other.com" {
		t.Errorf("after remove: domains = %v, want [other.com]", domains)
	}

	err = removeTrustedDomain("nonexistent.com")
	if err == nil {
		t.Error("expected error removing nonexistent domain")
	}
}

// ---------------------------------------------------------------------------
// Trust persistence: sigstore identities
// ---------------------------------------------------------------------------

func TestTrustIdentityPersistence(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmp)
	defer os.Setenv("XDG_CONFIG_HOME", origHome)

	os.MkdirAll(filepath.Join(tmp, "authzer"), 0755)

	if err := addTrustedIdentity("user@example.com", "https://github.com/login/oauth"); err != nil {
		t.Fatalf("addTrustedIdentity: %v", err)
	}

	reg, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.TrustedIdentities) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(reg.TrustedIdentities))
	}
	if reg.TrustedIdentities[0].Subject != "user@example.com" {
		t.Errorf("subject = %q, want %q", reg.TrustedIdentities[0].Subject, "user@example.com")
	}
	if reg.TrustedIdentities[0].Issuer != "https://github.com/login/oauth" {
		t.Errorf("issuer = %q, want %q", reg.TrustedIdentities[0].Issuer, "https://github.com/login/oauth")
	}

	err = addTrustedIdentity("user@example.com", "https://github.com/login/oauth")
	if err == nil {
		t.Error("expected error for duplicate identity")
	}

	if err := addTrustedIdentity("other@example.com", ""); err != nil {
		t.Fatalf("addTrustedIdentity: %v", err)
	}
	reg, _ = loadRegistry()
	if len(reg.TrustedIdentities) != 2 {
		t.Errorf("expected 2 identities, got %d", len(reg.TrustedIdentities))
	}

	if err := removeTrustedIdentity("user@example.com"); err != nil {
		t.Fatalf("removeTrustedIdentity: %v", err)
	}
	reg, _ = loadRegistry()
	if len(reg.TrustedIdentities) != 1 {
		t.Errorf("expected 1 identity after remove, got %d", len(reg.TrustedIdentities))
	}

	err = removeTrustedIdentity("nobody@example.com")
	if err == nil {
		t.Error("expected error removing nonexistent identity")
	}
}

// ---------------------------------------------------------------------------
// Trust persistence: SSH keys
// ---------------------------------------------------------------------------

func TestTrustKeyPersistence(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}

	tmp := t.TempDir()
	origHome := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmp)
	defer os.Setenv("XDG_CONFIG_HOME", origHome)

	os.MkdirAll(filepath.Join(tmp, "authzer"), 0755)

	keyFile := filepath.Join(tmp, "test_key")
	exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyFile, "-N", "", "-C", "test-key").Run()

	if err := addTrustedKeyFromFile(keyFile + ".pub"); err != nil {
		t.Fatalf("addTrustedKeyFromFile: %v", err)
	}

	reg, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.TrustedKeys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(reg.TrustedKeys))
	}
	if !strings.HasPrefix(reg.TrustedKeys[0].PublicKey, "ssh-ed25519 ") {
		t.Errorf("publicKey should start with 'ssh-ed25519 '")
	}
	if !strings.HasPrefix(reg.TrustedKeys[0].Fingerprint, "SHA256:") {
		t.Errorf("fingerprint should start with 'SHA256:'")
	}
	if reg.TrustedKeys[0].Comment != "test-key" {
		t.Errorf("comment = %q, want %q", reg.TrustedKeys[0].Comment, "test-key")
	}

	// Duplicate
	err = addTrustedKeyFromFile(keyFile + ".pub")
	if err == nil {
		t.Error("expected error for duplicate key")
	}

	// Remove by fingerprint
	fp := reg.TrustedKeys[0].Fingerprint
	if err := removeTrustedKey(fp); err != nil {
		t.Fatalf("removeTrustedKey: %v", err)
	}
	reg, _ = loadRegistry()
	if len(reg.TrustedKeys) != 0 {
		t.Errorf("expected 0 keys after remove, got %d", len(reg.TrustedKeys))
	}

	// Re-add, then remove by comment
	addTrustedKeyFromFile(keyFile + ".pub")
	if err := removeTrustedKey("test-key"); err != nil {
		t.Fatalf("removeTrustedKey by comment: %v", err)
	}
	reg, _ = loadRegistry()
	if len(reg.TrustedKeys) != 0 {
		t.Errorf("expected 0 keys after remove by comment, got %d", len(reg.TrustedKeys))
	}

	err = removeTrustedKey("nonexistent")
	if err == nil {
		t.Error("expected error removing nonexistent key")
	}
}

// ---------------------------------------------------------------------------
// errNoSignature type assertion
// ---------------------------------------------------------------------------

func TestErrNoSignature(t *testing.T) {
	err := &errNoSignature{method: "test"}
	if err.Error() != "no test signature found" {
		t.Errorf("Error() = %q, want %q", err.Error(), "no test signature found")
	}

	var sentinel error = err
	if _, ok := sentinel.(*errNoSignature); !ok {
		t.Fatal("type assertion should succeed")
	}
}

// ---------------------------------------------------------------------------
// identityFromSSHVerifyOutput
// ---------------------------------------------------------------------------

func TestIdentityFromSSHVerifyOutput(t *testing.T) {
	keys := []TrustedKey{
		{PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:abc123", Comment: "my-key"},
		{PublicKey: "ssh-rsa AAAA", Fingerprint: "SHA256:def456", Comment: "other-key"},
	}

	tests := []struct {
		output string
		want   string
	}{
		{`Good "authzer" signature for authzer with ED25519 key SHA256:abc123`, "my-key (SHA256:abc123)"},
		{`Good "authzer" signature for authzer with RSA key SHA256:def456`, "other-key (SHA256:def456)"},
		{`Good "authzer" signature for authzer with ED25519 key SHA256:unknown`, "my-key"},
		{"", "my-key"},
	}

	for _, tc := range tests {
		got := identityFromSSHVerifyOutput(tc.output, keys)
		if got != tc.want {
			t.Errorf("identityFromSSHVerifyOutput(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}
