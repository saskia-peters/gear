// Package backup hosts the test-connection adapter of the Admin hexagon (Story
// 3.2, FR-29/AD-15): it implements the BackupDestinationTester port the core
// uses for the "Verbindung testen" action. Per the user's stdlib-partial depth
// decision it exercises each mechanism as follows:
//
//   - local: real round-trip — create + write + delete a test file in the
//     configured path.
//   - s3: minimal hand-rolled AWS SigV4 PUT via stdlib net/http (no SDK) — PUT
//     a test object, verify 2xx, best-effort delete.
//   - ftp: stdlib net only — TCP dial, banner read, USER/PASS auth attempt,
//     QUIT.
//   - sftp: SSH auth handshake via the ALREADY-PRESENT golang.org/x/crypto/ssh
//     (no new dependency) — dial, password auth, close.
//
// No protocol-level file read/write for FTP/SFTP (reachability + auth
// handshake only, per the user decision). All mechanisms are bounded by a dial
// + protocol timeout. Failures return a detailed error that the core logs
// structured (NFR-O1) while surfacing only a generic German message inline (no
// endpoint/TLS detail leak).
package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/saskia-peters/gear/internal/admin/core"
)

const (
	// dialTimeout bounds a single TCP/TLS dial (avoids hanging dials).
	dialTimeout = 10 * time.Second
	// protocolTimeout bounds the protocol exchange AFTER the connection is
	// established (FTP banner/auth, SSH handshake, the S3 HTTP request): a
	// stalled server cannot block the test indefinitely.
	protocolTimeout = 10 * time.Second
	// sigV4Region is the signing region for the minimal S3 SigV4 PUT. S3-
	// compatible endpoints (MinIO, SeaweedFS, ...) typically ignore the region
	// for path-style requests; us-east-1 is the AWS default.
	sigV4Region = "us-east-1"
)

// Tester implements the core's BackupDestinationTester port (Story 3.2).
type Tester struct{}

// NewTester constructs the test-connection adapter.
func NewTester() *Tester { return &Tester{} }

// Test exercises one destination's mechanism/endpoint (see the package
// comment for the per-mechanism depth). The plaintext credential exists only
// in params, in memory (NFR-S4). S3/FTP/SFTP reject an empty credential here,
// independent of the core's create/update rules (the tester is the public
// seam the future backup job will use and must guard itself, finding).
func (t *Tester) Test(ctx context.Context, params core.BackupTestParams) error {
	switch params.Mechanism {
	case core.BackupMechanismLocal:
		return t.testLocal(ctx, params)
	case core.BackupMechanismS3:
		return t.testS3(ctx, params)
	case core.BackupMechanismFTP:
		return t.testFTP(ctx, params)
	case core.BackupMechanismSFTP:
		return t.testSFTP(ctx, params)
	default:
		return fmt.Errorf("backup tester: unsupported mechanism %q", params.Mechanism)
	}
}

// requireCredential rejects a test call with an empty username/password for
// the credentialed mechanisms (finding: defensive guard at the port boundary).
func requireCredential(mechanism string, params core.BackupTestParams) error {
	if params.Username == "" || params.Password == "" {
		return fmt.Errorf("backup tester: %s: a credential (username + password) is required", mechanism)
	}
	return nil
}

