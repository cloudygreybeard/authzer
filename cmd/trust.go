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
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Verification errors
// ---------------------------------------------------------------------------

// errNoSignature indicates no sidecar signature was found for this
// verification method. The orchestrator treats this as a non-fatal
// fallthrough to the next verifier in the chain.
type errNoSignature struct {
	method string
}

func (e *errNoSignature) Error() string {
	return fmt.Sprintf("no %s signature found", e.method)
}

// ---------------------------------------------------------------------------
// SourceVerifier interface
// ---------------------------------------------------------------------------

// SourceVerifier verifies the authenticity of a remote resource.
// Implementations return the verified identity on success, errNoSignature
// when no matching signature sidecar exists (allowing fallthrough to the
// next verifier), or a hard error when verification fails.
type SourceVerifier interface {
	Name() string
	Verify(sourceURL string, manifest []byte) (identity string, err error)
}

// ---------------------------------------------------------------------------
// Phase 1: Domain-based trust
// ---------------------------------------------------------------------------

// DomainVerifier trusts sources whose host matches a configured list.
type DomainVerifier struct {
	TrustedDomains []string
}

func (v *DomainVerifier) Name() string { return "domain" }

func (v *DomainVerifier) Verify(sourceURL string, _ []byte) (string, error) {
	u, err := url.Parse(sourceURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}
	host := u.Hostname()
	for _, d := range v.TrustedDomains {
		if strings.EqualFold(host, d) {
			return fmt.Sprintf("trusted domain: %s", d), nil
		}
	}
	return "", fmt.Errorf("domain %q is not trusted", host)
}

// ---------------------------------------------------------------------------
// Phase 2: Sigstore (cosign) verification
// ---------------------------------------------------------------------------

// SigstoreVerifier verifies manifests signed with cosign (keyless OIDC).
// It fetches the sigstore bundle sidecar (<url>.sigstore.json) and shells
// out to cosign verify-blob for each trusted identity until one succeeds.
type SigstoreVerifier struct {
	TrustedIdentities []TrustedIdentity
}

func (v *SigstoreVerifier) Name() string { return "sigstore" }

