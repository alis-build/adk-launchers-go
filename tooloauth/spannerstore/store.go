package spannerstore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/spanner"
	"go.alis.build/adk/launchers/tooloauth"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Table is the Spanner table the store reads and writes.
const Table = "ToolOAuthCredentials"

// DDL creates [Table]. RowKey is a SHA-256 digest of the credential key;
// WrappedKey is the KMS-encrypted data key and KeyName the KMS key that
// wrapped it; Nonce and Ciphertext are the AES-256-GCM seal of the token. Rows become eligible for deletion a day
// after ExpireTime and Spanner removes them in the background, typically
// within three days; Get never serves a row past its expiry.
const DDL = `CREATE TABLE ToolOAuthCredentials (
  RowKey BYTES(32) NOT NULL,
  WrappedKey BYTES(MAX) NOT NULL,
  Nonce BYTES(12) NOT NULL,
  Ciphertext BYTES(MAX) NOT NULL,
  KeyName STRING(MAX) NOT NULL,
  ExpireTime TIMESTAMP NOT NULL,
) PRIMARY KEY (RowKey),
  ROW DELETION POLICY (OLDER_THAN(ExpireTime, INTERVAL 1 DAY))`

// rowKeyVersion prefixes every row key digest, so a later change to its
// inputs can move to new rows without misreading old ones.
const rowKeyVersion = "tooloauth-spannerstore/v1"

var columns = []string{"RowKey", "WrappedKey", "Nonce", "Ciphertext", "KeyName", "ExpireTime"}

// Store is an auth.CredentialStore on one Spanner table, encrypting each
// token under a per-row data key wrapped by Cloud KMS. It is safe for
// concurrent use.
type Store struct {
	client *spanner.Client
	keys   keyWrapper
}

var _ auth.CredentialStore = (*Store)(nil)

// New returns a Store on client's database, wrapping data keys with the
// Cloud KMS key keyName (projects/.../locations/.../keyRings/.../cryptoKeys/...).
// keyName must name the key, not one of its versions: KMS encrypts with a
// version name but decrypts only with the key's, so every read would fail.
// Keep keyName stable and spelled the same way: each row records it, and a
// row written under another name is reported as an error until it expires.
// To move to another key, delete the table's rows; users sign in again.
// The database must hold [Table], created with [DDL].
func New(client *spanner.Client, kmsClient *kms.KeyManagementClient, keyName string) (*Store, error) {
	if client == nil || kmsClient == nil || keyName == "" {
		return nil, errors.New("spannerstore: spanner client, KMS client and key name are required")
	}
	if strings.Contains(keyName, "/cryptoKeyVersions/") {
		return nil, errors.New("spannerstore: key name must name a CryptoKey, not a CryptoKeyVersion")
	}
	return newStore(client, kmsWrapper{client: kmsClient, keyName: keyName}), nil
}

// newStore returns a Store wrapping data keys with keys.
func newStore(client *spanner.Client, keys keyWrapper) *Store {
	return &Store{client: client, keys: keys}
}

// Get implements auth.CredentialStore. A row within auth.ExpirySkew of its
// expiry is a miss. A row whose token can never be read (its seal or data
// fails, or KMS rejects its wrapped key as invalid although the row was
// written with the configured key, as for a ciphertext copied from another
// row) is deleted and reported as a miss, never as another user's token. A
// row written with a different KMS key is an error and is kept. A KMS
// failure that is not about the row itself (unavailable, permission denied,
// key disabled or destroyed) is an error. Errors never name the credential
// key or carry token text.
func (s *Store) Get(ctx context.Context, key auth.CredentialKey) (auth.Credential, bool, error) {
	rk := rowKey(key)
	row, err := s.client.Single().ReadRow(ctx, Table, spanner.Key{rk}, columns)
	// Only a missing row is a miss. A missing table or database is also
	// NotFound from Spanner, and must be an error: as a miss, every user
	// would be sent through a sign-in whose token could not be stored.
	if errors.Is(err, spanner.ErrRowNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("spannerstore: read credential: %w", err)
	}
	var stored struct {
		rowKey, wrappedKey, nonce, ciphertext []byte
		keyName                               string
		expireTime                            time.Time
	}
	if err := row.Columns(&stored.rowKey, &stored.wrappedKey, &stored.nonce, &stored.ciphertext, &stored.keyName, &stored.expireTime); err != nil {
		return nil, false, fmt.Errorf("spannerstore: read credential: %w", err)
	}
	// Expired, or within the skew producers decline to cache inside, as the
	// CredentialStore contract and ADK's in-memory store treat it. The row
	// deletion policy removes it later; Get neither decrypts nor deletes it.
	if !stored.expireTime.After(time.Now().Add(auth.ExpirySkew)) {
		return nil, false, nil
	}
	dataKey, err := s.keys.Unwrap(ctx, stored.wrappedKey, rk)
	if status.Code(err) == codes.InvalidArgument {
		// KMS answers a ciphertext that is not the given key's the same way
		// whether the row is bad or the store points at the wrong key. Only
		// a row written with the configured key is this row's fault; any
		// other is a misconfiguration, and deleting on it would sign every
		// user out.
		if stored.keyName != s.keys.KeyName() {
			return nil, false, errors.New("spannerstore: read credential: row was written with another KMS key than the configured one")
		}
		// Corrupted, or copied from another row.
		return s.dropUndecryptable(ctx, rk, stored.wrappedKey)
	}
	if err != nil {
		// An outage, a permission problem, or a disabled or destroyed key
		// version: not this row's fault, and deleting on it would sign every
		// user out over an operator's mistake.
		// The code only: a KMS message can name the key resource.
		return nil, false, fmt.Errorf("spannerstore: read credential: unwrap data key: KMS %s", status.Code(err))
	}
	aead, err := newAEAD(dataKey)
	if err != nil {
		return s.dropUndecryptable(ctx, rk, stored.wrappedKey)
	}
	plain, err := aead.Open(nil, stored.nonce, stored.ciphertext, rk)
	if err != nil {
		return s.dropUndecryptable(ctx, rk, stored.wrappedKey)
	}
	var tok tooloauth.Token
	if err := json.Unmarshal(plain, &tok); err != nil {
		return s.dropUndecryptable(ctx, rk, stored.wrappedKey)
	}
	return &tok, true, nil
}

