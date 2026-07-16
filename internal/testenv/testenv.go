// Package testenv sets default environment variables required by go.alis.build/mux
// before that package initializes. Blank-import it from launcher packages that
// transitively depend on mux so go test works without manual env setup.
package testenv

import "os"

func init() {
	if os.Getenv("ALIS_OS_PROJECT") == "" {
		os.Setenv("ALIS_OS_PROJECT", "test")
	}
	if os.Getenv("IDENTITY_SERVICE_URL") == "" {
		os.Setenv("IDENTITY_SERVICE_URL", "http://localhost")
	}
}
