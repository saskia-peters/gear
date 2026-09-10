package smtp

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
	adminports "github.com/saskia-peters/gear/internal/admin/ports"
)

// fakeSettingsPort implements the read-only SmtpSettingsPort the sender
// consumes (AD-14).
type fakeSettingsPort struct {
	settings *admcore.SmtpSettings
	err      error
}

func (f *fakeSettingsPort) CurrentSmtpSettings(context.Context) (*admcore.SmtpSettings, error) {
	return f.settings, f.err
}

// fakeCipher round-trips "enc:"-prefixed values.
type fakeCipher struct {
	decryptErr error
}

func (f *fakeCipher) Encrypt(plaintext string) (string, error) { return "enc:" + plaintext, nil }
func (f *fakeCipher) Decrypt(encoded string) (string, error) {
	if f.decryptErr != nil {
		return "", f.decryptErr
	}
	if !strings.HasPrefix(encoded, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(encoded, "enc:"), nil
}

var _ adminports.SmtpSettingsPort = (*fakeSettingsPort)(nil)

// testTLSCert returns a self-signed test CA and a server certificate issued by
// it (valid for 127.0.0.1 + localhost), plus the root pool that trusts the CA,
// so the client can verify the in-process TLS test server.
func testTLSCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "G.E.A.R. test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating CA certificate: %v", err)
	}
	caLeaf, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parsing CA certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caLeaf)

	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating server key: %v", err)
	}
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, caTmpl, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating server certificate: %v", err)
	}
	serverCert := tls.Certificate{Certificate: [][]byte{srvDER}, PrivateKey: srvKey}
	return serverCert, pool
}

// capturedMail is one message observed by the in-process SMTP test server.
type capturedMail struct {
	from string
	to   string
	data string
}

// smtpTestServer is a minimal in-process SMTP server speaking plain, STARTTLS
// or implicit-TLS. It captures the envelope + DATA of every message.
type smtpTestServer struct {
	addr  string
	mode  string
	cert  tls.Certificate
	pool  *x509.CertPool
	mu    sync.Mutex
	mails []capturedMail
}

func startSMTPTestServer(t *testing.T, mode string) *smtpTestServer {
	t.Helper()
	cert, pool := testTLSCert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &smtpTestServer{addr: ln.Addr().String(), mode: mode, cert: cert, pool: pool}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *smtpTestServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if s.mode == "tls" {
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		conn = tlsConn
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(line string) {
		_, _ = w.WriteString(line)
		_ = w.Flush()
	}
	write("220 localhost ESMTP\r\n")

	var from, to string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "EHLO"):
			write("250-localhost\r\n250-8BITMIME\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(line, "STARTTLS"):
			write("220 2.0.0 Ready to start TLS\r\n")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
		case strings.HasPrefix(line, "AUTH"):
			write("235 2.7.0 Authentication successful\r\n")
		case strings.HasPrefix(line, "MAIL FROM"):
			from = parseEnvelopeAddr(line, "MAIL FROM:")
			write("250 2.1.0 OK\r\n")
		case strings.HasPrefix(line, "RCPT TO"):
			to = parseEnvelopeAddr(line, "RCPT TO:")
			write("250 2.1.5 OK\r\n")
		case strings.HasPrefix(line, "DATA"):
			write("354 End data with <CR><LF>.<CR><LF>\r\n")
			var data strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" {
					break
				}
				data.WriteString(dl)
			}
			s.record(capturedMail{from: from, to: to, data: data.String()})
			write("250 2.0.0 queued\r\n")
		case strings.HasPrefix(line, "QUIT"):
			write("221 2.0.0 Bye\r\n")
			return
		}
	}
}

// parseEnvelopeAddr extracts the bare address from an SMTP envelope line like
// "MAIL FROM:<a@b.de> BODY=8BITMIME" (strips angle brackets and any params).
func parseEnvelopeAddr(line, prefix string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return strings.Trim(rest, "<>")
}

func (s *smtpTestServer) record(m capturedMail) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mails = append(s.mails, m)
}

func (s *smtpTestServer) collectedMails() []capturedMail {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]capturedMail, len(s.mails))
	copy(out, s.mails)
	return out
}

