package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"google.golang.org/genai"
)

// cloudPlatformScope is the standard OAuth scope used for Vertex AI and
// the Gemini API on Google Cloud. credentials.DetectDefault REQUIRES at
// least Scopes or Audience to be set — calling it with nil options
// returns "credentials: options must be provided" and the ADC branch
// becomes dead code.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

const defaultModel = "gemini-3.5-flash"

// Provider implements provider.Provider for Google's Gemini API.
type Provider struct {
	client *genai.Client
	name   string
	model  string
}

type oauthCreds struct {
	AccessToken   string `json:"access_token"`
	AccessToken2  string `json:"accessToken"`
	ExpiryDate    int64  `json:"expiry_date"`
	ExpiresAt     int64  `json:"expiresAt"`
	RefreshToken  string `json:"refresh_token"`
	RefreshToken2 string `json:"refreshToken"`
	TokenType     string `json:"token_type"`
	TokenType2    string `json:"tokenType"`
}

func (c *oauthCreds) Token() string {
	if c.AccessToken != "" {
		return c.AccessToken
	}
	return c.AccessToken2
}

func (c *oauthCreds) Expiry() time.Time {
	val := c.ExpiryDate
	if val == 0 {
		val = c.ExpiresAt
	}
	if val > 0 {
		if val > 9999999999 {
			return time.UnixMilli(val)
		}
		return time.Unix(val, 0)
	}
	return time.Time{}
}

type fileTokenProvider struct {
	filePath string
}

func (tp *fileTokenProvider) Token(ctx context.Context) (*auth.Token, error) {
	data, err := os.ReadFile(tp.filePath)
	if err != nil {
		return nil, fmt.Errorf("read oauth credentials: %w", err)
	}

	var creds oauthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse oauth credentials: %w", err)
	}

	tokVal := creds.Token()
	if tokVal == "" {
		return nil, fmt.Errorf("no access token in credentials file")
	}

	// We don't perform an OAuth refresh exchange ourselves; the upstream
	// CLI (gemini / antigravity) refreshes the file out-of-band. If we're
	// asked for a token after expiry and the file hasn't been refreshed,
	// fail loudly with an actionable message instead of sending a known-
	// dead bearer that the API would reject with a confusing 401.
	expiry := creds.Expiry()
	if !expiry.IsZero() && time.Now().After(expiry) {
		return nil, fmt.Errorf("oauth token at %s is expired (re-run the upstream CLI to refresh)", tp.filePath)
	}

	tokenType := creds.TokenType
	if tokenType == "" {
		tokenType = creds.TokenType2
	}
	if tokenType == "" {
		tokenType = "Bearer"
	}

	return &auth.Token{
		Value:  tokVal,
		Type:   tokenType,
		Expiry: expiry,
	}, nil
}

func expandHome(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if len(path) == 1 {
		return home
	}
	if path[1] == '/' || path[1] == '\\' {
		return filepath.Join(home, path[2:])
	}
	return path
}

// errCredentialMissing wraps os.ErrNotExist for the precedence walker so
// the "file isn't there" case is silent while permission / parse / empty-
// token failures get a slog.Warn (they typically indicate a misconfigured
// install — chmod 0600 on the wrong file, half-written JSON, etc.).
var errCredentialMissing = errors.New("credential file not present")

func tryLoadOAuthCredentials(filePath string) (*auth.Credentials, error) {
	expanded := expandHome(filePath)
	if _, err := os.Stat(expanded); err != nil {
		if os.IsNotExist(err) {
			return nil, errCredentialMissing
		}
		slog.Warn("google oauth: stat failed", "path", expanded, "err", err)
		return nil, err
	}

	data, err := os.ReadFile(expanded)
	if err != nil {
		slog.Warn("google oauth: read failed", "path", expanded, "err", err)
		return nil, err
	}

	var creds oauthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		slog.Warn("google oauth: parse failed", "path", expanded, "err", err)
		return nil, err
	}

	tokVal := creds.Token()
	if tokVal == "" {
		slog.Warn("google oauth: empty access token", "path", expanded)
		return nil, fmt.Errorf("empty access token in %s", expanded)
	}

	expiry := creds.Expiry()
	if !expiry.IsZero() && time.Now().After(expiry) {
		slog.Warn("google oauth: token expired", "path", expanded, "expired_at", expiry)
		return nil, fmt.Errorf("token in %s expired at %s", expanded, expiry.Format(time.RFC3339))
	}

	tp := &fileTokenProvider{filePath: expanded}
	return auth.NewCredentials(&auth.CredentialsOptions{
		TokenProvider: tp,
	}), nil
}

