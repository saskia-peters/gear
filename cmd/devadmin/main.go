//go:build dev

// Command devadmin is a local development utility to set a user's password
// hash directly in the database (e.g. to unlock the seeded admin accounts for
// local testing). It is NOT a production tool — real admin credentials are
// generated and distributed out-of-band (AD-13 / FR-27), never via this path.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/saskia-peters/gear/internal/platform/config"
	"github.com/saskia-peters/gear/internal/platform/crypto"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "devadmin:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load(os.Getenv)

	// Optional CLI args: `devadmin <email> <password>` sets the password
	// non-interactively (one-line command); without them, prompt for both.
	email := ""
	password := ""
	if len(os.Args) > 1 {
		email = strings.TrimSpace(os.Args[1])
	}
	if len(os.Args) > 2 {
		password = os.Args[2]
	}

	if email == "" {
		email = strings.TrimSpace(prompt("E-Mail des Kontos", ""))
	}
	if email == "" {
		return fmt.Errorf("email is required")
	}

	if password == "" {
		var err error
		password, err = readPassword("Neues Passwort (mindestens 10 Zeichen)")
		if err != nil {
			return err
		}
	}
	if len([]rune(password)) < 10 {
		return fmt.Errorf("password must be at least 10 characters")
	}

	hash, err := crypto.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	var updated bool
	err = pool.QueryRow(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE email = $2 RETURNING true`,
		hash, email,
	).Scan(&updated)
	if err != nil {
		return fmt.Errorf("update password for %q: %w", email, err)
	}

	fmt.Printf("Passwort für %s aktualisiert.\n", email)
	return nil
}

func prompt(label, _ string) string {
	fmt.Printf("%s: ", label)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func readPassword(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
