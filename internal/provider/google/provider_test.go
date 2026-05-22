package google

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/auth"

	_ "somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestTryLoadOAuthCredentials_Formats(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		data        interface{}
		expectError bool
		checkToken  string
		checkExpiry time.Time
	}{
		{
			name: "snake_case and seconds expiry",
			data: oauthCreds{
				AccessToken: "token-snake",
				ExpiryDate:  time.Now().Add(1 * time.Hour).Unix(),
				TokenType:   "Bearer",
			},
			expectError: false,
			checkToken:  "token-snake",
		},
		{
			name: "camelCase and milliseconds expiry",
			data: oauthCreds{
				AccessToken2: "token-camel",
				ExpiresAt:    time.Now().Add(1 * time.Hour).UnixNano() / 1e6,
				TokenType2:   "Bearer",
			},
			expectError: false,
			checkToken:  "token-camel",
		},
		{
			name: "expired token",
			data: oauthCreds{
				AccessToken: "token-expired",
				ExpiryDate:  time.Now().Add(-1 * time.Hour).Unix(),
			},
			expectError: true,
		},
		{
			name: "missing access token",
			data: oauthCreds{
				ExpiryDate: time.Now().Add(1 * time.Hour).Unix(),
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, "creds.json")
			bz, err := json.Marshal(tc.data)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if err := os.WriteFile(filePath, bz, 0644); err != nil {
				t.Fatalf("write file failed: %v", err)
			}

			creds, err := tryLoadOAuthCredentials(filePath)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tok, err := creds.Token(context.Background())
			if err != nil {
				t.Fatalf("failed to get token: %v", err)
			}

			if tok.Value != tc.checkToken {
				t.Errorf("expected token %q, got %q", tc.checkToken, tok.Value)
			}
		})
	}
}

func TestSelectOAuthCredentials_Precedence(t *testing.T) {
	// Override HOME so expandHome() resolves into a sandbox dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	writeCreds := func(relPath, tokenVal string) {
		absPath := filepath.Join(tmpHome, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		data := oauthCreds{
			AccessToken: tokenVal,
			ExpiryDate:  time.Now().Add(1 * time.Hour).Unix(),
		}
		bz, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, bz, 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	tokenOf := func(c *auth.Credentials) string {
		t.Helper()
		tok, err := c.Token(context.Background())
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		return tok.Value
	}

	t.Run("agy beats gemini when both present", func(t *testing.T) {
		// Fresh sandbox per subtest to avoid leftover files.
		sub := t.TempDir()
		t.Setenv("HOME", sub)
		// Use the first agy path and the first gemini path.
		writeAt := func(rel, tok string) {
			abs := filepath.Join(sub, rel)
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				t.Fatal(err)
			}
			bz, _ := json.Marshal(oauthCreds{
				AccessToken: tok,
				ExpiryDate:  time.Now().Add(time.Hour).Unix(),
			})
			if err := os.WriteFile(abs, bz, 0600); err != nil {
				t.Fatal(err)
			}
		}
		writeAt(filepath.Join(".config", "google-antigravity", "session.json"), "token-agy")
		writeAt(filepath.Join(".gemini", "oauth_creds.json"), "token-gemini")

		creds, source, err := selectOAuthCredentials()
		if err != nil {
			t.Fatalf("selectOAuthCredentials: %v", err)
		}
		if source != CredentialSourceAgy {
			t.Errorf("source = %q, want %q", source, CredentialSourceAgy)
		}
		if got := tokenOf(creds); got != "token-agy" {
			t.Errorf("loaded token = %q, want token-agy (agy precedence violated)", got)
		}
	})

	t.Run("falls back to gemini when agy missing", func(t *testing.T) {
		sub := t.TempDir()
		t.Setenv("HOME", sub)
		// Only gemini file present.
		geminiPath := filepath.Join(sub, ".gemini", "oauth_creds.json")
		if err := os.MkdirAll(filepath.Dir(geminiPath), 0755); err != nil {
			t.Fatal(err)
		}
		bz, _ := json.Marshal(oauthCreds{
			AccessToken: "token-gemini-only",
			ExpiryDate:  time.Now().Add(time.Hour).Unix(),
		})
		if err := os.WriteFile(geminiPath, bz, 0600); err != nil {
			t.Fatal(err)
		}

		creds, source, err := selectOAuthCredentials()
		if err != nil {
			t.Fatalf("selectOAuthCredentials: %v", err)
		}
		if source != CredentialSourceGemini {
			t.Errorf("source = %q, want %q", source, CredentialSourceGemini)
		}
		if got := tokenOf(creds); got != "token-gemini-only" {
			t.Errorf("loaded token = %q, want token-gemini-only", got)
		}
	})

	t.Run("missing files are not warning-worthy", func(t *testing.T) {
		// Sanity check: empty home directory walks the chain without
		// failing in unexpected ways (only ADC would remain, which we
		// don't assert on here because the test host may or may not have
		// gcloud configured).
		sub := t.TempDir()
		t.Setenv("HOME", sub)
		_, _, err := selectOAuthCredentials()
		// Either ADC works on this host (no error) or no creds anywhere
		// (returns our specific "no google credentials" error). Both are
		// fine; the point is we don't panic or report a misconfiguration.
		if err != nil && !strings.Contains(err.Error(), "no google credentials") {
			t.Errorf("unexpected error shape: %v", err)
		}
	})
	_ = writeCreds // keep helper available if extended in future
}

func TestFileTokenProvider_RejectsExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	bz, _ := json.Marshal(oauthCreds{
		AccessToken: "stale",
		ExpiryDate:  time.Now().Add(-time.Hour).Unix(),
	})
	if err := os.WriteFile(path, bz, 0600); err != nil {
		t.Fatal(err)
	}
	tp := &fileTokenProvider{filePath: path}
	tok, err := tp.Token(context.Background())
	if err == nil {
		t.Errorf("expected error for expired token, got token %+v", tok)
	}
	if err != nil && !strings.Contains(err.Error(), "expired") {
		t.Errorf("error %q should mention expiry", err)
	}
}
