package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeSet initialises l's storage and writes app/set1 through it.
func storeSet(t *testing.T, l *evalsLauncher) {
	t.Helper()
	if err := l.initStorage(nil); err != nil {
		t.Fatalf("initStorage: %v", err)
	}
	if _, err := l.setsManager.CreateEvalSet("app", "set1"); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// Acceptance 5; mirrors adk-python test_evals.py:143.
func TestEvalStorageURIFromEnvStoresUnderPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADK_EVAL_STORAGE_URI", "file://"+dir+"/evals")
	storeSet(t, NewLauncher().(*evalsLauncher))
	if !exists(t, filepath.Join(dir, "evals", "app", "set1.evalset.json")) {
		t.Error("set1 not stored under the URI's path")
	}
}

// Mirrors adk-python test_evals.py:164.
func TestEvalStorageFileURICreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := NewLauncher(WithEvalStorageURI("file://" + dir)).(*evalsLauncher).initStorage(nil); err != nil {
		t.Fatalf("initStorage: %v", err)
	}
	if !exists(t, dir) {
		t.Error("storage directory not created")
	}
}

// Mirrors adk-python test_evals.py:177.
func TestEvalStorageAcceptsLocalhostFileURI(t *testing.T) {
	dir := t.TempDir()
	storeSet(t, NewLauncher(WithEvalStorageURI("file://localhost"+dir+"/x")).(*evalsLauncher))
	if !exists(t, filepath.Join(dir, "x", "app", "set1.evalset.json")) {
		t.Error("set1 not stored under the localhost URI's path")
	}
}

// Mirrors adk-python test_evals.py:190, :200, :241.
func TestEvalStorageRejectsBadURIs(t *testing.T) {
	t.Setenv("ADK_EVAL_STORAGE_URI", "")
	for uri, want := range map[string]string{
		"file://remote/x": "file:// URIs must target the local host",
		// net/url would drop the user and see localhost; urlparse does not.
		"file://user:pw@localhost/x": "file:// URIs must target the local host",
		"file:relative/x":            "file:// URIs must use an absolute path",
		"s3://b":                     "Unsupported evals storage URI",
	} {
		err := NewLauncher(WithEvalStorageURI(uri)).(*evalsLauncher).initStorage(nil)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.HasPrefix(err.Error(), "evals: eval_storage_uri: ") {
			t.Errorf("%s error = %v, want %q", uri, err, want)
		}
	}
}

// Mirrors adk-python test_evals.py:119.
func TestEvalStorageExplicitURIWinsOverEnv(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	t.Setenv("ADK_EVAL_STORAGE_URI", "file://"+b)
	storeSet(t, NewLauncher(WithEvalStorageURI("file://"+a)).(*evalsLauncher))
	if !exists(t, filepath.Join(a, "app", "set1.evalset.json")) || exists(t, filepath.Join(b, "app", "set1.evalset.json")) {
		t.Error("set1 not stored under the explicit URI alone")
	}
}

// Mirrors adk-python test_evals.py:246, failing deterministically on a path
// under a regular file.
func TestEvalStorageUnwritableDirectoryNamesEnvVar(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := NewLauncher(WithEvalStorageURI("file://" + f + "/sub")).(*evalsLauncher).initStorage(nil)
	if err == nil || !strings.Contains(err.Error(), "ADK_EVAL_STORAGE_URI") || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %v, want one naming ADK_EVAL_STORAGE_URI and not a directory", err)
	}
}

// Escapes are decoded and a malformed % kept, as Python's unquote does.
func TestEvalStorageFileURIDecodesPath(t *testing.T) {
	dir := t.TempDir()
	if err := NewLauncher(WithEvalStorageURI("file://" + dir + "/a%20b/100%")).(*evalsLauncher).initStorage(nil); err != nil {
		t.Fatalf("initStorage: %v", err)
	}
	if !exists(t, filepath.Join(dir, "a b", "100%")) {
		t.Error("decoded directory not created")
	}
}