// CredentialSource labels the origin of the auth credential returned by
// selectOAuthCredentials. Used by tests and diagnostics.
type CredentialSource string

const (
	CredentialSourceNone   CredentialSource = ""
	CredentialSourceAgy    CredentialSource = "agy"
	CredentialSourceGemini CredentialSource = "gemini"
	CredentialSourceADC    CredentialSource = "adc"
)

// agyCredentialPaths lists the OAuth credential file locations that the
// agy / antigravity CLIs are known to write to. First match wins.
var agyCredentialPaths = []string{
	"~/.config/google-antigravity/session.json",
	"~/.config/google-antigravity/oauth_creds.json",
	"~/.config/antigravity/session.json",
	"~/.config/antigravity/oauth_creds.json",
	"~/.config/antigravity-cli/session.json",
	"~/.config/antigravity-cli/oauth_creds.json",
	"~/.gemini/antigravity-cli/oauth_creds.json",
}

// geminiCredentialPaths lists the locations the official gemini CLI uses.
var geminiCredentialPaths = []string{
	"~/.gemini/oauth_creds.json",
	"~/.config/gemini-cli/oauth_creds.json",
}

// selectOAuthCredentials walks the precedence chain (agy → gemini → ADC)
// and returns the first usable credential plus a tag identifying which
// source it came from. Tests use the tag to verify precedence; the New()
// builder discards it.
func selectOAuthCredentials() (*auth.Credentials, CredentialSource, error) {
	for _, path := range agyCredentialPaths {
		if c, err := tryLoadOAuthCredentials(path); err == nil {
			return c, CredentialSourceAgy, nil
		}
	}
	for _, path := range geminiCredentialPaths {
		if c, err := tryLoadOAuthCredentials(path); err == nil {
			return c, CredentialSourceGemini, nil
		}
	}
	// Application Default Credentials. DetectDefault REQUIRES scopes —
	// passing nil makes the call always error, leaving ADC unreachable.
	c, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes: []string{cloudPlatformScope},
	})
	if err == nil {
		return c, CredentialSourceADC, nil
	}
	slog.Debug("google adc: DetectDefault failed", "err", err)
	return nil, CredentialSourceNone, fmt.Errorf("no google credentials found (tried agy session, gemini session, and ADC)")
}