func (v *SigstoreVerifier) Verify(sourceURL string, manifest []byte) (string, error) {
	bundleURL := sidecarURL(sourceURL, ".sigstore.json")
	logV(2, "sigstore: looking for bundle at %s", bundleURL)
	bundle, err := fetchURL(bundleURL)
	if err != nil {
		logV(2, "sigstore: no bundle found, skipping")
		return "", &errNoSignature{method: "sigstore"}
	}

	cosignPath, err := exec.LookPath("cosign")
	if err != nil {
		return "", fmt.Errorf("sigstore bundle found but cosign is not installed\n\nInstall cosign: https://docs.sigstore.dev/cosign/system_config/installation/")
	}
	logV(4, "sigstore: using cosign at %s", cosignPath)

	tmpDir, err := os.MkdirTemp("", "authzer-verify-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	manifestFile := filepath.Join(tmpDir, "manifest.yaml")
	bundleFile := filepath.Join(tmpDir, "manifest.sigstore.json")
	if err := os.WriteFile(manifestFile, manifest, 0600); err != nil {
		return "", fmt.Errorf("writing temp manifest: %w", err)
	}
	if err := os.WriteFile(bundleFile, bundle, 0600); err != nil {
		return "", fmt.Errorf("writing temp bundle: %w", err)
	}

	var lastStderr string
	for _, identity := range v.TrustedIdentities {
		args := []string{
			"verify-blob",
			"--bundle", bundleFile,
			"--certificate-identity", identity.Subject,
		}
		if identity.Issuer != "" {
			args = append(args, "--certificate-oidc-issuer", identity.Issuer)
		}
		args = append(args, manifestFile)

		logV(4, "exec: cosign %s", strings.Join(args, " "))
		cmd := exec.Command(cosignPath, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			logV(2, "sigstore: verified against identity %s", identity.Subject)
			return fmt.Sprintf("sigstore identity: %s", identity.Subject), nil
		}
		lastStderr = strings.TrimSpace(stderr.String())
		logV(4, "sigstore: identity %s failed: %s", identity.Subject, lastStderr)
	}

	return "", fmt.Errorf("sigstore verification failed against all %d trusted identities\n\nlast error: %s", len(v.TrustedIdentities), lastStderr)
}

// ---------------------------------------------------------------------------
// Phase 3: SSH signature verification
// ---------------------------------------------------------------------------

// SSHVerifier verifies manifests signed with SSH keys. It fetches the
// signature sidecar (<url>.sig) and shells out to ssh-keygen -Y verify
// using an allowed_signers file built from trusted keys.
//
// Publisher signing workflow:
//
//	ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n authzer site-pack.yaml
type SSHVerifier struct {
	TrustedKeys []TrustedKey
}

func (v *SSHVerifier) Name() string { return "ssh" }

func (v *SSHVerifier) Verify(sourceURL string, manifest []byte) (string, error) {
	sigURL := sidecarURL(sourceURL, ".sig")
	logV(2, "ssh: looking for signature at %s", sigURL)
	sig, err := fetchURL(sigURL)
	if err != nil {
		logV(2, "ssh: no signature found, skipping")
		return "", &errNoSignature{method: "ssh"}
	}

	sshKeygenPath, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return "", fmt.Errorf("SSH signature found but ssh-keygen is not in PATH")
	}
	logV(4, "ssh: using ssh-keygen at %s", sshKeygenPath)
	logV(2, "ssh: verifying against %d trusted keys", len(v.TrustedKeys))

	tmpDir, err := os.MkdirTemp("", "authzer-verify-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	sigFile := filepath.Join(tmpDir, "manifest.sig")
	if err := os.WriteFile(sigFile, sig, 0600); err != nil {
		return "", fmt.Errorf("writing temp signature: %w", err)
	}

	var allowedSigners strings.Builder
	for _, k := range v.TrustedKeys {
		fmt.Fprintf(&allowedSigners, "authzer %s\n", k.PublicKey)
	}
	allowedFile := filepath.Join(tmpDir, "allowed_signers")
	if err := os.WriteFile(allowedFile, []byte(allowedSigners.String()), 0600); err != nil {
		return "", fmt.Errorf("writing allowed_signers: %w", err)
	}

	args := []string{"-Y", "verify", "-f", allowedFile, "-I", "authzer", "-n", "authzer", "-s", sigFile}
	logV(4, "exec: ssh-keygen %s", strings.Join(args, " "))
	cmd := exec.Command(sshKeygenPath, args...)
	cmd.Stdin = bytes.NewReader(manifest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		logV(2, "ssh: verification failed: %s", errMsg)
		return "", fmt.Errorf("SSH signature verification failed: %s", errMsg)
	}

	id := identityFromSSHVerifyOutput(stderr.String(), v.TrustedKeys)
	logV(2, "ssh: verified: %s", id)
	return fmt.Sprintf("ssh key: %s", id), nil
}

// identityFromSSHVerifyOutput extracts a human-readable key identity
// from ssh-keygen's stderr, falling back to the first trusted key.
func identityFromSSHVerifyOutput(output string, keys []TrustedKey) string {
	// ssh-keygen prints: Good "authzer" signature for authzer with ED25519 key SHA256:xxx
	for _, k := range keys {
		if strings.Contains(output, k.Fingerprint) {
			if k.Comment != "" {
				return k.Comment + " (" + k.Fingerprint + ")"
			}
			return k.Fingerprint
		}
	}
	if len(keys) > 0 && keys[0].Comment != "" {
		return keys[0].Comment
	}
	return "verified"
}

// ---------------------------------------------------------------------------
// Verification chain orchestrator
// ---------------------------------------------------------------------------

// verifySource runs the verification chain: sigstore -> SSH -> domain.
// It returns the verified identity string on success. Verifiers that
// find no sidecar signature fall through; hard verification failures
// are returned immediately.
func verifySource(sourceURL string, manifest []byte, reg *ContextRegistry) (string, error) {
	if reg == nil {
		reg = &ContextRegistry{}
	}

	var verifiers []SourceVerifier
	if len(reg.TrustedIdentities) > 0 {
		verifiers = append(verifiers, &SigstoreVerifier{TrustedIdentities: reg.TrustedIdentities})
	}
	if len(reg.TrustedKeys) > 0 {
		verifiers = append(verifiers, &SSHVerifier{TrustedKeys: reg.TrustedKeys})
	}
	if len(reg.TrustedSources) > 0 {
		verifiers = append(verifiers, &DomainVerifier{TrustedDomains: reg.TrustedSources})
	}

	if len(verifiers) == 0 {
		return "", fmt.Errorf("no trusted sources configured\n\nTo trust a domain:\n  authzer config trust add DOMAIN\n\nTo trust a sigstore identity:\n  authzer config trust add-identity SUBJECT --issuer ISSUER\n\nTo trust an SSH key:\n  authzer config trust add-key PUBLIC_KEY_FILE")
	}

	for _, v := range verifiers {
		logV(2, "verify: trying %s verifier", v.Name())
		id, err := v.Verify(sourceURL, manifest)
		if err == nil {
			logV(1, "verify: source verified via %s: %s", v.Name(), id)
			return fmt.Sprintf("[%s] %s", v.Name(), id), nil
		}
		if _, ok := err.(*errNoSignature); ok {
			logV(2, "verify: %s has no signature, falling through", v.Name())
			continue
		}
		logV(2, "verify: %s failed: %v", v.Name(), err)
		return "", err
	}

	return "", fmt.Errorf("source %q could not be verified\n\nConfigure trust with:\n  authzer config trust add DOMAIN\n  authzer config trust add-identity SUBJECT --issuer ISSUER\n  authzer config trust add-key PUBLIC_KEY_FILE\n\nOr skip verification (not recommended):\n  --insecure-skip-source-verify", sourceURL)
}

// ---------------------------------------------------------------------------
// URL utilities
// ---------------------------------------------------------------------------

// fetchURL retrieves the content at the given URL. It supports two
// URL styles:
//
//   - Git repository file URLs, fetched via shallow git clone. Auth
//     is delegated to the configured git credential helper (GCM, gh,
//     netrc, keychain, etc.). The repo/path boundary is detected
//     automatically for known forges (github.com, gitlab.com,
//     bitbucket.org, codeberg.org, sr.ht) or via an explicit "//"
//     separator for self-hosted instances. Append ?ref=TAG to pin
//     to a tag or branch (defaults to HEAD).
//
//     https://github.com/OWNER/REPO/path/to/file?ref=v1
//     https://git.example.com/group/repo//path/to/file?ref=v1
//
//   - Plain HTTPS URLs, fetched with a standard HTTP GET (no auth).
//
// Release asset URLs (e.g. /releases/download/...) are not supported;
// site-pack files must live in the repository tree.
func fetchURL(rawURL string) ([]byte, error) {
	if repoURL, filePath, ref, ok := parseGitFileURL(rawURL); ok {
		logV(1, "fetching %s from git repo %s@%s", filePath, repoURL, ref)
		return fetchGitFile(repoURL, filePath, ref)
	}

	logV(1, "fetching %s via HTTP GET", rawURL)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	logV(7, "HTTP GET %s -> %d %s", rawURL, resp.StatusCode, resp.Status)
	if verbosity >= 7 {
		for k, vals := range resp.Header {
			for _, v := range vals {
				logV(7, "  %s: %s", k, v)
			}
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", rawURL, err)
	}
	logV(3, "received %d bytes from %s", len(body), rawURL)
	logV(6, "content from %s:\n%s", rawURL, truncateForLog(body, 2048))
	logV(8, "full content from %s:\n%s", rawURL, string(body))
	return body, nil
}

// knownForgeSegments maps forge hostnames to the number of path
// segments that form the repository root. For example, github.com
// uses {owner}/{repo} = 2 segments.
var knownForgeSegments = map[string]int{
	"github.com":    2,
	"gitlab.com":    2,
	"bitbucket.org": 2,
	"sr.ht":         2,
	"codeberg.org":  2,
}

// parseGitFileURL detects git repository file references in a URL and
// splits them into repo clone URL, file path within the repo, and ref.
//
// Detection strategies (in order):
//  1. Known forge hosts -- the repo root is inferred from the URL
//     structure (e.g. github.com always uses {owner}/{repo}).
//  2. Explicit "//" separator (kustomize convention) -- for self-hosted
//     or unusual forges.
//  3. ".git" in the URL path -- the repo root ends at ".git".
//
// An optional ?ref=TAG query parameter pins the clone to a tag or
// branch (defaults to HEAD).
func parseGitFileURL(rawURL string) (repoURL, filePath, ref string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", "", false
	}

	ref = "HEAD"
	if r := u.Query().Get("ref"); r != "" {
		ref = r
	}

	cleanPath := strings.TrimPrefix(u.Path, "/")

	// Strategy 1: known forge host.
	if segments, known := knownForgeSegments[u.Host]; known {
		parts := strings.SplitN(cleanPath, "/", segments+1)
		if len(parts) > segments {
			filePath = strings.TrimLeft(parts[segments], "/")
			if filePath != "" {
				repoURL = fmt.Sprintf("%s://%s/%s", u.Scheme, u.Host, strings.Join(parts[:segments], "/"))
				logV(2, "parsed URL as %s forge: repo=%s path=%s ref=%s", u.Host, repoURL, filePath, ref)
				return repoURL, filePath, ref, true
			}
		}
	}

	// Strategy 2: explicit "//" separator.
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd >= 0 {
		searchFrom := schemeEnd + 3
		if idx := strings.Index(rawURL[searchFrom:], "//"); idx >= 0 {
			idx += searchFrom
			base := rawURL[:idx]
			remainder := rawURL[idx+2:]
			if qIdx := strings.Index(remainder, "?"); qIdx >= 0 {
				remainder = remainder[:qIdx]
			}
			filePath = strings.TrimPrefix(remainder, "/")
			if filePath != "" {
				logV(2, "parsed URL with // separator: repo=%s path=%s ref=%s", base, filePath, ref)
				return base, filePath, ref, true
			}
		}
	}

	// Strategy 3: ".git" in URL path.
	if dotGit := strings.Index(rawURL, ".git/"); dotGit >= 0 {
		repoURL = rawURL[:dotGit+4]
		remainder := rawURL[dotGit+5:]
		if qIdx := strings.Index(remainder, "?"); qIdx >= 0 {
			remainder = remainder[:qIdx]
		}
		filePath = strings.TrimPrefix(remainder, "/")
		if filePath != "" {
			logV(2, "parsed URL with .git boundary: repo=%s path=%s ref=%s", repoURL, filePath, ref)
			return repoURL, filePath, ref, true
		}
	}

	return "", "", "", false
}

// fetchGitFile does a shallow clone of the given repo at the specified
// ref, reads the requested file, and cleans up. Authentication is
// handled by whatever git credential helper is configured (gh, GCM,
// netrc, keychain, etc.).
func fetchGitFile(repoURL, filePath, ref string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "authzer-git-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	args := []string{"clone", "--depth", "1", "--single-branch"}
	if ref != "HEAD" {
		args = append(args, "--branch", ref)
	}
	args = append(args, repoURL, tmpDir)

	logV(4, "exec: git %s", strings.Join(args, " "))
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git clone %s: %s: %w",
			repoURL, strings.TrimSpace(stderr.String()), err)
	}
	if s := strings.TrimSpace(stderr.String()); s != "" {
		logV(7, "git stderr: %s", s)
	}
	logV(4, "cloned %s@%s to %s", repoURL, ref, tmpDir)

	target := filepath.Join(tmpDir, filePath)
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("reading %s from %s@%s: %w", filePath, repoURL, ref, err)
	}
	logV(2, "read %d bytes from %s@%s:%s", len(data), repoURL, ref, filePath)
	logV(6, "content from %s@%s:%s:\n%s", repoURL, ref, filePath, truncateForLog(data, 2048))
	logV(8, "full content from %s@%s:%s:\n%s", repoURL, ref, filePath, string(data))
	return data, nil
}

