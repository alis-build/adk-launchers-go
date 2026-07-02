package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestGorillaPostHostPatterns_apiA2aLRO(t *testing.T) {
	router := mux.NewRouter().StrictSlash(true)

	router.Methods(http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions).
		PathPrefix("/api").
		Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	router.Methods(http.MethodPost).
		Path("/a2a/v1/invoke").
		Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	router.PathPrefix("/resume-operation/").Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	patterns, err := gorillaPostHostPatterns(router)
	if err != nil {
		t.Fatalf("gorillaPostHostPatterns: %v", err)
	}

	want := []string{
		"POST /a2a/v1/invoke",
		"POST /api/",
		"POST /resume-operation/",
	}
	if strings.Join(patterns, "\n") != strings.Join(want, "\n") {
		t.Fatalf("patterns:\n%s\nwant:\n%s", strings.Join(patterns, "\n"), strings.Join(want, "\n"))
	}
}

func TestPostHostPattern_skipsRootCatchAll(t *testing.T) {
	route := mux.NewRouter().PathPrefix("/").Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	tpl, err := route.GetPathTemplate()
	if err != nil {
		t.Fatalf("GetPathTemplate: %v", err)
	}
	if got := postHostPattern(tpl, route); got != "" {
		t.Fatalf("postHostPattern(%q) = %q, want empty", tpl, got)
	}
}

func TestHostServeMuxUnmethodedCatchAllWithSpecificGET(t *testing.T) {
	gorilla := mux.NewRouter()
	gorilla.HandleFunc("/ui/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	host := http.NewServeMux()
	host.HandleFunc("GET /agui/threads/{threadId}/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	host.HandleFunc("/", gorilla.ServeHTTP)

	srv := httptest.NewServer(host)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/agui/threads/t1/messages")
	if err != nil {
		t.Fatalf("get agui: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("agui status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}

	resp, err = http.Get(srv.URL + "/ui/")
	if err != nil {
		t.Fatalf("get ui: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ui status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHostServeMuxPostPrecedenceOverGRPCFallback(t *testing.T) {
	gorilla := mux.NewRouter().StrictSlash(true)
	gorilla.Methods(http.MethodPost).PathPrefix("/api").Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	host := http.NewServeMux()
	host.HandleFunc("POST /", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "grpc fallback", http.StatusNotFound)
	})
	host.HandleFunc("POST /api/", gorilla.ServeHTTP)

	srv := httptest.NewServer(host)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/apps/test.agent.v1/users/user/sessions", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %q, want %d", resp.StatusCode, body, http.StatusTeapot)
	}
}