// testLocal does the full local round-trip (FR-29, user decision): create +
// write + verify + delete a test file inside the configured path. A missing or
// unwritable directory fails the test. The context is honored: a canceled or
// deadline-exceeded test aborts (finding).
func (t *Tester) testLocal(ctx context.Context, params core.BackupTestParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := params.BucketOrPath
	if dir == "" {
		return fmt.Errorf("backup tester: local: no path configured")
	}
	f, err := os.CreateTemp(dir, ".gear-backup-test-*")
	if err != nil {
		return fmt.Errorf("backup tester: local: cannot create a test file in the configured path: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }

	if _, err := f.Write([]byte("gear-backup-test")); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("backup tester: local: cannot write the test file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("backup tester: local: cannot close the test file: %w", err)
	}
	if _, err := os.Stat(path); err != nil {
		cleanup()
		return fmt.Errorf("backup tester: local: the test file is not readable: %w", err)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return err
	}
	cleanup()
	return nil
}

// testS3 performs the minimal hand-rolled AWS SigV4 PUT (user decision): PUT a
// test object into the configured bucket, verify a 2xx response, then a
// best-effort DELETE of that object. No SDK — stdlib net/http only. The
// endpoint is expected to be an S3-compatible base URL (scheme://host[:port]
// [/prefix]); the bucket lives in bucket_or_path.
func (t *Tester) testS3(ctx context.Context, params core.BackupTestParams) error {
	if err := requireCredential("s3", params); err != nil {
		return err
	}
	if params.Endpoint == "" {
		return fmt.Errorf("backup tester: s3: no endpoint configured")
	}
	bucket := strings.Trim(params.BucketOrPath, "/")
	if bucket == "" {
		return fmt.Errorf("backup tester: s3: no bucket configured")
	}
	key := fmt.Sprintf("gear-backup-test-%d", time.Now().UnixNano())

	objectURL := strings.TrimRight(params.Endpoint, "/") + "/" + bucket + "/" + key
	body := []byte("gear-backup-test")
	amzDate := time.Now().UTC().Format("20060102T150405Z")

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("backup tester: s3: building the PUT request failed: %w", err)
	}
	bodyHash := sha256.Sum256(body)
	signRequest(req, params.Username, params.Password, sigV4Region, amzDate, bodyHash[:])

	client := &http.Client{Timeout: protocolTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("backup tester: s3: PUT failed: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("backup tester: s3: PUT answered HTTP %d", resp.StatusCode)
	}

	// Best-effort delete of the test object (a failed cleanup must not fail the
	// reachability test).
	delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, objectURL, nil)
	if err == nil {
		emptyHash := sha256.Sum256(nil)
		signRequest(delReq, params.Username, params.Password, sigV4Region, time.Now().UTC().Format("20060102T150405Z"), emptyHash[:])
		if delResp, delErr := client.Do(delReq); delErr == nil {
			_, _ = io.Copy(io.Discard, delResp.Body)
			_ = delResp.Body.Close()
		}
	}
	return nil
}

// signRequest mutates req with the AWS SigV4 (AWS4-HMAC-SHA256) authorization
// headers for a single request. The payload hash is supplied by the caller
// (the hashed body for the actual method, the empty hash for DELETE). The
// canonical-request / string-to-sign / signing-key construction is factored
// into pure helpers (sigV4CanonicalRequest, sigV4StringToSign, sigV4SigningKey)
// so a known-answer test can pin the chain independently.
func signRequest(req *http.Request, accessKey, secretKey, region, amzDate string, payloadHash []byte) {
	host := req.URL.Host
	payloadHashHex := hex.EncodeToString(payloadHash)
	req.Header.Set("x-amz-content-sha256", payloadHashHex)
	req.Header.Set("x-amz-date", amzDate)

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalRequest := sigV4CanonicalRequest(req.Method, canonicalURI, req.URL.RawQuery, host, amzDate, payloadHashHex)

	scope := amzDate[:8] + "/" + region + "/s3/aws4_request"
	stringToSign := sigV4StringToSign(amzDate, scope, hashHex(canonicalRequest))
	signingKey := sigV4SigningKey(secretKey, amzDate[:8], region)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization",
		fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=%s",
			accessKey, scope, signature))
}

// sigV4CanonicalRequest assembles the AWS SigV4 canonical request for the
// fixed header set this tester signs (host;x-amz-content-sha256;x-amz-date).
func sigV4CanonicalRequest(method, canonicalURI, query, host, amzDate, payloadHashHex string) string {
	return method + "\n" + canonicalURI + "\n" + query + "\n" +
		"host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHashHex + "\n" +
		"x-amz-date:" + amzDate + "\n" +
		"\n" +
		"host;x-amz-content-sha256;x-amz-date\n" +
		payloadHashHex
}

// sigV4StringToSign assembles the AWS SigV4 string-to-sign for a scope of the
// form "<date>/<region>/s3/aws4_request".
func sigV4StringToSign(amzDate, scope, canonicalRequestHash string) string {
	return "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + canonicalRequestHash
}

// sigV4SigningKey derives the AWS SigV4 signing key for the s3 service.
func sigV4SigningKey(secret, date, region string) []byte {
	key := hmacSHA256([]byte("AWS4"+secret), date)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	return hmacSHA256(key, "aws4_request")
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(data))
	return m.Sum(nil)
}

// testFTP performs a stdlib-only reachability + auth handshake (user
// decision): TCP dial, banner read (220), USER/PASS auth attempt, QUIT. No
// protocol-level file transfer.
func (t *Tester) testFTP(ctx context.Context, params core.BackupTestParams) error {
	if err := requireCredential("ftp", params); err != nil {
		return err
	}
	addr, err := dialAddr(params.Endpoint, "21")
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("backup tester: ftp: connection failed: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(protocolTimeout)); err != nil {
		return fmt.Errorf("backup tester: ftp: setting deadline failed: %w", err)
	}
	if err := ftpExpect(conn, "220"); err != nil {
		return err
	}
	code, err := ftpCommand(conn, "USER "+params.Username)
	if err != nil {
		return err
	}
	// 230 = already logged in (no password needed), 331 = password required.
	switch code {
	case "230", "331":
	default:
		return fmt.Errorf("backup tester: ftp: USER answered %s", code)
	}
	if code == "331" {
		pcode, err := ftpCommand(conn, "PASS "+params.Password)
		if err != nil {
			return err
		}
		if pcode != "230" {
			return fmt.Errorf("backup tester: ftp: PASS answered %s", pcode)
		}
	}
	_, _ = ftpCommand(conn, "QUIT")
	return nil
}