// isURL returns true if the string looks like an HTTP(S) URL.
func isURL(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// truncateForLog returns content as a string, truncated to maxLen
// bytes with a marker if it exceeds the limit.
func truncateForLog(data []byte, maxLen int) string {
	if len(data) <= maxLen {
		return string(data)
	}
	return string(data[:maxLen]) + fmt.Sprintf("\n... truncated (%d bytes total)", len(data))
}

// sidecarURL appends a suffix to a URL's path component, preserving
// any query parameters. For example:
//
//	sidecarURL("https://host/path/file.yaml?ref=v1", ".sig")
//	  => "https://host/path/file.yaml.sig?ref=v1"
func sidecarURL(base, suffix string) string {
	if qIdx := strings.LastIndex(base, "?"); qIdx >= 0 {
		return base[:qIdx] + suffix + base[qIdx:]
	}
	return base + suffix
}

// ---------------------------------------------------------------------------
// SSH public key parsing
// ---------------------------------------------------------------------------

// parseSSHPublicKey extracts the key type + data, SHA256 fingerprint,
// and optional comment from an SSH public key line (authorized_keys
// format). No external dependencies are needed.
func parseSSHPublicKey(line string) (pubKey, fingerprint, comment string, err error) {
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("invalid SSH public key format: expected at least 'type base64data'")
	}

	keyType := parts[0]
	validPrefixes := []string{"ssh-", "ecdsa-", "sk-"}
	valid := false
	for _, p := range validPrefixes {
		if strings.HasPrefix(keyType, p) {
			valid = true
			break
		}
	}
	if !valid {
		return "", "", "", fmt.Errorf("unsupported key type: %s", keyType)
	}

	keyData, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", "", fmt.Errorf("decoding key data: %w", err)
	}

	hash := sha256.Sum256(keyData)
	fp := "SHA256:" + base64.RawStdEncoding.EncodeToString(hash[:])

	pubKey = keyType + " " + parts[1]
	if len(parts) > 2 {
		comment = strings.TrimSpace(parts[2])
	}

	return pubKey, fp, comment, nil
}

