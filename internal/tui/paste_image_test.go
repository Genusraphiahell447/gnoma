package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stagePastedImageCache redirects os.UserCacheDir() to a temp dir by
// overriding XDG_CACHE_HOME. Returns the resolved cache root.
func stagePastedImageCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	return filepath.Join(root, "gnoma", "pasted-images")
}

func TestStorePastedImage_WritesToUserCacheWithRestrictivePerms(t *testing.T) {
	cacheDir := stagePastedImageCache(t)

	path, err := storePastedImage([]byte("png-bytes"), ".png")
	if err != nil {
		t.Fatalf("storePastedImage: %v", err)
	}
	if filepath.Dir(path) != cacheDir {
		t.Errorf("path dir = %q, want %q", filepath.Dir(path), cacheDir)
	}
	if filepath.Ext(path) != ".png" {
		t.Errorf("path ext = %q, want .png", filepath.Ext(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 0600", mode)
	}
	if dirInfo, _ := os.Stat(cacheDir); dirInfo != nil {
		if mode := dirInfo.Mode().Perm(); mode != 0o700 {
			t.Errorf("dir mode = %o, want 0700", mode)
		}
	}
}

func TestStorePastedImage_DoesNotPolluteProjectRoot(t *testing.T) {
	// Make sure the cache dir lookup doesn't fall back to cwd / the
	// project root for any reason. Stage XDG_CACHE_HOME and verify
	// the returned path is under it, not under cwd.
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	cwd, _ := os.Getwd()
	path, err := storePastedImage([]byte("x"), ".png")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, path)
	if err == nil && !filepath.IsAbs(rel) && rel[0] != '.' {
		// path is inside cwd — that would mean we polluted the workdir
		t.Errorf("storePastedImage wrote under cwd at %q", path)
	}
}

func TestPruneStalePastedImages_RemovesOldKeepsFresh(t *testing.T) {
	cacheDir := stagePastedImageCache(t)

	// Manually create one stale + one fresh file (mtime via os.Chtimes).
	stale := filepath.Join(cacheDir, "pasted_image_stale.png")
	fresh := filepath.Join(cacheDir, "pasted_image_fresh.png")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-pastedImageStaleAfter - time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	pruneStalePastedImages(cacheDir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file should be pruned, stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file should survive, stat err = %v", err)
	}
}

func TestPruneStalePastedImages_MissingDirIsNoOp(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("prune panicked on missing dir: %v", r)
		}
	}()
	pruneStalePastedImages(filepath.Join(t.TempDir(), "does", "not", "exist"))
}
