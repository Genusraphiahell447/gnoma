package google

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/stream"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"google.golang.org/genai"
)

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
		Expiry: creds.Expiry(),
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

func tryLoadOAuthCredentials(filePath string) (*auth.Credentials, error) {
	filePath = expandHome(filePath)
	if _, err := os.Stat(filePath); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var creds oauthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}

	tokVal := creds.Token()
	if tokVal == "" {
		return nil, fmt.Errorf("empty access token")
	}

	expiry := creds.Expiry()
	if !expiry.IsZero() && time.Now().After(expiry) {
		return nil, fmt.Errorf("token expired")
	}

	tp := &fileTokenProvider{filePath: filePath}
	return auth.NewCredentials(&auth.CredentialsOptions{
		TokenProvider: tp,
	}), nil
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
		// Precedence: agy > gemini > adc
		var creds *auth.Credentials

		// 1. Agy credentials
		agyPaths := []string{
			"~/.config/google-antigravity/session.json",
			"~/.config/google-antigravity/oauth_creds.json",
			"~/.config/antigravity/session.json",
			"~/.config/antigravity/oauth_creds.json",
			"~/.config/antigravity-cli/session.json",
			"~/.config/antigravity-cli/oauth_creds.json",
			"~/.gemini/antigravity-cli/oauth_creds.json",
		}
		for _, path := range agyPaths {
			if c, err := tryLoadOAuthCredentials(path); err == nil {
				creds = c
				break
			}
		}

		// 2. Gemini credentials
		if creds == nil {
			geminiPaths := []string{
				"~/.gemini/oauth_creds.json",
				"~/.config/gemini-cli/oauth_creds.json",
			}
			for _, path := range geminiPaths {
				if c, err := tryLoadOAuthCredentials(path); err == nil {
					creds = c
					break
				}
			}
		}

		// 3. Application Default Credentials (ADC)
		if creds == nil {
			if c, err := credentials.DetectDefault(nil); err == nil {
				creds = c
			}
		}

		if creds == nil {
			return nil, fmt.Errorf("google: no credentials found (tried agy session, gemini session, and ADC)")
		}

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