// ---------------------------------------------------------------------------
// Trust management: domain persistence
// ---------------------------------------------------------------------------

func loadTrustedDomains() ([]string, error) {
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	if reg == nil {
		return nil, nil
	}
	return reg.TrustedSources, nil
}

func addTrustedDomain(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		reg = &ContextRegistry{
			TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ContextList"},
		}
	}
	for _, d := range reg.TrustedSources {
		if strings.EqualFold(d, domain) {
			return fmt.Errorf("domain %q is already trusted", domain)
		}
	}
	reg.TrustedSources = append(reg.TrustedSources, domain)
	return saveRegistry(reg)
}

func removeTrustedDomain(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("domain %q is not in the trust list", domain)
	}
	found := false
	filtered := make([]string, 0, len(reg.TrustedSources))
	for _, d := range reg.TrustedSources {
		if strings.EqualFold(d, domain) {
			found = true
			continue
		}
		filtered = append(filtered, d)
	}
	if !found {
		return fmt.Errorf("domain %q is not in the trust list", domain)
	}
	reg.TrustedSources = filtered
	return saveRegistry(reg)
}

// ---------------------------------------------------------------------------
// Trust management: sigstore identity persistence
// ---------------------------------------------------------------------------

func addTrustedIdentity(subject, issuer string) error {
	subject = strings.TrimSpace(subject)
	issuer = strings.TrimSpace(issuer)

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		reg = &ContextRegistry{
			TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ContextList"},
		}
	}
	for _, id := range reg.TrustedIdentities {
		if strings.EqualFold(id.Subject, subject) {
			return fmt.Errorf("identity %q is already trusted", subject)
		}
	}
	reg.TrustedIdentities = append(reg.TrustedIdentities, TrustedIdentity{
		Subject: subject,
		Issuer:  issuer,
	})
	return saveRegistry(reg)
}