func (s *smtpTestServer) port() int {
	_, portStr, err := net.SplitHostPort(s.addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(portStr)
	return n
}

// newSender builds a ResetEmailSender against a fake settings port.
func newSender(settings *admcore.SmtpSettings) *ResetEmailSender {
	return NewResetEmailSender(&fakeSettingsPort{settings: settings}, &fakeCipher{}, nil)
}

func TestConfigured(t *testing.T) {
	valid := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: 25, Security: admcore.SmtpSecurityNone,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:pw",
	}
	cases := []struct {
		name     string
		settings *admcore.SmtpSettings
		portErr  error
		cipher   *fakeCipher
		want     bool
	}{
		{"no settings row", nil, nil, &fakeCipher{}, false},
		{"empty host", &admcore.SmtpSettings{Host: "", Port: 25, Security: admcore.SmtpSecurityNone, SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:pw"}, nil, &fakeCipher{}, false},
		{"empty sender", &admcore.SmtpSettings{Host: "127.0.0.1", Port: 25, Security: admcore.SmtpSecurityNone, SenderAddress: "", PasswordEncrypted: "enc:pw"}, nil, &fakeCipher{}, false},
		{"anonymous none relay (no password)", &admcore.SmtpSettings{Host: "127.0.0.1", Port: 25, Security: admcore.SmtpSecurityNone, SenderAddress: "noreply@example.com", PasswordEncrypted: ""}, nil, &fakeCipher{}, true},
		{"anonymous starttls relay (no password)", &admcore.SmtpSettings{Host: "127.0.0.1", Port: 587, Security: admcore.SmtpSecurityStartTLS, SenderAddress: "noreply@example.com", PasswordEncrypted: ""}, nil, &fakeCipher{}, true},
		{"implicit tls without password", &admcore.SmtpSettings{Host: "127.0.0.1", Port: 465, Security: admcore.SmtpSecurityTLS, SenderAddress: "noreply@example.com", PasswordEncrypted: ""}, nil, &fakeCipher{}, false},
		{"username without password", &admcore.SmtpSettings{Host: "127.0.0.1", Port: 25, Security: admcore.SmtpSecurityNone, SenderAddress: "noreply@example.com", Username: "u", PasswordEncrypted: ""}, nil, &fakeCipher{}, false},
		{"username + password", &admcore.SmtpSettings{Host: "127.0.0.1", Port: 25, Security: admcore.SmtpSecurityNone, SenderAddress: "noreply@example.com", Username: "u", PasswordEncrypted: "enc:pw"}, nil, &fakeCipher{}, true},
		{"valid configured", valid, nil, &fakeCipher{}, true},
		{"undecryptable password", valid, nil, &fakeCipher{decryptErr: errors.New("bad")}, false},
		{"settings read error", valid, errors.New("db down"), &fakeCipher{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := NewResetEmailSender(&fakeSettingsPort{settings: tc.settings, err: tc.portErr}, tc.cipher, nil)
			if got := sender.Configured(); got != tc.want {
				t.Errorf("Configured() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSendPasswordResetEmailPlain(t *testing.T) {
	// SEND_RESET: plain (none) security end-to-end — the in-process server
	// captures the envelope and the body carrying the clickable reset link.
	srv := startSMTPTestServer(t, "none")
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: srv.port(), Security: admcore.SmtpSecurityNone,
		SenderAddress: "noreply@example.com", SenderName: "G.E.A.R.", PasswordEncrypted: "enc:pw",
	}
	sender := newSender(settings)

	const resetLink = "http://localhost:5173/reset-password/rawtoken123"
	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", resetLink); err != nil {
		t.Fatalf("SendPasswordResetEmail err = %v", err)
	}
	mails := srv.collectedMails()
	if len(mails) != 1 {
		t.Fatalf("delivered mails = %d, want 1", len(mails))
	}
	if mails[0].from != "noreply@example.com" || mails[0].to != "user@example.com" {
		t.Errorf("envelope = %s -> %s, want noreply -> user", mails[0].from, mails[0].to)
	}
	if !strings.Contains(mails[0].data, resetLink) {
		t.Errorf("body does not contain the reset link:\n%s", mails[0].data)
	}
	if !strings.Contains(mails[0].data, "Subject: =?UTF-8?Q?") {
		t.Errorf("subject not RFC 2047 encoded:\n%s", mails[0].data)
	}
	// The From header renders the RFC 5322 display-name form (finding 2).
	if !strings.Contains(mails[0].data, `From: "G.E.A.R." <noreply@example.com>`) {
		t.Errorf("From header missing display-name form:\n%s", mails[0].data)
	}
	// RFC 5322 requires Date + Message-ID (finding 4); some relays reject
	// messages without them.
	if !strings.Contains(mails[0].data, "Date:") {
		t.Errorf("message missing Date header:\n%s", mails[0].data)
	}
	if !strings.Contains(mails[0].data, "Message-ID: <") {
		t.Errorf("message missing Message-ID header:\n%s", mails[0].data)
	}
}

func TestSendPasswordResetEmailAnonymousRelay(t *testing.T) {
	// A password-less no-auth relay (none + no username) delivers without a
	// password (finding 3).
	srv := startSMTPTestServer(t, "none")
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: srv.port(), Security: admcore.SmtpSecurityNone,
		SenderAddress: "noreply@example.com", SenderName: "G.E.A.R.",
	}
	sender := newSender(settings)
	if !sender.Configured() {
		t.Fatal("Configured() = false for a password-less no-auth relay, want true")
	}
	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok"); err != nil {
		t.Fatalf("SendPasswordResetEmail(anonymous) err = %v", err)
	}
	if len(srv.collectedMails()) != 1 {
		t.Fatalf("delivered mails = %d, want 1", len(srv.collectedMails()))
	}
}

func TestSendPasswordResetEmailNonASCIIFromName(t *testing.T) {
	// The From header's display name is RFC 2047-encoded when non-ASCII
	// (finding 2).
	srv := startSMTPTestServer(t, "none")
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: srv.port(), Security: admcore.SmtpSecurityNone,
		SenderAddress: "noreply@example.com", SenderName: "Geräteausgabe OV", PasswordEncrypted: "enc:pw",
	}
	sender := newSender(settings)
	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok"); err != nil {
		t.Fatalf("SendPasswordResetEmail err = %v", err)
	}
	mails := srv.collectedMails()
	if len(mails) != 1 {
		t.Fatalf("delivered mails = %d, want 1", len(mails))
	}
	if !strings.Contains(mails[0].data, "From: =?UTF-8?Q?") {
		t.Errorf("non-ASCII From name not RFC 2047 encoded:\n%s", mails[0].data)
	}
	if strings.Contains(mails[0].data, "Geräteausgabe") {
		t.Errorf("From header leaks raw UTF-8:\n%s", mails[0].data)
	}
}

func TestSendPasswordResetEmailWithAuth(t *testing.T) {
	// Sender with a username exercises the AUTH PLAIN path (server accepts).
	srv := startSMTPTestServer(t, "none")
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: srv.port(), Security: admcore.SmtpSecurityNone,
		SenderAddress: "noreply@example.com", Username: "smtpuser", PasswordEncrypted: "enc:pw",
	}
	sender := newSender(settings)
	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok"); err != nil {
		t.Fatalf("SendPasswordResetEmail err = %v", err)
	}
	if len(srv.collectedMails()) != 1 {
		t.Fatalf("delivered mails = %d, want 1", len(srv.collectedMails()))
	}
}

