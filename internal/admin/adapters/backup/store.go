package backup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// StoreLocal writes a backup dump to the given path (Story 7.7, NFR-R3),
// streaming from r and creating the parent directory if needed. The write is
// atomic AND crash-durable: the temp file is fsynced before the rename into
// place, so neither a partial write nor a power loss leaves a truncated
// artifact at the final name. The context is honored.
func StoreLocal(ctx context.Context, path string, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("backup store: local: no path configured")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("backup store: local: cannot create directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".gear-backup-*")
	if err != nil {
		return fmt.Errorf("backup store: local: cannot create the dump file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("backup store: local: cannot write the dump file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("backup store: local: cannot sync the dump file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("backup store: local: cannot close the dump file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("backup store: local: cannot finalize the dump file: %w", err)
	}
	return nil
}

// StoreS3 PUTs a backup dump to an S3-compatible endpoint as a dated object
// (Story 7.7, NFR-R3). It reuses the tester's hand-rolled AWS SigV4 signer
// (stdlib only, no SDK). params carries the LIVE destination (mechanism s3,
// endpoint, bucket_or_path, the in-memory decrypted credential — never logged,
// NFR-S4 — and the optional protocol Timeout resolved from the
// backup_protocol_timeout app setting); key is the dated object key (e.g.
// gear-20260921.dump). body is streamed into the request (no full-buffer copy);
// bodyHash is the precomputed SHA-256 of body (the SigV4 payload hash — the
// store computes it once per run, never re-buffers). A network error or a
// non-2xx answer is returned as a failure.
func StoreS3(ctx context.Context, params core.BackupTestParams, key string, body io.Reader, bodyHash []byte) error {
	if err := requireCredential("s3", params); err != nil {
		return err
	}
	if params.Endpoint == "" {
		return fmt.Errorf("backup store: s3: no endpoint configured")
	}
	if key == "" {
		return fmt.Errorf("backup store: s3: no object key configured")
	}
	bucket := strings.Trim(params.BucketOrPath, "/")
	if bucket == "" {
		return fmt.Errorf("backup store: s3: no bucket configured")
	}
	// PathEscape the bucket + key so spaces/reserved characters never produce a
	// malformed object URL (a bucket cannot contain "/", but keys can).
	objectURL := strings.TrimRight(params.Endpoint, "/") + "/" + url.PathEscape(bucket) + "/" + url.PathEscape(key)
	amzDate := time.Now().UTC().Format("20060102T150405Z")

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL, body)
	if err != nil {
		return fmt.Errorf("backup store: s3: building the PUT request failed: %w", err)
	}
	signRequest(req, params.Username, params.Password, sigV4Region, amzDate, bodyHash)

	timeout := params.Timeout
	if timeout <= 0 {
		timeout = protocolTimeout
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("backup store: s3: PUT failed: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("backup store: s3: PUT answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// SHA256 computes the SHA-256 of an io.Reader, streaming (bounded memory).
func SHA256(r io.Reader) ([]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
