package stream

import "testing"

func TestIsPendingProxyResponse(t *testing.T) {
	if IsPendingProxyResponse(nil) {
		t.Fatal("nil response should not be pending")
	}
	if IsPendingProxyResponse(map[string]any{"status": "ok"}) {
		t.Fatal("non-pending status")
	}
	if !IsPendingProxyResponse(map[string]any{"status": "pending"}) {
		t.Fatal("pending proxy response")
	}
}
