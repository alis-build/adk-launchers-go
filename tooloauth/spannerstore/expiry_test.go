package spannerstore

import (
	"context"
	"testing"
	"time"

	"go.alis.build/adk/launchers/tooloauth"
)

func TestStore_expiryWithinSkewIsAMiss(t *testing.T) {
	kms := newFakeKMS()
	s := newStore(startSpanner(t), kms)
	ctx := context.Background()
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-1"}, time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	cred, ok, err := s.Get(ctx, key7)

	if ok || cred != nil || err != nil {
		t.Errorf("Get() = %v, %v, %v; want a miss", cred, ok, err)
	}
	if got := kms.unwrapCount(); got != 0 {
		t.Errorf("KMS unwrapped %d times for an expired row, want 0", got)
	}
}

// AC5: the plugin renews a refreshable token by Setting it again with a
// later expiry.
func TestStore_laterSetExtendsExpiry(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())
	ctx := context.Background()
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-1"}, time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("first Set() error = %v", err)
	}
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-1"}, time.Now().Add(2*time.Hour)); err != nil {
		t.Fatalf("second Set() error = %v", err)
	}

	if _, ok, err := s.Get(ctx, key7); !ok || err != nil {
		t.Errorf("Get() = %v, %v; want a hit after the extension", ok, err)
	}
}

func TestStore_laterSetCanShortenExpiry(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())
	ctx := context.Background()
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-1"}, time.Now().Add(2*time.Hour)); err != nil {
		t.Fatalf("first Set() error = %v", err)
	}
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-1"}, time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("second Set() error = %v", err)
	}

	if _, ok, err := s.Get(ctx, key7); ok || err != nil {
		t.Errorf("Get() = %v, %v; want a miss after the shortening", ok, err)
	}
}