// New creates a Google GenAI provider from config.
func New(cfg provider.ProviderConfig) (provider.Provider, error) {
	var client *genai.Client
	var err error

	if cfg.APIKey != "" {
		client, err = genai.NewClient(context.Background(), &genai.ClientConfig{
			APIKey:  cfg.APIKey,
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			return nil, fmt.Errorf("google: create client (Gemini API): %w", err)
		}
	} else {
		creds, source, selErr := selectOAuthCredentials()
		if selErr != nil {
			return nil, fmt.Errorf("google: %w", selErr)
		}
		slog.Debug("google auth: credential selected", "source", source)

		// Resolve Project ID
		var projectID string
		if projectVal, ok := cfg.Options["project"]; ok {
			if s, ok := projectVal.(string); ok {
				projectID = s
			}
		}
		if projectID == "" {
			if projectIDVal, ok := cfg.Options["project_id"]; ok {
				if s, ok := projectIDVal.(string); ok {
					projectID = s
				}
			}
		}
		if projectID == "" && creds != nil {
			if pid, err := creds.ProjectID(context.Background()); err == nil && pid != "" {
				projectID = pid
			}
		}
		if projectID == "" {
			projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		}
		if projectID == "" {
			projectID = os.Getenv("GOOGLE_PROJECT")
		}
		if projectID == "" {
			return nil, fmt.Errorf("google: project id is required for Vertex AI backend")
		}

		// Resolve Location
		var location string
		if locVal, ok := cfg.Options["location"]; ok {
			if s, ok := locVal.(string); ok {
				location = s
			}
		}
		if location == "" {
			if regVal, ok := cfg.Options["region"]; ok {
				if s, ok := regVal.(string); ok {
					location = s
				}
			}
		}
		if location == "" {
			location = os.Getenv("GOOGLE_CLOUD_LOCATION")
		}
		if location == "" {
			location = os.Getenv("GOOGLE_CLOUD_REGION")
		}
		if location == "" {
			location = "us-central1"
		}

		client, err = genai.NewClient(context.Background(), &genai.ClientConfig{
			Backend:     genai.BackendVertexAI,
			Credentials: creds,
			Project:     projectID,
			Location:    location,
		})
		if err != nil {
			return nil, fmt.Errorf("google: create client (Vertex AI): %w", err)
		}
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	return &Provider{
		client: client,
		name:   "google",
		model:  model,
	}, nil
}

// Stream initiates a streaming content generation request.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (stream.Stream, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	contents := translateContents(req.Messages)
	config := translateConfig(req)

	iter := p.client.Models.GenerateContentStream(ctx, model, contents, config)

	return newGoogleStream(ctx, iter, model), nil
}

// Name returns "google".
func (p *Provider) Name() string { return p.name }

// DefaultModel returns the configured default model.
func (p *Provider) DefaultModel() string { return p.model }

// Models returns available Google models with capabilities by querying the API.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	var models []provider.ModelInfo
	for model, err := range p.client.Models.All(ctx) {
		if err != nil {
			// Fallback to hardcoded list if API call fails
			return p.fallbackModels(), nil
		}

		caps := inferGoogleModelCapabilities(model)
		models = append(models, provider.ModelInfo{
			ID:           model.Name,
			Name:         model.DisplayName,
			Provider:     p.name,
			Capabilities: caps,
		})
	}

	if len(models) == 0 {
		// API returned no models, use fallback
		return p.fallbackModels(), nil
	}

	return models, nil
}

// fallbackModels returns a hardcoded list of known Google models.
func (p *Provider) fallbackModels() []provider.ModelInfo {
	return []provider.ModelInfo{
		{
			ID: "gemini-3.1-pro-preview", Name: "Gemini 3.1 Pro", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 1048576,
				MaxOutput:     65536,
			},
		},
		{
			ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 1048576,
				MaxOutput:     65536,
			},
		},
		{
			ID: "gemini-3.1-flash-lite", Name: "Gemini 3.1 Flash Lite", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 1048576, MaxOutput: 65536,
			},
		},
		// Legacy IDs retained for users pinned to older models.
		{
			ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro (legacy)", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 1048576,
				MaxOutput:     65536,
			},
		},
		{
			ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash (legacy)", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse:       true,
				JSONOutput:    true,
				ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
				Vision:        true,
				ContextWindow: 1048576,
				MaxOutput:     65536,
			},
		},
		{
			ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash (legacy)", Provider: p.name,
			Capabilities: provider.Capabilities{
				ToolUse: true, JSONOutput: true, Vision: true,
				ContextWindow: 1048576, MaxOutput: 8192,
			},
		},
	}
}

// inferGoogleModelCapabilities infers capabilities from the Google Model.
func inferGoogleModelCapabilities(m *genai.Model) provider.Capabilities {
	// Default capabilities for most modern Gemini models
	caps := provider.Capabilities{
		ToolUse:       true,
		JSONOutput:    true,
		Vision:        true,
		ThinkingModes: []provider.EffortLevel{provider.EffortLow, provider.EffortMedium, provider.EffortHigh},
		ContextWindow: 1048576,
		MaxOutput:     65536,
	}

	// Model-specific overrides based on model name
	switch m.Name {
	case "gemini-3.1-pro-preview", "gemini-3.5-flash", "gemini-3.1-flash-lite":
		caps.ContextWindow = 1048576
		caps.MaxOutput = 65536
	case "gemini-2.5-pro", "gemini-2.5-flash":
		caps.ContextWindow = 1048576
		caps.MaxOutput = 65536
	case "gemini-2.0-pro", "gemini-2.0-flash":
		caps.ContextWindow = 1048576
		caps.MaxOutput = 8192
	case "gemini-1.5-pro", "gemini-1.5-flash":
		caps.ContextWindow = 1048576
		caps.MaxOutput = 8192
	}

	return caps
}