func removeTrustedIdentity(subject string) error {
	subject = strings.TrimSpace(subject)

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("identity %q is not in the trust list", subject)
	}
	found := false
	filtered := make([]TrustedIdentity, 0, len(reg.TrustedIdentities))
	for _, id := range reg.TrustedIdentities {
		if strings.EqualFold(id.Subject, subject) {
			found = true
			continue
		}
		filtered = append(filtered, id)
	}
	if !found {
		return fmt.Errorf("identity %q is not in the trust list", subject)
	}
	reg.TrustedIdentities = filtered
	return saveRegistry(reg)
}

// ---------------------------------------------------------------------------
// Trust management: SSH key persistence
// ---------------------------------------------------------------------------

func addTrustedKeyFromFile(pathOrURL string) error {
	var data []byte
	var err error
	if isURL(pathOrURL) {
		logV(1, "trust: fetching public key from %s", pathOrURL)
		data, err = fetchURL(pathOrURL)
	} else {
		logV(1, "trust: reading public key from %s", pathOrURL)
		data, err = os.ReadFile(pathOrURL)
	}
	if err != nil {
		return fmt.Errorf("reading public key: %w", err)
	}
	logV(2, "trust: received %d byte key", len(data))
	return addTrustedKeyFromString(strings.TrimSpace(string(data)))
}

