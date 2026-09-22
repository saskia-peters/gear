package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/admin/core"
)

func TestStoreLocalWritesFile(t *testing.T) {
	// LOCAL_WRITE: the dump lands as a real file (creating the parent dir) and
	// the content round-trips exactly.
	dir := filepath.Join(t.TempDir(), "nested", "path")
	path := filepath.Join(dir, "gear-20260921123000.dump")
	want := []byte("GEAR custom-format dump bytes")

	if err := StoreLocal(context.Background(), path, bytes.NewReader(want)); err != nil {
		t.Fatalf("StoreLocal err = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile err = %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("file content = %q, want %q", got, want)
	}
}

func TestStoreLocalOverwritesAtomic(t *testing.T) {
	// The write is atomic (temp + rename): a pre-existing artifact is replaced
	// whole — no partial file can be observed.
	dir := t.TempDir()
	path := filepath.Join(dir, "gear-20260921123000.dump")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StoreLocal(context.Background(), path, bytes.NewReader([]byte("fresh"))); err != nil {
		t.Fatalf("StoreLocal err = %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "fresh" {
		t.Errorf("file content = %q, want fresh (atomic replace)", got)
	}
}

func TestStoreLocalContextCanceled(t *testing.T) {
	// A canceled context aborts before any file IO.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := StoreLocal(ctx, filepath.Join(t.TempDir(), "x.dump"), bytes.NewReader([]byte("data"))); err == nil {
		t.Fatal("StoreLocal(canceled ctx) err = nil, want context error")
	}
}

func TestStoreLocalNoPath(t *testing.T) {
	if err := StoreLocal(context.Background(), "", bytes.NewReader([]byte("data"))); err == nil {
		t.Fatal("StoreLocal(empty path) err = nil, want error")
	}
}

func TestStoreS3PutOK(t *testing.T) {
	// S3_PUT: the store signs a real SigV4 PUT (independently verified by the
	// fake server, which answers 2xx ONLY for a valid signature) to the dated
	// object key and returns nil.
	endpoint, reqs := startS3TestServer(t, http.StatusOK)
	params := core.BackupTestParams{
		Mechanism: core.BackupMechanismS3, Endpoint: endpoint, BucketOrPath: "bucket",
		Username: "svc", Password: "geheim",
	}
	key := "gear-20260921123000.dump"
	body := []byte("dump")
	hash := sha256.Sum256(body)

	if err := StoreS3(context.Background(), params, key, bytes.NewReader(body), hash[:]); err != nil {
		t.Fatalf("StoreS3 err = %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(*reqs))
	}
	put := (*reqs)[0]
	if put.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", put.method)
	}
	if put.path != "/bucket/gear-20260921123000.dump" {
		t.Errorf("PUT path = %q, want the dated object key under the bucket", put.path)
	}
	if !put.verified {
		t.Errorf("PUT signature was NOT accepted by the independent validator: %+v", put)
	}
}

func TestStoreS3PutNon2xxFails(t *testing.T) {
	// A non-2xx PUT answer is a failure (never silent).
	endpoint, _ := startS3TestServer(t, http.StatusForbidden)
	params := core.BackupTestParams{
		Mechanism: core.BackupMechanismS3, Endpoint: endpoint, BucketOrPath: "bucket",
		Username: "svc", Password: "geheim",
	}
	body := []byte("dump")
	hash := sha256.Sum256(body)
	err := StoreS3(context.Background(), params, "gear-20260921123000.dump", bytes.NewReader(body), hash[:])
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("StoreS3 err = %v, want an HTTP 403 error", err)
	}
}

func TestStoreS3URLEscapesBucketAndKey(t *testing.T) {
	// PATCH 6: a bucket/key with spaces or reserved characters must be
	// PathEscaped in the object URL — the wire path carries %20, not a space.
	var mu sync.Mutex
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.EscapedPath()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	params := core.BackupTestParams{
		Mechanism: core.BackupMechanismS3, Endpoint: srv.URL, BucketOrPath: "my bucket",
		Username: "svc", Password: "geheim",
	}
	body := []byte("dump")
	hash := sha256.Sum256(body)
	if err := StoreS3(context.Background(), params, "gear 2026.dump", bytes.NewReader(body), hash[:]); err != nil {
		t.Fatalf("StoreS3 err = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(gotPath, "/my%20bucket/gear%202026.dump") {
		t.Errorf("escaped PUT path = %q, want the %%20-escaped bucket + key", gotPath)
	}
}

func TestStoreS3HonorsConfiguredTimeout(t *testing.T) {
	// PATCH 5: the configured protocol timeout bounds the upload — a server
	// that outlives it fails the PUT (the tester's 10s const would not).
	ln := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ln.Close)

	params := core.BackupTestParams{
		Mechanism: core.BackupMechanismS3, Endpoint: ln.URL, BucketOrPath: "bucket",
		Username: "svc", Password: "geheim", Timeout: 50 * time.Millisecond,
	}
	body := []byte("dump")
	hash := sha256.Sum256(body)
	start := time.Now()
	err := StoreS3(context.Background(), params, "gear-20260921123000.dump", bytes.NewReader(body), hash[:])
	if err == nil {
		t.Fatal("StoreS3 err = nil, want a timeout error for a server that outlives the configured timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("StoreS3 took %v, want bounded by the configured timeout", elapsed)
	}
}

func TestStoreS3Guards(t *testing.T) {
	// The store guards itself before any network attempt: missing credential,
	// missing endpoint, missing bucket, missing key.
	ctx := context.Background()
	base := core.BackupTestParams{
		Mechanism: core.BackupMechanismS3, Endpoint: "http://127.0.0.1:1", BucketOrPath: "bucket",
		Username: "svc", Password: "geheim",
	}
	body := []byte("d")
	hash := sha256.Sum256(body)
	reader := func() io.Reader { return bytes.NewReader(body) }
	if err := StoreS3(ctx, core.BackupTestParams{Mechanism: core.BackupMechanismS3, Endpoint: base.Endpoint, BucketOrPath: base.BucketOrPath, Username: "svc", Password: ""}, "k", reader(), hash[:]); err == nil || !strings.Contains(err.Error(), "credential") {
		t.Errorf("no credential: err = %v, want a credential error", err)
	}
	noEndpoint := base
	noEndpoint.Endpoint = ""
	if err := StoreS3(ctx, noEndpoint, "k", reader(), hash[:]); err == nil {
		t.Error("no endpoint: err = nil, want error")
	}
	noBucket := base
	noBucket.BucketOrPath = "  "
	if err := StoreS3(ctx, noBucket, "k", reader(), hash[:]); err == nil {
		t.Error("no bucket: err = nil, want error")
	}
	if err := StoreS3(ctx, base, "", reader(), hash[:]); err == nil {
		t.Error("no key: err = nil, want error")
	}
}

func TestSHA256Streams(t *testing.T) {
	// The helper hashes a streamed body — the same bytes the signer sees.
	body := []byte("GEAR custom-format dump")
	got, err := SHA256(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("SHA256 err = %v", err)
	}
	want := sha256.Sum256(body)
	if !bytes.Equal(got, want[:]) {
		t.Errorf("SHA256 = %x, want %x", got, want[:])
	}
}
