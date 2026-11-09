package spannerstore

import (
	"bytes"
	"context"
	"net"
	"testing"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const testKeyName = "projects/p/locations/global/keyRings/r/cryptoKeys/k"

// kmsServer is an in-process KMS that "encrypts" by prefixing the plaintext
// with the AAD, and checks on Decrypt that the request names testKeyName and
// carries the same AAD.
type kmsServer struct {
	kmspb.UnimplementedKeyManagementServiceServer
}

func (kmsServer) Encrypt(_ context.Context, req *kmspb.EncryptRequest) (*kmspb.EncryptResponse, error) {
	if req.GetName() != testKeyName {
		return nil, status.Errorf(codes.InvalidArgument, "unexpected key %q", req.GetName())
	}
	return &kmspb.EncryptResponse{Ciphertext: append(bytes.Clone(req.GetAdditionalAuthenticatedData()), req.GetPlaintext()...)}, nil
}

func (kmsServer) Decrypt(_ context.Context, req *kmspb.DecryptRequest) (*kmspb.DecryptResponse, error) {
	aad := req.GetAdditionalAuthenticatedData()
	if req.GetName() != testKeyName || !bytes.HasPrefix(req.GetCiphertext(), aad) {
		return nil, status.Error(codes.InvalidArgument, "decryption failed")
	}
	return &kmspb.DecryptResponse{Plaintext: req.GetCiphertext()[len(aad):]}, nil
}

// startKMS serves kmsServer on a loopback port and returns a client for it.
func startKMS(t *testing.T) *kms.KeyManagementClient {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	kmspb.RegisterKeyManagementServiceServer(srv, kmsServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	client, err := kms.NewKeyManagementClient(context.Background(),
		option.WithEndpoint(lis.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		t.Fatalf("kms.NewKeyManagementClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// The KMS wrapper sends the key name and the row binding with both calls.
func TestKMSWrapper_bindsTheAAD(t *testing.T) {
	w := kmsWrapper{client: startKMS(t), keyName: testKeyName}
	ctx := context.Background()

	wrapped, err := w.Wrap(ctx, []byte("data-key"), []byte("row-1"))
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}
	got, err := w.Unwrap(ctx, wrapped, []byte("row-1"))
	if err != nil || string(got) != "data-key" {
		t.Errorf("Unwrap() = %q, %v; want data-key", got, err)
	}
	if _, err := w.Unwrap(ctx, wrapped, []byte("row-2")); err == nil {
		t.Error("Unwrap() with another row's AAD succeeded, want an error")
	}
}

// New wires the real KMS client in: a Store built with it round-trips a
// token through Spanner.
func TestNew_roundTripsWithKMS(t *testing.T) {
	s, err := New(startSpanner(t), startKMS(t), testKeyName)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := s.Set(ctx, key7, tokenFor("at-kms-1"), farFuture()); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	cred, ok, err := s.Get(ctx, key7)
	if err != nil || !ok {
		t.Fatalf("Get() = %v, %v, %v", cred, ok, err)
	}
}
