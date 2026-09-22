package backup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/saskia-peters/gear/internal/admin/core"
)

func testParams(mechanism string) core.BackupTestParams {
	return core.BackupTestParams{
		Mechanism: mechanism, Endpoint: "127.0.0.1", BucketOrPath: "/tmp",
		Username: "svc", Password: "geheim",
	}
}

func TestLocalRoundTrip(t *testing.T) {
	// TEST_OK (local): create + write + verify + delete a test file in the
	// configured path — a full round-trip that leaves no residue.
	dir := t.TempDir()
	params := testParams(core.BackupMechanismLocal)
	params.BucketOrPath = dir
	params.Endpoint = ""

	if err := NewTester().Test(context.Background(), params); err != nil {
		t.Fatalf("local Test err = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir err = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("leftover files after test: %+v", entries)
	}
}

func TestLocalMissingDir(t *testing.T) {
	// TEST_FAIL (local): a missing/unwritable path fails the test.
	params := testParams(core.BackupMechanismLocal)
	params.BucketOrPath = filepath.Join(t.TempDir(), "does-not-exist")
	params.Endpoint = ""
	err := NewTester().Test(context.Background(), params)
	if err == nil {
		t.Fatal("local Test(err) = nil, want error for a missing path")
	}
}

// recordedS3Request captures one request the test S3 server saw.
type recordedS3Request struct {
	method   string
	path     string
	auth     string
	sha      string
	date     string
	verified bool
}

// verifyS3Request independently re-derives the expected AWS SigV4 signature
// for the received request (from its method, URI, headers and body) using the
// known secret, and reports whether it matches the received Authorization
// header. This is the test-side oracle: the tester's signing is only accepted
// when a request it produced passes this derivation (finding: signature never
// validated).
func verifyS3Request(r *http.Request, secret, region string) error {
	date := r.Header.Get("x-amz-date")
	if date == "" {
		return fmt.Errorf("missing x-amz-date")
	}
	payloadHashHex := r.Header.Get("x-amz-content-sha256")
	if payloadHashHex == "" {
		return fmt.Errorf("missing x-amz-content-sha256")
	}
	auth := r.Header.Get("Authorization")
	cred := authValue(auth, "Credential=")
	gotSig := authValue(auth, "Signature=")
	signedHeaders := authValue(auth, "SignedHeaders=")
	if cred == "" || gotSig == "" || signedHeaders == "" {
		return fmt.Errorf("malformed Authorization header")
	}
	// The body must match the declared payload hash.
	body, _ := io.ReadAll(r.Body)
	if hashHex(string(body)) != payloadHashHex {
		return fmt.Errorf("payload hash mismatch")
	}
	// Only host;x-amz-content-sha256;x-amz-date is acceptable here (the tester
	// signs exactly those three).
	if signedHeaders != "host;x-amz-content-sha256;x-amz-date" {
		return fmt.Errorf("unexpected SignedHeaders %q", signedHeaders)
	}
	canonicalURI := r.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	cr := sigV4CanonicalRequest(r.Method, canonicalURI, r.URL.RawQuery, r.Host, date, payloadHashHex)
	scope := date[:8] + "/" + region + "/s3/aws4_request"
	sts := sigV4StringToSign(date, scope, hashHex(cr))
	key := sigV4SigningKey(secret, date[:8], region)
	want := hex.EncodeToString(hmacSHA256(key, sts))
	if !strings.EqualFold(gotSig, want) {
		return fmt.Errorf("signature mismatch: got %s want %s", gotSig, want)
	}
	return nil
}

// authValue extracts the value of the named comma-separated Authorization
// parameter (Credential=, SignedHeaders=, Signature=). Unlike a strict split
// on commas this also handles the Credential= parameter that follows the
// "AWS4-HMAC-SHA256 " prefix in the same comma-segment.
func authValue(auth, key string) string {
	idx := strings.Index(auth, key)
	if idx < 0 {
		return ""
	}
	rest := auth[idx+len(key):]
	if i := strings.IndexAny(rest, ", "); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// startS3TestServer runs an in-process S3-compatible endpoint that
// INDEPENDENTLY validates the AWS SigV4 signature of every request before
// answering (a bad signature is rejected with 403, regardless of putStatus);
// valid PUTs answer putStatus and valid DELETEs answer 204. Every request is
// recorded.
func startS3TestServer(t *testing.T, putStatus int) (string, *[]recordedS3Request) {
	t.Helper()
	var mu sync.Mutex
	reqs := []recordedS3Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verr := verifyS3Request(r, "geheim", sigV4Region)
		mu.Lock()
		reqs = append(reqs, recordedS3Request{
			method: r.Method, path: r.URL.Path,
			auth: r.Header.Get("Authorization"),
			sha:  r.Header.Get("x-amz-content-sha256"),
			date: r.Header.Get("x-amz-date"),
			verified: verr == nil,
		})
		mu.Unlock()
		if verr != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(putStatus)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &reqs
}

func TestS3TestOK(t *testing.T) {
	// TEST_OK (s3): a hand-rolled SigV4 PUT against the in-process server is
	// signed correctly (independently re-verified), accepted with 2xx, and the
	// test object is deleted best-effort.
	endpoint, reqs := startS3TestServer(t, http.StatusOK)
	params := testParams(core.BackupMechanismS3)
	params.Endpoint = endpoint
	params.BucketOrPath = "bucket"

	if err := NewTester().Test(context.Background(), params); err != nil {
		t.Fatalf("s3 Test err = %v", err)
	}

	if len(*reqs) < 1 {
		t.Fatal("no PUT request received")
	}
	put := (*reqs)[0]
	if put.method != http.MethodPut {
		t.Errorf("first method = %s, want PUT", put.method)
	}
	if !strings.HasPrefix(put.path, "/bucket/gear-backup-test-") {
		t.Errorf("PUT path = %q, want /bucket/gear-backup-test-...", put.path)
	}
	if put.date == "" || put.sha == "" {
		t.Errorf("missing sigv4 headers: date=%q sha=%q", put.date, put.sha)
	}
	if !strings.Contains(put.auth, "AWS4-HMAC-SHA256") || !strings.Contains(put.auth, "Credential=svc/") {
		t.Errorf("Authorization = %q, want AWS4-HMAC-SHA256 Credential=svc/", put.auth)
	}
	if !strings.Contains(put.auth, "Signature=") {
		t.Errorf("Authorization = %q, want a Signature", put.auth)
	}
	// The server only answers 200 when the independent derivation matched, so
	// this is the real signature-validity assertion (finding).
	if !put.verified {
		t.Errorf("PUT signature was NOT accepted by the independent validator: %+v", put)
	}

	// Best-effort DELETE of the test object (also signed correctly).
	var sawDelete, deleteVerified bool
	for _, r := range *reqs {
		if r.method == http.MethodDelete {
			sawDelete = true
			deleteVerified = r.verified
			break
		}
	}
	if !sawDelete {
		t.Error("the test object was not deleted best-effort")
	}
	if !deleteVerified {
		t.Error("DELETE signature was NOT accepted by the independent validator")
	}
}

func TestS3ServerRejectsBadSignature(t *testing.T) {
	// A request with a tampered signature must be rejected with 403 by the
	// independent validator — the server's 2xx is NOT granted on shape alone.
	endpoint, _ := startS3TestServer(t, http.StatusOK)
	body := []byte("gear-backup-test")
	bodyHash := sha256.Sum256(body)
	amzDate := time.Now().UTC().Format("20060102T150405Z")

	req, err := http.NewRequest(http.MethodPut, endpoint+"/bucket/evil", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	signRequest(req, "svc", "geheim", sigV4Region, amzDate, bodyHash[:])
	// Tamper with the signature after signing.
	auth := req.Header.Get("Authorization")
	auth = strings.Replace(auth, "Signature=", "Signature=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", 1)
	req.Header.Set("Authorization", auth)

	resp, err := (&http.Client{Timeout: protocolTimeout}).Do(req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("tampered request status = %d, want 403", resp.StatusCode)
	}
}

func TestS3TestNon2xxFails(t *testing.T) {
	// TEST_FAIL (s3): a non-2xx PUT answer fails the test.
	endpoint, _ := startS3TestServer(t, http.StatusForbidden)
	params := testParams(core.BackupMechanismS3)
	params.Endpoint = endpoint
	params.BucketOrPath = "bucket"
	err := NewTester().Test(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("s3 Test err = %v, want an error mentioning HTTP 403", err)
	}
}

func TestS3TestNoBucket(t *testing.T) {
	params := testParams(core.BackupMechanismS3)
	params.Endpoint = "http://127.0.0.1:1"
	params.BucketOrPath = "   "
	if err := NewTester().Test(context.Background(), params); err == nil {
		t.Fatal("s3 Test(err) = nil, want error for a missing bucket")
	}
}

func TestS3TestNoCredential(t *testing.T) {
	// Finding: the tester guards itself — s3 without a credential fails before
	// any network attempt.
	params := testParams(core.BackupMechanismS3)
	params.Username = ""
	params.Password = ""
	if err := NewTester().Test(context.Background(), params); err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("s3 Test(no credential) err = %v, want a credential error", err)
	}
}

// TestSigV4KnownAnswer pins the signing-key chain and the canonical request
// against an independently computed vector (verified with a separate Python
// implementation): fixed inputs → fixed canonical request, string-to-sign,
// canonical-request hash and final signature. Any regression in the signer
// breaks this test even when the shape-level checks stay green.
func TestSigV4KnownAnswer(t *testing.T) {
	const (
		accessKey      = "AKIAIOSFODNN7EXAMPLE"
		secretKey      = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
		region         = "us-east-1"
		amzDate        = "20130524T000000Z"
		payloadHashHex = "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072"
	)

	wantCanonicalRequest := "PUT\n" +
		"/test.txt\n" +
		"\n" +
		"host:examplebucket.s3.amazonaws.com\n" +
		"x-amz-content-sha256:" + payloadHashHex + "\n" +
		"x-amz-date:" + amzDate + "\n" +
		"\n" +
		"host;x-amz-content-sha256;x-amz-date\n" +
		payloadHashHex

	got := sigV4CanonicalRequest("PUT", "/test.txt", "", "examplebucket.s3.amazonaws.com", amzDate, payloadHashHex)
	if got != wantCanonicalRequest {
		t.Errorf("canonical request mismatch:\n got %q\nwant %q", got, wantCanonicalRequest)
	}

	crHash := hashHex(got)
	const wantCRHash = "05ecaa5cb802d3cae43160ff8272e7d18a15ec7fa85fb35771c97d4e56c9c6bc"
	if crHash != wantCRHash {
		t.Errorf("canonical request hash = %s, want %s", crHash, wantCRHash)
	}

	scope := "20130524/us-east-1/s3/aws4_request"
	wantStringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + wantCRHash
	if got := sigV4StringToSign(amzDate, scope, crHash); got != wantStringToSign {
		t.Errorf("string-to-sign mismatch:\n got %q\nwant %q", got, wantStringToSign)
	}

	key := sigV4SigningKey(secretKey, "20130524", region)
	sig := hex.EncodeToString(hmacSHA256(key, wantStringToSign))
	const wantSignature = "e8a654937e21054a5da5e95e82cff2d3ee352d5aa6e0d966385c61adda282699"
	if sig != wantSignature {
		t.Errorf("signature = %s, want %s", sig, wantSignature)
	}

	// The full signRequest path must land on the same Authorization signature.
	req, err := http.NewRequest(http.MethodPut, "https://examplebucket.s3.amazonaws.com/test.txt", bytes.NewReader([]byte("Welcome to Amazon S3.")))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	bodyHash := sha256.Sum256([]byte("Welcome to Amazon S3."))
	signRequest(req, accessKey, secretKey, region, amzDate, bodyHash[:])
	auth := req.Header.Get("Authorization")
	if !strings.Contains(auth, "Signature="+wantSignature) {
		t.Errorf("Authorization = %q, want Signature=%s", auth, wantSignature)
	}
}

// startFTPTestServer runs an in-process minimal FTP server. failPass makes the
// PASS step reject the login; multilineBanner makes the greeting an RFC 959
// multi-line reply (220-.../220 ...) so the multi-line parser is exercised.
func startFTPTestServer(t *testing.T, failPass, multilineBanner bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				write := func(line string) { _, _ = io.WriteString(c, line+"\r\n") }
				if multilineBanner {
					write("220-Welcome to the test server")
					write("220 more greeting lines")
				} else {
					write("220 ftp test ready")
				}
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimSpace(line)
					switch {
					case strings.HasPrefix(line, "USER"):
						write("331 password required")
					case strings.HasPrefix(line, "PASS"):
						if failPass {
							write("530 login incorrect")
						} else {
							write("230 logged in")
						}
					case strings.HasPrefix(line, "QUIT"):
						write("221 bye")
						return
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func TestFTPHandshakeOK(t *testing.T) {
	// TEST_OK (ftp): banner read + USER/PASS auth + QUIT complete successfully.
	params := testParams(core.BackupMechanismFTP)
	params.Endpoint = startFTPTestServer(t, false, false)
	if err := NewTester().Test(context.Background(), params); err != nil {
		t.Fatalf("ftp Test err = %v", err)
	}
}

func TestFTPMultiLineBanner(t *testing.T) {
	// Finding: an RFC 959 multi-line greeting (220-... continuation lines) must
	// be consumed in full so the USER/PASS exchange is not corrupted.
	params := testParams(core.BackupMechanismFTP)
	params.Endpoint = startFTPTestServer(t, false, true)
	if err := NewTester().Test(context.Background(), params); err != nil {
		t.Fatalf("ftp Test(multiline banner) err = %v", err)
	}
}

func TestFTPHandshakeAuthFail(t *testing.T) {
	// TEST_FAIL (ftp): the PASS step is rejected.
	params := testParams(core.BackupMechanismFTP)
	params.Endpoint = startFTPTestServer(t, true, false)
	err := NewTester().Test(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "PASS") {
		t.Fatalf("ftp Test err = %v, want a PASS rejection", err)
	}
}

func TestFTPNoCredential(t *testing.T) {
	// Finding: the tester guards itself — ftp without a credential fails
	// before any network attempt.
	params := testParams(core.BackupMechanismFTP)
	params.Username = ""
	params.Password = ""
	err := NewTester().Test(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("ftp Test(no credential) err = %v, want a credential error", err)
	}
}

func TestFTPUnreachable(t *testing.T) {
	// TEST_FAIL (ftp): connection refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // closed → refused

	params := testParams(core.BackupMechanismFTP)
	params.Endpoint = addr
	if err := NewTester().Test(context.Background(), params); err == nil {
		t.Fatal("ftp Test(unreachable) err = nil, want error")
	}
}

// startSSHTestServer runs an in-process SSH server with a password auth
// callback. acceptAuth selects whether the svc/geheim login is accepted.
func startSSHTestServer(t *testing.T, acceptAuth bool) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if acceptAuth && string(password) == "geheim" {
				return nil, nil
			}
			return nil, fmt.Errorf("auth rejected")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				go func() {
					for ch := range chans {
						_ = ch.Reject(ssh.UnknownChannelType, "no channels")
					}
				}()
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func TestSFTPHandshakeOK(t *testing.T) {
	// TEST_OK (sftp): dial + SSH password auth handshake + close succeeds via
	// the already-present x/crypto/ssh (no new dependency).
	params := testParams(core.BackupMechanismSFTP)
	params.Endpoint = startSSHTestServer(t, true)
	if err := NewTester().Test(context.Background(), params); err != nil {
		t.Fatalf("sftp Test err = %v", err)
	}
}

func TestSFTPHandshakeAuthFail(t *testing.T) {
	// TEST_FAIL (sftp): password auth is rejected.
	params := testParams(core.BackupMechanismSFTP)
	params.Endpoint = startSSHTestServer(t, false)
	err := NewTester().Test(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("sftp Test err = %v, want an auth handshake failure", err)
	}
}

func TestUnsupportedMechanism(t *testing.T) {
	params := testParams("nfs")
	if err := NewTester().Test(context.Background(), params); err == nil {
		t.Fatal("Test(err) = nil, want error for an unsupported mechanism")
	}
}

func TestTestTimeoutBounded(t *testing.T) {
	// The protocol timeout bounds a stalled server: a TCP listener that accepts
	// and never speaks must fail the FTP banner read within the timeout rather
	// than hang.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Accept and stay silent — never send a banner.
			_ = conn
		}
	}()

	params := testParams(core.BackupMechanismFTP)
	params.Endpoint = ln.Addr().String()
	start := time.Now()
	err = NewTester().Test(context.Background(), params)
	if err == nil {
		t.Fatal("ftp Test(stalled server) err = nil, want error")
	}
	if elapsed := time.Since(start); elapsed > protocolTimeout+5*time.Second {
		t.Errorf("test took %v, want bounded by the protocol timeout", elapsed)
	}
}

func TestSFTPNoCredential(t *testing.T) {
	// Finding: the tester guards itself — sftp without a credential fails
	// before any network attempt.
	params := testParams(core.BackupMechanismSFTP)
	params.Username = ""
	params.Password = ""
	err := NewTester().Test(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("sftp Test(no credential) err = %v, want a credential error", err)
	}
}

func TestLocalContextCanceled(t *testing.T) {
	// Finding: the local test honors a canceled context instead of proceeding
	// with the file round-trip.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	params := testParams(core.BackupMechanismLocal)
	params.BucketOrPath = t.TempDir()
	params.Endpoint = ""
	err := NewTester().Test(ctx, params)
	if err == nil {
		t.Fatal("local Test(canceled ctx) err = nil, want context error")
	}
	if err != context.Canceled && err.Error() != "context canceled" {
		t.Errorf("err = %v, want the context cancellation surfaced", err)
	}
}

func TestDialAddr(t *testing.T) {
	// Finding: default-port append + scheme rejection are unit-tested.
	cases := []struct {
		name      string
		endpoint  string
		defaultP  string
		want      string
		wantError bool
	}{
		{"empty endpoint", "", "21", "", true},
		{"host without port appends default", "ftp.example.com", "21", "ftp.example.com:21", false},
		{"sftp host appends 22", "sftp.example.com", "22", "sftp.example.com:22", false},
		{"explicit port kept", "ftp.example.com:2121", "21", "ftp.example.com:2121", false},
		{"ipv6 host with port", "[::1]:2121", "21", "[::1]:2121", false},
		{"scheme-bearing endpoint rejected", "ftp://ftp.example.com", "21", "", true},
		{"https URL rejected", "https://s3.example.com", "21", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dialAddr(tc.endpoint, tc.defaultP)
			if tc.wantError {
				if err == nil {
					t.Fatalf("dialAddr(%q) err = nil, want error", tc.endpoint)
				}
				return
			}
			if err != nil {
				t.Fatalf("dialAddr(%q) err = %v", tc.endpoint, err)
			}
			if got != tc.want {
				t.Errorf("dialAddr(%q) = %q, want %q", tc.endpoint, got, tc.want)
			}
		})
	}
}