func TestSendPasswordResetEmailStartTLS(t *testing.T) {
	// STARTTLS security end-to-end: the client upgrades and delivers over TLS.
	srv := startSMTPTestServer(t, "starttls")
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: srv.port(), Security: admcore.SmtpSecurityStartTLS,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:pw",
	}
	sender := NewResetEmailSender(&fakeSettingsPort{settings: settings}, &fakeCipher{}, nil)
	// Inject the test root CA so the self-signed server cert verifies.
	sender.client = Client{TLSConfig: &tls.Config{RootCAs: srv.pool}}

	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok"); err != nil {
		t.Fatalf("SendPasswordResetEmail(STARTTLS) err = %v", err)
	}
	if len(srv.collectedMails()) != 1 {
		t.Fatalf("delivered mails = %d, want 1", len(srv.collectedMails()))
	}
}

func TestSendPasswordResetEmailImplicitTLS(t *testing.T) {
	// TLS (implicit, port 465) end-to-end: tls.Dial + smtp.NewClient over the
	// TLS connection.
	srv := startSMTPTestServer(t, "tls")
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: srv.port(), Security: admcore.SmtpSecurityTLS,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:pw",
	}
	sender := NewResetEmailSender(&fakeSettingsPort{settings: settings}, &fakeCipher{}, nil)
	sender.client = Client{TLSConfig: &tls.Config{RootCAs: srv.pool}}

	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok"); err != nil {
		t.Fatalf("SendPasswordResetEmail(TLS) err = %v", err)
	}
	if len(srv.collectedMails()) != 1 {
		t.Fatalf("delivered mails = %d, want 1", len(srv.collectedMails()))
	}
}

func TestSendPasswordResetEmailUnconfigured(t *testing.T) {
	// SEND_RESET_UNCONFIGURED: no settings row → the sender refuses (the User
	// core keeps the must-change-password fallback because Configured()=false).
	sender := newSender(nil)
	err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok")
	if err == nil {
		t.Fatal("SendPasswordResetEmail(nil settings) err = nil, want error")
	}
}

func TestSendPasswordResetEmailUnreachable(t *testing.T) {
	// TEST_FAIL / SEND_RESET with an unreachable server → error surfaced (the
	// User core logs it and still returns the uniform confirmation).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // port is now closed → connection refused

	_, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: port, Security: admcore.SmtpSecurityNone,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "enc:pw",
	}
	sender := newSender(settings)
	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok"); err == nil {
		t.Fatal("SendPasswordResetEmail(unreachable) err = nil, want error")
	}
}

func TestSendPasswordResetEmailDecryptFail(t *testing.T) {
	// DECRYPT_FAIL: stored ciphertext unreadable → the send refuses with the
	// decrypt error (never sends with a broken password).
	settings := &admcore.SmtpSettings{
		Host: "127.0.0.1", Port: 25, Security: admcore.SmtpSecurityNone,
		SenderAddress: "noreply@example.com", PasswordEncrypted: "kaputt",
	}
	sender := NewResetEmailSender(&fakeSettingsPort{settings: settings}, &fakeCipher{decryptErr: errors.New("bad key")}, nil)
	if err := sender.SendPasswordResetEmail(context.Background(), "user@example.com", "http://x/reset/tok"); err == nil {
		t.Fatal("SendPasswordResetEmail(undecryptable) err = nil, want error")
	}
}