func addTrustedKeyFromString(keyLine string) error {
	pubKey, fingerprint, comment, err := parseSSHPublicKey(keyLine)
	if err != nil {
		return err
	}

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		reg = &ContextRegistry{
			TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ContextList"},
		}
	}
	for _, k := range reg.TrustedKeys {
		if k.Fingerprint == fingerprint {
			return fmt.Errorf("key %s is already trusted", fingerprint)
		}
	}
	reg.TrustedKeys = append(reg.TrustedKeys, TrustedKey{
		PublicKey:   pubKey,
		Fingerprint: fingerprint,
		Comment:     comment,
	})
	return saveRegistry(reg)
}

func removeTrustedKey(ref string) error {
	ref = strings.TrimSpace(ref)

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("key %q is not in the trust list", ref)
	}
	found := false
	filtered := make([]TrustedKey, 0, len(reg.TrustedKeys))
	for _, k := range reg.TrustedKeys {
		if k.Fingerprint == ref || k.Comment == ref {
			found = true
			continue
		}
		filtered = append(filtered, k)
	}
	if !found {
		return fmt.Errorf("key %q is not in the trust list", ref)
	}
	reg.TrustedKeys = filtered
	return saveRegistry(reg)
}

// ---------------------------------------------------------------------------
// CLI: authzer config trust
// ---------------------------------------------------------------------------

var configTrustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Manage trusted sources for remote imports",
	Long: `Manage trusted sources for remote SitePack imports. Three verification
methods are supported, tried in priority order:

  1. Sigstore (cosign)  — keyless OIDC signing via certificate transparency
  2. SSH signatures     — sign with any SSH key via ssh-keygen
  3. Domain trust       — allow all imports from specific hosts

Sigstore verification requires cosign in PATH. SSH verification requires
ssh-keygen in PATH. Domain trust requires no external tools.

Publisher signing workflows:

  # Sigstore (keyless — authenticates via browser/OIDC):
  cosign sign-blob --bundle site-pack.yaml.sigstore.json site-pack.yaml

  # SSH (sign with an existing SSH key):
  ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n authzer site-pack.yaml`,
}

var trustAddCmd = &cobra.Command{
	Use:   "add DOMAIN",
	Short: "Add a domain to the trusted sources list",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := addTrustedDomain(args[0]); err != nil {
			return err
		}
		logHuman("Added %q to trusted domains.\n", args[0])
		return nil
	},
}

var trustRemoveCmd = &cobra.Command{
	Use:   "remove DOMAIN",
	Short: "Remove a domain from the trusted sources list",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := removeTrustedDomain(args[0]); err != nil {
			return err
		}
		logHuman("Removed %q from trusted domains.\n", args[0])
		return nil
	},
}