// dropUndecryptable deletes the row under rk, whose token can never be read,
// and reports a miss, so the user is asked to sign in again rather than see
// the sign-in as unavailable until the row expires. It deletes only if the
// row still holds wrappedKey, the data key Get read: a token another request
// stored meanwhile is kept. The deletion is best effort: if it fails, a
// later Set overwrites the row anyway.
func (s *Store) dropUndecryptable(ctx context.Context, rk, wrappedKey []byte) (auth.Credential, bool, error) {
	_, _ = s.client.ReadWriteTransaction(ctx, func(ctx context.Context, tx *spanner.ReadWriteTransaction) error {
		row, err := tx.ReadRow(ctx, Table, spanner.Key{rk}, []string{"WrappedKey"})
		if err != nil {
			return err
		}
		var current []byte
		if err := row.Columns(&current); err != nil {
			return err
		}
		if !bytes.Equal(current, wrappedKey) {
			return nil
		}
		return tx.BufferWrite([]*spanner.Mutation{spanner.Delete(Table, spanner.Key{rk})})
	})
	return nil, false, nil
}

// Set implements auth.CredentialStore. It stores only *tooloauth.Token and
// requires an expiry; the row is kept until then, and a later Set replaces
// it, expiry included.
func (s *Store) Set(ctx context.Context, key auth.CredentialKey, cred auth.Credential, expiresAt time.Time) error {
	if cred == nil {
		return errors.New("spannerstore: Set requires a credential")
	}
	if expiresAt.IsZero() {
		return errors.New("spannerstore: Set requires an expiry")
	}
	tok, ok := cred.(*tooloauth.Token)
	if !ok || tok == nil {
		return errors.New("spannerstore: unsupported credential type")
	}
	plain, err := json.Marshal(tok)
	if err != nil {
		return errors.New("spannerstore: write credential: unencodable token")
	}
	rk := rowKey(key)
	dataKey := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err := rand.Read(dataKey); err != nil {
		return fmt.Errorf("spannerstore: write credential: %w", err)
	}
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("spannerstore: write credential: %w", err)
	}
	aead, err := newAEAD(dataKey)
	if err != nil {
		return fmt.Errorf("spannerstore: write credential: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plain, rk)
	wrappedKey, err := s.keys.Wrap(ctx, dataKey, rk)
	if err != nil {
		return fmt.Errorf("spannerstore: write credential: wrap data key: %w", err)
	}
	m := spanner.InsertOrUpdate(Table, columns, []any{rk, wrappedKey, nonce, ciphertext, s.keys.KeyName(), expiresAt.UTC()})
	if _, err := s.client.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		return fmt.Errorf("spannerstore: write credential: %w", err)
	}
	return nil
}

// Delete implements auth.CredentialStore. Deleting an absent key is not an
// error.
func (s *Store) Delete(ctx context.Context, key auth.CredentialKey) error {
	if _, err := s.client.Apply(ctx, []*spanner.Mutation{spanner.Delete(Table, spanner.Key{rowKey(key)})}); err != nil {
		return fmt.Errorf("spannerstore: delete credential: %w", err)
	}
	return nil
}

// rowKey is the SHA-256 digest of key's fields, each length-prefixed as
// auth/gcp's joinFields does, so keys a separator join would confuse (app
// "acme", user "bob|X" and app "acme|bob", user "X") get their own rows, and
// no user ID is stored in clear. It doubles as the AAD binding a ciphertext
// and its wrapped data key to their row.
func rowKey(key auth.CredentialKey) []byte {
	var b strings.Builder
	for _, f := range []string{rowKeyVersion, key.AppName, key.UserID, key.Key} {
		b.WriteString(strconv.Itoa(len(f)))
		b.WriteByte(':')
		b.WriteString(f)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return sum[:]
}

// newAEAD returns AES-256-GCM under dataKey.
func newAEAD(dataKey []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
