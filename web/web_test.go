package web

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session/compaction"
)

// TestH2CFlag covers the -h2c flag end to end on a real listener: the flag has
// to reach http.Server.Protocols, because gRPC and gRPC-Web handlers registered
// through SetupHostRoutes are unreachable without cleartext HTTP/2.
//
// Ported from ADK's own web launcher test, with the default inverted: this
// launcher serves gRPC on the same listener, so h2c is on unless asked off.
func TestH2CFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantH2C bool
	}{
		{
			name:    "enabled by default",
			wantH2C: true,
		},
		{
			name: "disabled explicitly",
			args: []string{"-h2c=false"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLauncher().(*webLauncher)
			if _, err := l.Parse(tc.args); err != nil {
				t.Fatalf("Parse(%v) failed: %v", tc.args, err)
			}

			// The real handler is the process-wide alismux one, which this test
			// has no routes on; only the protocol negotiation is under test, and
			// that is decided by srv.Protocols rather than by the handler.
			srv := buildHTTPServer("", l.config)
			srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Protocol", r.Proto)
				w.WriteHeader(http.StatusNoContent)
			})

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("net.Listen() failed: %v", err)
			}
			serveErr := make(chan error, 1)
			go func() {
				serveErr <- srv.Serve(listener)
			}()
			t.Cleanup(func() {
				if err := srv.Close(); err != nil {
					t.Errorf("server Close() failed: %v", err)
				}
				if err := <-serveErr; err != http.ErrServerClosed {
					t.Errorf("server Serve() error = %v, want %v", err, http.ErrServerClosed)
				}
			})

			// HTTP/1.1 keeps working either way: turning h2c off must not be the
			// only thing standing between REST clients and the server.
			url := "http://" + listener.Addr().String()
			assertProtocol(t, http.DefaultClient, url, 1)

			h2cProtocols := new(http.Protocols)
			h2cProtocols.SetUnencryptedHTTP2(true)
			h2cClient := &http.Client{
				Transport: &http.Transport{Protocols: h2cProtocols},
			}
			t.Cleanup(h2cClient.CloseIdleConnections)

			resp, err := h2cClient.Get(url)
			if !tc.wantH2C {
				if err == nil {
					if closeErr := resp.Body.Close(); closeErr != nil {
						t.Errorf("response body Close() failed: %v", closeErr)
					}
					t.Fatalf("h2c request unexpectedly succeeded with protocol %q", resp.Proto)
				}
				return
			}
			if err != nil {
				t.Fatalf("h2c request failed: %v", err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("response body Close() failed: %v", err)
				}
			}()
			if resp.ProtoMajor != 2 {
				t.Errorf("h2c response protocol = %q, want HTTP/2", resp.Proto)
			}
			if got := resp.Header.Get("X-Request-Protocol"); got != "HTTP/2.0" {
				t.Errorf("handler request protocol = %q, want %q", got, "HTTP/2.0")
			}
		})
	}
}

// TestExecuteValidatesConfig covers the launcher.Config check Execute runs before
// anything else. Without it an unusable Compaction setting starts a server that
// fails per request instead of failing at boot, and sublaunchers that build their
// runtime lazily (evals) would not surface it until the first request.
func TestExecuteValidatesConfig(t *testing.T) {
	t.Run("an invalid compaction config fails before the server starts", func(t *testing.T) {
		l := NewLauncher()
		config := &launcher.Config{Compaction: &compaction.Config{CompactionInterval: -1}}

		err := l.(*webLauncher).Execute(context.Background(), config, nil)
		if err == nil {
			t.Fatal("Execute() with a negative CompactionInterval returned no error")
		}
		if !strings.Contains(err.Error(), "Compaction") {
			t.Errorf("Execute() error = %v, want it to name the invalid Compaction", err)
		}
	})

	t.Run("a config with no compaction passes the check", func(t *testing.T) {
		l := NewLauncher()
		config := &launcher.Config{}

		// Nil Compaction means compaction is disabled and is valid, so the run
		// gets as far as needing a sublauncher — which is how we know the check
		// passed rather than short-circuited.
		err := l.(*webLauncher).Execute(context.Background(), config, nil)
		if err == nil {
			t.Fatal("Execute() with no sublaunchers returned no error")
		}
		if !strings.Contains(err.Error(), "no active sublaunchers") {
			t.Errorf("Execute() error = %v, want the missing-sublauncher error", err)
		}
	})
}

// assertProtocol drives one request and checks which HTTP version carried it.
func assertProtocol(t *testing.T, client *http.Client, url string, wantMajor int) {
	t.Helper()

	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("response body Close() failed: %v", err)
		}
	}()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("reading response body failed: %v", err)
	}
	if resp.ProtoMajor != wantMajor {
		t.Errorf("response protocol = %q, want HTTP/%d", resp.Proto, wantMajor)
	}
}