var trustAddIdentityCmd = &cobra.Command{
	Use:   "add-identity SUBJECT",
	Short: "Trust a sigstore (OIDC) identity for remote verification",
	Long: `Add a sigstore identity to the trusted list. Remote manifests signed
with cosign by this identity will be accepted.

The subject is the OIDC identity (e.g. email) used during signing. The
--issuer flag narrows trust to a specific OIDC provider (recommended).

Publisher signing workflow:
  cosign sign-blob --bundle site-pack.yaml.sigstore.json site-pack.yaml

Example:
  authzer config trust add-identity user@example.com \
    --issuer https://github.com/login/oauth`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issuer, _ := cmd.Flags().GetString("issuer")
		if err := addTrustedIdentity(args[0], issuer); err != nil {
			return err
		}
		msg := fmt.Sprintf("Added sigstore identity %q", args[0])
		if issuer != "" {
			msg += fmt.Sprintf(" (issuer: %s)", issuer)
		}
		logHuman("%s to trusted sources.\n", msg)
		return nil
	},
}

var trustRemoveIdentityCmd = &cobra.Command{
	Use:   "remove-identity SUBJECT",
	Short: "Remove a sigstore identity from the trust list",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := removeTrustedIdentity(args[0]); err != nil {
			return err
		}
		logHuman("Removed sigstore identity %q from trusted sources.\n", args[0])
		return nil
	},
}

var trustAddKeyCmd = &cobra.Command{
	Use:   "add-key FILE_OR_URL",
	Short: "Trust an SSH public key for remote verification",
	Long: `Add an SSH public key to the trusted list. Remote manifests signed
with the corresponding private key will be accepted.

The argument can be a local file path or an HTTPS URL pointing to a
file inside a git repository. For known forge hosts (GitHub, GitLab,
Bitbucket, etc.) the repository boundary is detected automatically.
Auth is handled by the configured git credential helper (GCM, gh,
netrc, keychain, etc.).

Publisher signing workflow:
  ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n authzer site-pack.yaml

Examples:
  authzer config trust add-key ~/.ssh/id_ed25519.pub
  authzer config trust add-key https://github.com/ORG/REPO/signing-key.pub
  authzer config trust add-key https://github.com/ORG/REPO/signing-key.pub?ref=v1`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := addTrustedKeyFromFile(args[0]); err != nil {
			return err
		}
		logHuman("Added SSH key from %s to trusted sources.\n", args[0])
		return nil
	},
}

var trustRemoveKeyCmd = &cobra.Command{
	Use:   "remove-key FINGERPRINT",
	Short: "Remove an SSH key by fingerprint or comment",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := removeTrustedKey(args[0]); err != nil {
			return err
		}
		logHuman("Removed SSH key %q from trusted sources.\n", args[0])
		return nil
	},
}

var trustListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all trusted sources",
	RunE: func(_ *cobra.Command, _ []string) error {
		reg, err := loadRegistry()
		if err != nil {
			return err
		}
		if reg == nil {
			logHuman("No trusted sources configured.\n")
			return nil
		}

		empty := true

		if len(reg.TrustedSources) > 0 {
			empty = false
			logHuman("Domains:\n")
			for _, d := range reg.TrustedSources {
				logHuman("  %s\n", d)
			}
		}

		if len(reg.TrustedIdentities) > 0 {
			empty = false
			logHuman("Sigstore identities:\n")
			for _, id := range reg.TrustedIdentities {
				if id.Issuer != "" {
					logHuman("  %s (issuer: %s)\n", id.Subject, id.Issuer)
				} else {
					logHuman("  %s\n", id.Subject)
				}
			}
		}

		if len(reg.TrustedKeys) > 0 {
			empty = false
			logHuman("SSH keys:\n")
			for _, k := range reg.TrustedKeys {
				if k.Comment != "" {
					logHuman("  %s  %s\n", k.Fingerprint, k.Comment)
				} else {
					logHuman("  %s\n", k.Fingerprint)
				}
			}
		}

		if empty {
			logHuman("No trusted sources configured.\n")
		}
		return nil
	},
}

func init() {
	trustAddIdentityCmd.Flags().String("issuer", "", "OIDC issuer URL (e.g. https://github.com/login/oauth)")

	configTrustCmd.AddCommand(
		trustAddCmd,
		trustRemoveCmd,
		trustAddIdentityCmd,
		trustRemoveIdentityCmd,
		trustAddKeyCmd,
		trustRemoveKeyCmd,
		trustListCmd,
	)
	configCmd.AddCommand(configTrustCmd)
}
