package spannerstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"go.alis.build/adk/launchers/tooloauth"
	"google.golang.org/adk/v2/auth"
)

// AC6: keys that a separator join would confuse get their own rows.
func TestStore_separatorLookalikesGetOwnRows(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())
	ctx := context.Background()
	bob := auth.CredentialKey{AppName: "acme", UserID: "bob|X", Key: "k"}
	acmeBob := auth.CredentialKey{AppName: "acme|bob", UserID: "X", Key: "k"}
	for key, access := range map[auth.CredentialKey]string{bob: "at-bob", acmeBob: "at-acmebob"} {
		if err := s.Set(ctx, key, &tooloauth.Token{AccessToken: access}, time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("Set(%+v) error = %v", key, err)
		}
	}

	for key, want := range map[auth.CredentialKey]string{bob: "at-bob", acmeBob: "at-acmebob"} {
		cred, ok, err := s.Get(ctx, key)
		if err != nil || !ok {
			t.Fatalf("Get(%+v) = %v, %v", key, ok, err)
		}
		if got := cred.(*tooloauth.Token).AccessToken; got != want {
			t.Errorf("Get(%+v) access token = %q, want %q", key, got, want)
		}
	}
}

// A ciphertext copied into another user's row does not decrypt there: KMS
// refuses the wrapped data key, bound to its row, and the copy is dropped as
// unreadable, never served as the first user's token.
func TestStore_copiedCiphertextIsRefused(t *testing.T) {
	client := startSpanner(t)
	kms := newFakeKMS()
	s := newStore(client, kms)
	ctx := context.Background()
	user8 := auth.CredentialKey{AppName: "app-1", UserID: "user-8", Key: "slot-a"}
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-user-7"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	copyRow(t, client, key7, user8)

	cred, ok, err := s.Get(ctx, user8)

	if cred != nil || ok || err != nil {
		t.Errorf("Get(user-8) = %v, %v, %v; want a miss", cred, ok, err)
	}
	if kms.aadRejections != 1 {
		t.Errorf("KMS AAD rejections = %d, want 1: the wrapped key's row binding must refuse the copy", kms.aadRejections)
	}
	if rowExists(t, client, user8) {
		t.Error("the copied row is still stored, want it dropped")
	}
	if cred, ok, err := s.Get(ctx, key7); !ok || err != nil || cred.(*tooloauth.Token).AccessToken != "at-user-7" {
		t.Errorf("Get(user-7) = %v, %v, %v; want user-7's own token intact", cred, ok, err)
	}
}

// With a KMS that ignored the binding, the token's own seal still binds it
// to its row: each binding stops a copied row on its own.
func TestStore_copiedCiphertextIsRefusedBySeal(t *testing.T) {
	client := startSpanner(t)
	kms := newFakeKMS()
	kms.ignoreAAD = true
	s := newStore(client, kms)
	ctx := context.Background()
	user8 := auth.CredentialKey{AppName: "app-1", UserID: "user-8", Key: "slot-a"}
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-user-7"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	copyRow(t, client, key7, user8)

	cred, ok, err := s.Get(ctx, user8)

	if cred != nil || ok || err != nil {
		t.Errorf("Get(user-8) = %v, %v, %v; want the seal to refuse it as a miss", cred, ok, err)
	}
	if rowExists(t, client, user8) {
		t.Error("the copied row is still stored, want it dropped")
	}
}

// rowExists reports whether a row is stored under key.
func rowExists(t *testing.T, client *spanner.Client, key auth.CredentialKey) bool {
	t.Helper()
	_, err := client.Single().ReadRow(context.Background(), Table, spanner.Key{rowKey(key)}, []string{"RowKey"})
	if errors.Is(err, spanner.ErrRowNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("ReadRow: %v", err)
	}
	return true
}

// copyRow copies from's stored row, ciphertext and all, into to's row.
func copyRow(t *testing.T, client *spanner.Client, from, to auth.CredentialKey) {
	t.Helper()
	ctx := context.Background()
	row, err := client.Single().ReadRow(ctx, Table, spanner.Key{rowKey(from)}, columns)
	if err != nil {
		t.Fatalf("ReadRow: %v", err)
	}
	var rk, wrappedKey, nonce, ciphertext []byte
	var keyName string
	var expire time.Time
	if err := row.Columns(&rk, &wrappedKey, &nonce, &ciphertext, &keyName, &expire); err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if _, err := client.Apply(ctx, []*spanner.Mutation{spanner.InsertOrUpdate(Table, columns, []any{rowKey(to), wrappedKey, nonce, ciphertext, keyName, expire})}); err != nil {
		t.Fatalf("copy row: %v", err)
	}
}

func TestStore_sameUserDifferentSlotsAreSeparate(t *testing.T) {
	s := newStore(startSpanner(t), newFakeKMS())
	ctx := context.Background()
	slotB := auth.CredentialKey{AppName: "app-1", UserID: "user-7", Key: "slot-b"}
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-a"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set(slot-a) error = %v", err)
	}
	if err := s.Set(ctx, slotB, &tooloauth.Token{AccessToken: "at-b"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set(slot-b) error = %v", err)
	}

	for key, want := range map[auth.CredentialKey]string{key7: "at-a", slotB: "at-b"} {
		cred, ok, err := s.Get(ctx, key)
		if err != nil || !ok || cred.(*tooloauth.Token).AccessToken != want {
			t.Errorf("Get(%s) = %v, %v, %v; want %q", key.Key, cred, ok, err, want)
		}
	}
}

// A token stored while Get is dropping an unreadable row is kept: the drop
// deletes only the row it read.
func TestStore_dropKeepsATokenStoredMeanwhile(t *testing.T) {
	client := startSpanner(t)
	kms := newFakeKMS()
	kms.ignoreAAD = true
	s := newStore(client, kms)
	ctx := context.Background()
	user8 := auth.CredentialKey{AppName: "app-1", UserID: "user-8", Key: "slot-a"}
	if err := s.Set(ctx, key7, &tooloauth.Token{AccessToken: "at-user-7"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	copyRow(t, client, key7, user8)
	kms.onUnwrap = func() {
		kms.onUnwrap = nil
		if err := s.Set(ctx, user8, &tooloauth.Token{AccessToken: "at-user-8-fresh"}, time.Now().Add(time.Hour)); err != nil {
			t.Errorf("concurrent Set() error = %v", err)
		}
	}

	if cred, ok, err := s.Get(ctx, user8); cred != nil || ok || err != nil {
		t.Errorf("Get(user-8) = %v, %v, %v; want a miss for the copied row", cred, ok, err)
	}

	cred, ok, err := s.Get(ctx, user8)
	if err != nil || !ok || cred.(*tooloauth.Token).AccessToken != "at-user-8-fresh" {
		t.Errorf("Get(user-8) after the drop = %v, %v, %v; want the token stored meanwhile", cred, ok, err)
	}
}
