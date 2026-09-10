// Package smtp hosts the outbound email-delivery adapter of the Admin hexagon
// (Story 3.1, FR-28/AD-14): a low-level stdlib SMTP client (net/smtp +
// crypto/tls — no third-party dependency) and the real ResetEmailSender that
// replaces the Epic-1 resetEmailStub. The sender reads the live Admin settings
// row at send time via the read-only settings port and decrypts the stored
// password in memory (NFR-S4); no caching (FR-28 immediacy).
package smtp

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
	adminports "github.com/saskia-peters/gear/internal/admin/ports"
	userports "github.com/saskia-peters/gear/internal/user/ports"
)

const (
	// dialTimeout bounds a single SMTP connection attempt (avoids hanging
	// dials).
	dialTimeout = 10 * time.Second
	// protocolTimeout bounds the whole SMTP conversation AFTER the connection
	// is established: a server that stalls after the greeting (MAIL/RCPT/DATA/
	// QUIT) can no longer block a send indefinitely. The deadline is extended
	// before each operation so a slow-but-responsive server is not cut off.
	protocolTimeout = 30 * time.Second
	// settingsReadTimeout bounds the Configured() settings read so a slow DB
	// cannot stall the FR-26 must-change-password fallback decision.
	settingsReadTimeout = 5 * time.Second
)

// Client is the low-level SMTP mailer implementing the Admin core's SmtpMailer
// port. It is stateless and safe for concurrent use. TLSConfig is a test/
// diagnostic seam: production wires nil and the ServerName is derived from the
// settings host; tests inject a config with their own root CA.
type Client struct {
	TLSConfig *tls.Config
}

// SendEmail delivers one message through the configured server, supporting
// none (plain TCP), STARTTLS (explicit upgrade via Client.StartTLS) and TLS
// (implicit TLS via a context-aware tls.Dialer + smtp.NewClient — stdlib
// net/smtp has no implicit TLS). The plaintext password exists only in
// params, in memory. After the connection is established a protocol deadline
// is set on the underlying conn and extended before each operation, so a
// stalled server cannot hang the send.
func (c Client) SendEmail(ctx context.Context, params admcore.SmtpSendParams) error {
	addr := net.JoinHostPort(params.Host, strconv.Itoa(params.Port))
	conn, err := c.dial(ctx, addr, params)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, params.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: SMTP greeting from %s failed: %w", addr, err)
	}
	defer func() { _ = client.Close() }()
	// Protocol deadline covering the whole conversation (finding: a server that
	// stalls after the greeting must not block the send indefinitely).
	extendDeadline(conn)

	if params.Security == admcore.SmtpSecurityStartTLS {
		extendDeadline(conn)
		if err := client.StartTLS(c.tlsConfigFor(params.Host)); err != nil {
			return fmt.Errorf("smtp: STARTTLS upgrade on %s failed: %w", addr, err)
		}
	}

	var auth smtp.Auth
	if params.Username != "" {
		// Auth is attempted only when a username is configured; without it the
		// server's AUTH is not exercised (some relays accept anonymous send).
		auth = smtp.PlainAuth("", params.Username, params.Password, params.Host)
	}
	if auth != nil {
		extendDeadline(conn)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: AUTH on %s failed: %w", addr, err)
		}
	}

	extendDeadline(conn)
	if err := client.Mail(params.From); err != nil {
		return fmt.Errorf("smtp: MAIL FROM on %s failed: %w", addr, err)
	}
	extendDeadline(conn)
	if err := client.Rcpt(params.To); err != nil {
		return fmt.Errorf("smtp: RCPT TO on %s failed: %w", addr, err)
	}
	extendDeadline(conn)
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA on %s failed: %w", addr, err)
	}
	if _, err := w.Write([]byte(buildMessage(params.SenderName, params.From, params.To, params.Subject, params.Body))); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp: writing message body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: finishing message body failed: %w", err)
	}
	extendDeadline(conn)
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp: QUIT on %s failed: %w", addr, err)
	}
	return nil
}

// extendDeadline pushes the protocol deadline forward to now+protocolTimeout
// on the underlying conn, bounding the current operation.
func extendDeadline(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(protocolTimeout))
}

