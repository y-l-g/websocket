package integration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func integrationRootAndBinary(t *testing.T) (string, string) {
	t.Helper()

	rootDir, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve module root: %v", err)
	}

	binPath := os.Getenv("FRANKENPHP_BINARY")
	if binPath == "" {
		binPath = filepath.Join(rootDir, "frankenphp")
	}

	if info, err := os.Stat(binPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("FrankenPHP integration binary not found at %s; build/copy module/frankenphp or set FRANKENPHP_BINARY", binPath)
		}
		t.Fatalf("FrankenPHP integration binary not readable at %s: %v", binPath, err)
	} else if info.IsDir() {
		t.Fatalf("FrankenPHP integration binary path is a directory: %s", binPath)
	}

	return rootDir, binPath
}
