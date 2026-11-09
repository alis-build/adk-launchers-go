// Package spannerstore keeps tooloauth's tokens in Cloud Spanner, encrypted
// with Cloud KMS. It implements ADK's auth.CredentialStore; pass a *Store as
// tooloauth.Config.Store.
//
// Each token is sealed with AES-256-GCM under a fresh data key, and the data
// key is encrypted with a Cloud KMS key (envelope encryption). Both are bound
// to the row they are written to, so a ciphertext copied into another user's
// row does not decrypt. Anyone who can read the database but not decrypt
// with the KMS key sees only ciphertext. The row key is a digest of the
// app, user and slot, so no user ID is stored in clear.
//
// Create the table with [DDL]. Its row deletion policy makes a row eligible
// for deletion a day after its expiry, and Spanner removes it in the
// background, typically within three days; the store never serves a row past
// its expiry.
//
// The service account needs roles/spanner.databaseUser on the database and
// roles/cloudkms.cryptoKeyEncrypterDecrypter on the key.
//
// Only *tooloauth.Token credentials are stored; Set refuses any other kind,
// as the CredentialStore contract allows for what a store cannot encode.
//
// # Setup
//
//	client, err := spanner.NewClient(ctx, "projects/p/instances/i/databases/d")
//	kmsClient, err := kms.NewKeyManagementClient(ctx)
//	store, err := spannerstore.New(client, kmsClient,
//		"projects/p/locations/global/keyRings/agents/cryptoKeys/tool-tokens")
//	cfg := tooloauth.Config{Tools: tools, Store: store}
//
// Pass the CryptoKey's name, not one of its versions.
//
// # Key rotation
//
// Each row's data key is wrapped by the KMS key version that was primary when
// the row was written, and KMS picks the version to decrypt with from the
// ciphertext. Rotating the key therefore needs no migration: old rows keep
// decrypting while their version is enabled, and rows written afterwards use
// the new one. Since every refresh rewrites the row, active users move to the
// new version on their own. Do not disable or destroy a version while rows
// wrapped by it are still unexpired: those users' sign-ins would read as
// unavailable until the rows expire.
package spannerstore
