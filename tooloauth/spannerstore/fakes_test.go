package spannerstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"cloud.google.com/go/spanner"
	"cloud.google.com/go/spanner/spannertest"
	"cloud.google.com/go/spanner/spansql"
)

// startSpanner starts the Spanner client library's in-memory fake with DDL
// applied and returns a client for it. SPANNER_EMULATOR_HOST makes the
// client dial it in plain text without credentials, and keeps it from
// probing GCP's metadata server.
func startSpanner(t *testing.T) *spanner.Client {
	t.Helper()
	srv, err := spannertest.NewServer("localhost:0")
	if err != nil {
		t.Fatalf("spannertest.NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	ddl, err := spansql.ParseDDL("", DDL)
	if err != nil {
		t.Fatalf("ParseDDL: %v", err)
	}
	if err := srv.UpdateDDL(ddl); err != nil {
		t.Fatalf("UpdateDDL: %v", err)
	}
	t.Setenv("SPANNER_EMULATOR_HOST", srv.Addr)
	client, err := spanner.NewClient(context.Background(), "projects/p/instances/i/databases/d")
	if err != nil {
		t.Fatalf("spanner.NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

// fakeKMS stands in for Cloud KMS: Wrap keeps the plaintext and its AAD
// under a random handle and returns the handle; Unwrap returns the plaintext
// only for a known handle with the same AAD.
type fakeKMS struct {
	mu      sync.Mutex
	keys    map[string]wrapped
	unwraps int
	// ignoreAAD makes Unwrap skip the AAD check, so a test can show the
	// token's own seal binds it to its row.
	ignoreAAD bool
}

type wrapped struct {
	plaintext, aad []byte
}

func newFakeKMS() *fakeKMS {
	return &fakeKMS{keys: map[string]wrapped{}}
}

func (f *fakeKMS) Wrap(_ context.Context, plaintext, aad []byte) ([]byte, error) {
	handle := make([]byte, 16)
	if _, err := rand.Read(handle); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys[hex.EncodeToString(handle)] = wrapped{plaintext: bytes.Clone(plaintext), aad: bytes.Clone(aad)}
	return handle, nil
}

func (f *fakeKMS) Unwrap(_ context.Context, ciphertext, aad []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unwraps++
	w, ok := f.keys[hex.EncodeToString(ciphertext)]
	if !ok || (!f.ignoreAAD && !bytes.Equal(w.aad, aad)) {
		return nil, errors.New("fake kms: decryption failed")
	}
	return bytes.Clone(w.plaintext), nil
}

func (f *fakeKMS) unwrapCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unwraps
}
