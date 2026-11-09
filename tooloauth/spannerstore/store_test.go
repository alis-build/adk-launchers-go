package spannerstore

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/spanner"
	"cloud.google.com/go/spanner/spannertest"
	"go.alis.build/adk/launchers/tooloauth"
	"google.golang.org/adk/v2/auth"
)

var key7 = auth.CredentialKey{AppName: "app-1", UserID: "user-7", Key: "slot-a"}

func TestStore_roundTripsToken(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())
	ctx := context.Background()
	expiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-plain-42", RefreshToken: "rt-plain-42", Expiry: expiry}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	cred, ok, err := s.Get(ctx, key7)

	if err != nil || !ok {
		t.Fatalf("Get() = %v, %v, %v; want a hit", cred, ok, err)
	}
	tok, isToken := cred.(*tooloauth.Token)
	if !isToken {
		t.Fatalf("Get() credential is %T, want *tooloauth.Token", cred)
	}
	if tok.AccessToken != "at-plain-42" || tok.RefreshToken != "rt-plain-42" || !tok.Expiry.Equal(expiry) {
		t.Errorf("Get() token = access %q, refresh %q, expiry %v; want at-plain-42, rt-plain-42, %v", tok.AccessToken, tok.RefreshToken, tok.Expiry, expiry)
	}
}

func TestStore_storesOnlyCiphertext(t *testing.T) {
	client := startSpanner(t)
	s := newStore(client, newFakeKMS())
	ctx := context.Background()
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-plain-42", RefreshToken: "rt-plain-42"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	iter := client.Single().Read(ctx, Table, spanner.AllKeys(), []string{"RowKey", "WrappedKey", "Nonce", "Ciphertext"})
	defer iter.Stop()
	rows := 0
	err := iter.Do(func(row *spanner.Row) error {
		rows++
		var rowKey, wrappedKey, nonce, ciphertext []byte
		if err := row.Columns(&rowKey, &wrappedKey, &nonce, &ciphertext); err != nil {
			return err
		}
		for _, col := range [][]byte{rowKey, wrappedKey, nonce, ciphertext} {
			for _, secret := range []string{"at-plain-42", "rt-plain-42", "user-7", "app-1", "slot-a"} {
				if bytes.Contains(col, []byte(secret)) {
					t.Errorf("a stored column contains %q", secret)
				}
			}
		}
		if len(wrappedKey) == 32 {
			t.Errorf("WrappedKey is 32 bytes, as a bare data key would be")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1", rows)
	}
}

func TestStore_rejectsOtherCredentials(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())
	ctx := context.Background()

	err := s.Set(ctx, key7, auth.BearerCredential{Token: "tok-x"}, time.Now().Add(time.Hour))

	if err == nil || err.Error() != "spannerstore: unsupported credential type" {
		t.Errorf("Set() error = %v, want %q", err, "spannerstore: unsupported credential type")
	}
	if err != nil && strings.Contains(err.Error(), "tok-x") {
		t.Errorf("Set() error %q carries the token", err)
	}
	if _, ok, _ := s.Get(ctx, key7); ok {
		t.Error("Get() hit after a rejected Set")
	}
}

func TestStore_rejectsZeroExpiry(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())

	err := s.Set(context.Background(), key7, &tooloauth.Token{AccessToken: "at-1"}, time.Time{})

	if err == nil || err.Error() != "spannerstore: Set requires an expiry" {
		t.Errorf("Set() error = %v, want %q", err, "spannerstore: Set requires an expiry")
	}
}

func TestStore_rejectsNilCredential(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())

	err := s.Set(context.Background(), key7, nil, time.Now().Add(time.Hour))

	if err == nil || err.Error() != "spannerstore: Set requires a credential" {
		t.Errorf("Set() error = %v, want %q", err, "spannerstore: Set requires a credential")
	}
}

func TestStore_deleteRemovesAndToleratesAbsent(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())
	ctx := context.Background()
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-1"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := s.Delete(ctx, key7); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok, err := s.Get(ctx, key7); ok || err != nil {
		t.Errorf("Get() after Delete = %v, %v; want a miss", ok, err)
	}
	if err := s.Delete(ctx, auth.CredentialKey{AppName: "app-1", UserID: "nobody", Key: "slot-a"}); err != nil {
		t.Errorf("Delete() of an absent key error = %v, want nil", err)
	}
}

func TestNew_requiresClientsAndKey(t *testing.T) {
	_, err := New(nil, nil, "")
	if err == nil || err.Error() != "spannerstore: spanner client, KMS client and key name are required" {
		t.Errorf("New() error = %v, want the required-arguments error", err)
	}
}

// A missing table is a backend failure, not a missing token: as a miss it
// would send every user through a sign-in whose token could not be stored.
func TestStore_missingTableIsAnError(t *testing.T) {
	srv, err := spannertest.NewServer("localhost:0")
	if err != nil {
		t.Fatalf("spannertest.NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	t.Setenv("SPANNER_EMULATOR_HOST", srv.Addr)
	client, err := spanner.NewClient(context.Background(), "projects/p/instances/i/databases/d")
	if err != nil {
		t.Fatalf("spanner.NewClient: %v", err)
	}
	t.Cleanup(client.Close)

	cred, ok, err := newStore(client, newFakeKMS()).Get(context.Background(), key7)

	if err == nil || ok || cred != nil {
		t.Errorf("Get() = %v, %v, %v; want an error", cred, ok, err)
	}
}

func TestNew_rejectsKeyVersionName(t *testing.T) {
	kmsClient := &kms.KeyManagementClient{}
	_, err := New(&spanner.Client{}, kmsClient, "projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1")
	if err == nil || err.Error() != "spannerstore: key name must name a CryptoKey, not a CryptoKeyVersion" {
		t.Errorf("New() error = %v, want the key-version error", err)
	}
}