// ftpCommand writes a CRLF-terminated FTP command and reads the response code.
func ftpCommand(conn net.Conn, cmd string) (string, error) {
	if _, err := conn.Write([]byte(cmd + "\r\n")); err != nil {
		return "", fmt.Errorf("backup tester: ftp: sending %q failed: %w", cmd, err)
	}
	return ftpReadCode(conn)
}

// ftpReadCode reads one FTP reply and returns its 3-digit code. RFC 959
// multi-line replies (first line "NNN-...", continuation lines, terminated by
// "NNN <text>") are consumed in full so a multi-line greeting cannot corrupt
// the following USER/PASS exchange (finding).
func ftpReadCode(conn net.Conn) (string, error) {
	first, err := ftpReadLine(conn)
	if err != nil {
		return "", fmt.Errorf("backup tester: ftp: reading the response failed: %w", err)
	}
	code := ""
	if len(first) >= 3 {
		code = first[:3]
	} else {
		return "", fmt.Errorf("backup tester: ftp: malformed response %q", first)
	}
	if len(first) >= 4 && first[3] == '-' {
		for {
			line, err := ftpReadLine(conn)
			if err != nil {
				return "", fmt.Errorf("backup tester: ftp: reading the response failed: %w", err)
			}
			if len(line) >= 4 && line[:3] == code && line[3] == ' ' {
				break
			}
		}
	}
	return code, nil
}

// ftpReadLine reads one CRLF-terminated line (bounded length) from the conn.
func ftpReadLine(conn net.Conn) (string, error) {
	var line []byte
	for {
		buf := make([]byte, 1)
		n, err := conn.Read(buf)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		line = append(line, buf[0])
		if buf[0] == '\n' {
			break
		}
		if len(line) > 4096 {
			return "", fmt.Errorf("backup tester: ftp: response line too long")
		}
	}
	return strings.TrimRight(string(line), "\r\n"), nil
}

// ftpExpect reads the next FTP reply and requires the given 3-digit code.
func ftpExpect(conn net.Conn, wantCode string) error {
	code, err := ftpReadCode(conn)
	if err != nil {
		return err
	}
	if code != wantCode {
		return fmt.Errorf("backup tester: ftp: expected %s, answered %s", wantCode, code)
	}
	return nil
}

// testSFTP performs an SSH auth handshake (user decision): dial, password
// auth, close — via the already-present golang.org/x/crypto/ssh (no NEW
// dependency). No protocol-level file access. The host key is deliberately not
// verified (reachability handshake only — no trusted-key relationship exists
// yet).
func (t *Tester) testSFTP(ctx context.Context, params core.BackupTestParams) error {
	if err := requireCredential("sftp", params); err != nil {
		return err
	}
	addr, err := dialAddr(params.Endpoint, "22")
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("backup tester: sftp: connection failed: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:    params.Username,
		Auth:    []ssh.AuthMethod{ssh.Password(params.Password)},
		Timeout: protocolTimeout,
		// Reachability + auth handshake only: no host-key trust exists yet, so
		// the test does not verify the key (it carries no data).
		HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("backup tester: sftp: SSH auth handshake failed: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	_ = client.Close() // sends DISCONNECT; failure is irrelevant to the handshake test
	return nil
}

// dialAddr returns the host:port dial address, appending the given default
// port when the endpoint carries none. A scheme-bearing endpoint
// (ftp://host, s3://...) is rejected — it is not a raw dial address.
func dialAddr(endpoint, defaultPort string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("backup tester: no endpoint configured")
	}
	// Check the scheme BEFORE SplitHostPort: for "ftp://host" the last-colon
	// split would "succeed" with host="ftp" (finding: scheme rejection).
	if strings.Contains(endpoint, "://") {
		return "", fmt.Errorf("backup tester: endpoint must be host[:port], got %q", endpoint)
	}
	if _, _, err := net.SplitHostPort(endpoint); err != nil {
		return net.JoinHostPort(endpoint, defaultPort), nil
	}
	return endpoint, nil
}

// Compile-time check: the adapter implements the port the core consumes.
var _ core.BackupDestinationTester = (*Tester)(nil)