// dial opens the transport connection: plain TCP (none/STARTTLS) or implicit
// TLS (security=tls). Both branches are context-aware (DialContext), so a
// canceled context aborts the dial — including the TLS handshake.
func (c Client) dial(ctx context.Context, addr string, params admcore.SmtpSendParams) (net.Conn, error) {
	netDialer := &net.Dialer{Timeout: dialTimeout}
	if params.Security == admcore.SmtpSecurityTLS {
		td := &tls.Dialer{NetDialer: netDialer, Config: c.tlsConfigFor(params.Host)}
		conn, err := td.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("smtp: implicit TLS connection to %s failed: %w", addr, err)
		}
		return conn, nil
	}
	conn, err := netDialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("smtp: connection to %s failed: %w", addr, err)
	}
	return conn, nil
}

// tlsConfigFor returns the TLS config used for implicit TLS and STARTTLS. The
// ServerName always reflects the settings host so certificate verification
// works; an injected config (tests) supplies its own root CAs.
func (c Client) tlsConfigFor(host string) *tls.Config {
	if c.TLSConfig != nil {
		cfg := c.TLSConfig.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName = host
		}
		return cfg
	}
	return &tls.Config{ServerName: host}
}

// buildMessage assembles an RFC 5322 message with CRLF line endings: a
// display-name-form From header (RFC 2047-encoded when non-ASCII), To,
// Subject (RFC 2047-encoded UTF-8 so German umlauts stay 7-bit clean), the
// mandatory Date and a unique Message-ID (some relays reject messages without
// them).
func buildMessage(senderName, fromAddr, to, subject, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", formatFrom(senderName, fromAddr))
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: %s\r\n", newMessageID(fromAddr))
	fmt.Fprintf(&b, "Subject: %s\r\n", encodeHeaderWord(subject))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return b.String()
}

// formatFrom renders the RFC 5322 From header value from the display name and
// the bare address. An empty display name emits just the address; a non-ASCII
// name is emitted as an RFC 2047 encoded-word (encoded-words are not allowed
// inside a quoted-string, so it stays bare); an ASCII name is quoted
// (backslash-escaping `\` and `"`) so spaces/specials are safe.
func formatFrom(senderName, fromAddr string) string {
	if senderName == "" {
		return fromAddr
	}
	if !isASCII(senderName) {
		return fmt.Sprintf("%s <%s>", encodeHeaderWord(senderName), fromAddr)
	}
	name := strings.ReplaceAll(senderName, `\`, `\\`)
	name = strings.ReplaceAll(name, `"`, `\"`)
	return fmt.Sprintf(`"%s" <%s>`, name, fromAddr)
}

// newMessageID returns a unique RFC 5322 Message-ID (angle-bracketed) using
// the CSPRNG; the domain is derived from the From address (fallback
// gear.local).
func newMessageID(fromAddr string) string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// CSPRNG failure is practically unreachable; fall back to a time-based
		// id so delivery never hard-fails on randomness.
		return fmt.Sprintf("<%d.%d@%s>", time.Now().UnixNano(), time.Now().UnixMilli(), messageIDDomain(fromAddr))
	}
	return fmt.Sprintf("<%x@%s>", b, messageIDDomain(fromAddr))
}

// messageIDDomain extracts the addr-spec domain for the Message-ID, falling
// back to gear.local when the address is empty/malformed.
func messageIDDomain(fromAddr string) string {
	if i := strings.LastIndexByte(fromAddr, '@'); i >= 0 && i+1 < len(fromAddr) {
		domain := fromAddr[i+1:]
		if domain != "" && !strings.ContainsAny(domain, " \t\r\n<>") {
			return domain
		}
	}
	return "gear.local"
}

// encodeHeaderWord RFC 2047 Q-encodes a header word as UTF-8 when it contains
// non-ASCII bytes; pure-ASCII input passes through unchanged.
func encodeHeaderWord(s string) string {
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	b.WriteString("=?UTF-8?Q?")
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == ' ':
			b.WriteByte('_')
		case ch >= 33 && ch <= 126 && ch != '=' && ch != '?' && ch != '_':
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "=%02X", ch)
		}
	}
	b.WriteString("?=")
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// ResetEmailSender is the real FR-26 delivery path (AD-14): it implements the
// UNCHANGED User-module ResetEmailSender port, reads the live Admin SMTP
// settings row at send time and decrypts the stored password in memory. It
// replaces the Epic-1 resetEmailStub in the composition root.
type ResetEmailSender struct {
	settings adminports.SmtpSettingsPort
	cipher   admcore.SecretCipher
	log      *slog.Logger
	client   Client
}

