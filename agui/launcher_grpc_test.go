package agui

import (
	"testing"

	historyservice "go.alis.build/agui/history/service"
	"google.golang.org/grpc"
)

func TestWithGRPCRegistrar_registersService(t *testing.T) {
	gs := grpc.NewServer()
	svc := &historyservice.ThreadService{}
	l := NewLauncher("my.agent", WithThreadService(svc), WithGRPCRegistrar(gs)).(*aguiLauncher)

	l.registerGRPC()

	info := gs.GetServiceInfo()
	if _, ok := info[threadGRPCServiceName]; !ok {
		t.Fatalf("expected %q in registered services, got %v", threadGRPCServiceName, info)
	}
}

func TestWithGRPCRegistrar_nilRegistrarIsNoOp(t *testing.T) {
	svc := &historyservice.ThreadService{}
	l := NewLauncher("my.agent", WithThreadService(svc)).(*aguiLauncher)

	l.registerGRPC() // must not panic
}

func TestWithGRPCRegistrar_withoutThreadServiceIsNoOp(t *testing.T) {
	gs := grpc.NewServer()
	l := NewLauncher("my.agent", WithGRPCRegistrar(gs)).(*aguiLauncher)

	l.registerGRPC() // must not panic

	if len(gs.GetServiceInfo()) != 0 {
		t.Fatalf("expected no services registered without WithThreadService, got %v", gs.GetServiceInfo())
	}
}

func TestWithGRPCRegistrar_skipsDoubleRegistration(t *testing.T) {
	gs := grpc.NewServer()
	svc := &historyservice.ThreadService{}
	l := NewLauncher("my.agent", WithThreadService(svc), WithGRPCRegistrar(gs)).(*aguiLauncher)

	l.registerGRPC()
	l.registerGRPC() // must not fatal on duplicate
}

func TestWithGRPCRegistrar_nilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil ServiceRegistrar")
		}
	}()
	WithGRPCRegistrar(nil)
}
