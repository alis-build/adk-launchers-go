package spannerstore

import (
	"context"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
)

// keyWrapper encrypts and decrypts data keys. Cloud KMS is the production
// one; tests use a fake, since KMS has no owner-provided fake.
type keyWrapper interface {
	// Wrap encrypts plaintext, binding it to aad.
	Wrap(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	// Unwrap decrypts ciphertext, and fails unless aad is the one it was
	// wrapped with.
	Unwrap(ctx context.Context, ciphertext, aad []byte) ([]byte, error)
}

// kmsWrapper wraps data keys with the Cloud KMS key keyName.
type kmsWrapper struct {
	client  *kms.KeyManagementClient
	keyName string
}

// Wrap implements keyWrapper with KMS Encrypt.
func (w kmsWrapper) Wrap(ctx context.Context, plaintext, aad []byte) ([]byte, error) {
	resp, err := w.client.Encrypt(ctx, &kmspb.EncryptRequest{Name: w.keyName, Plaintext: plaintext, AdditionalAuthenticatedData: aad})
	if err != nil {
		return nil, err
	}
	return resp.GetCiphertext(), nil
}

// Unwrap implements keyWrapper with KMS Decrypt.
func (w kmsWrapper) Unwrap(ctx context.Context, ciphertext, aad []byte) ([]byte, error) {
	resp, err := w.client.Decrypt(ctx, &kmspb.DecryptRequest{Name: w.keyName, Ciphertext: ciphertext, AdditionalAuthenticatedData: aad})
	if err != nil {
		return nil, err
	}
	return resp.GetPlaintext(), nil
}