// NewResetEmailSender constructs the real sender. settings and cipher are
// required; log may be nil (falls back to slog.Default()).
func NewResetEmailSender(settings adminports.SmtpSettingsPort, cipher admcore.SecretCipher, log *slog.Logger) *ResetEmailSender {
	return &ResetEmailSender{settings: settings, cipher: cipher, log: log, client: Client{}}
}

// SendPasswordResetEmail delivers a single transactional reset email carrying
// the full clickable reset link (FR-26). It returns an error when SMTP is not
// configured or the send fails — the User core logs it (NFR-O1) while still
// returning the uniform anti-enumeration confirmation. The stored ciphertext
// is decrypted only in memory for this send (NFR-S4); an anonymous no-auth
// relay (none/starttls without a username) sends without a password.
func (s *ResetEmailSender) SendPasswordResetEmail(ctx context.Context, email, resetLink string) error {
	settings, err := s.settings.CurrentSmtpSettings(ctx)
	if err != nil {
		return fmt.Errorf("smtp: failed to read smtp settings: %w", err)
	}
	if settings == nil || !settings.DeliveryUsable() {
		return fmt.Errorf("smtp: SMTP delivery is not configured")
	}
	password := ""
	if settings.PasswordConfigured() {
		password, err = s.cipher.Decrypt(settings.PasswordEncrypted)
		if err != nil {
			return fmt.Errorf("smtp: failed to decrypt the stored smtp password: %w", err)
		}
	}

	subject := "GEAR: Passwort zurücksetzen"
	body := fmt.Sprintf(
		"Hallo,\n\nfür dein G.E.A.R.-Konto wurde ein Passwort-Zurücksetzen angefordert. Öffne den folgenden Link, um ein neues Passwort zu setzen:\n\n%s\n\nDer Link ist 30 Minuten gültig und kann nur einmal verwendet werden.\n\nMit freundlichen Grüßen\nG.E.A.R.",
		resetLink,
	)
	if err := s.client.SendEmail(ctx, admcore.SmtpSendParams{
		Host:       settings.Host,
		Port:       settings.Port,
		Security:   settings.Security,
		Username:   settings.Username,
		Password:   password,
		From:       settings.SenderAddress,
		SenderName: settings.SenderName,
		To:         email,
		Subject:    subject,
		Body:       body,
	}); err != nil {
		if s.log != nil {
			s.log.Warn("password reset email delivery failed", "to", email, "host", settings.Host, "error", err)
		}
		return err
	}
	if s.log != nil {
		s.log.Info("password reset email delivered", "to", email, "host", settings.Host)
	}
	return nil
}

// Configured reports whether a real delivery path is available (FR-26): a
// DeliveryUsable settings row exists AND any required password decrypts. An
// anonymous no-auth relay (none/starttls without a username) is configured
// without a password. When false, the User module keeps the
// must-change-password fallback — no behavioral regression when unconfigured.
// Reads the live row (no caching) under a short timeout so a slow DB cannot
// stall the fallback decision.
func (s *ResetEmailSender) Configured() bool {
	ctx, cancel := context.WithTimeout(context.Background(), settingsReadTimeout)
	defer cancel()

	settings, err := s.settings.CurrentSmtpSettings(ctx)
	if err != nil || settings == nil || !settings.DeliveryUsable() {
		return false
	}
	if !settings.PasswordConfigured() {
		return true // anonymous no-auth relay — nothing to decrypt
	}
	_, err = s.cipher.Decrypt(settings.PasswordEncrypted)
	return err == nil
}

// Compile-time checks: the adapter implements the ports it claims, and the
// core Service satisfies the read-only settings port the sender consumes.
var (
	_ admcore.SmtpMailer          = Client{}
	_ userports.ResetEmailSender  = (*ResetEmailSender)(nil)
	_ adminports.SmtpSettingsPort = (*admcore.Service)(nil)
)