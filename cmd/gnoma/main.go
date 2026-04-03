package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"encoding/json"
	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	anthropicprov "somegit.dev/Owlibou/gnoma/internal/provider/anthropic"
	"somegit.dev/Owlibou/gnoma/internal/provider/mistral"
	googleprov "somegit.dev/Owlibou/gnoma/internal/provider/google"
	oaiprov "somegit.dev/Owlibou/gnoma/internal/provider/openai"
	"somegit.dev/Owlibou/gnoma/internal/provider/openaicompat"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tui"

	tea "charm.land/bubbletea/v2"
	"somegit.dev/Owlibou/gnoma/internal/tool/bash"
	"somegit.dev/Owlibou/gnoma/internal/tool/fs"
	"somegit.dev/Owlibou/gnoma/internal/tool/sysinfo"
)

func main() {
	var (
		providerName = flag.String("provider", "mistral", "LLM provider")
		model        = flag.String("model", "", "model name (empty = provider default)")
		system       = flag.String("system", defaultSystem, "system prompt")
		apiKey       = flag.String("api-key", "", "API key (or set MISTRAL_API_KEY env)")
		maxTurns     = flag.Int("max-turns", 50, "max tool-calling rounds per turn")
		permMode     = flag.String("permission", "default", "permission mode (default, accept_edits, bypass, deny, plan, auto)")
		incognito    = flag.Bool("incognito", false, "incognito mode — no persistence, no learning")
		verbose      = flag.Bool("verbose", false, "enable debug logging")
		version      = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *version {
		fmt.Println("gnoma v0.1.0-dev")
		os.Exit(0)
	}

	// Logger
	logLevel := slog.LevelWarn
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Load config (defaults → global → project → env vars)
	cfg, err := gnomacfg.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: config load: %v\n", err)
		defaults := gnomacfg.Defaults()
		cfg = &defaults
	}
	logger.Debug("config loaded",
		"provider", cfg.Provider.Default,
		"model", cfg.Provider.Model,
		"keys", len(cfg.Provider.APIKeys),
		"perm_mode", cfg.Permission.Mode,
		"perm_rules", len(cfg.Permission.Rules),
	)

	// CLI flags override config
	if !isFlagSet("provider") {
		*providerName = cfg.Provider.Default
	}
	if !isFlagSet("model") && cfg.Provider.Model != "" {
		*model = cfg.Provider.Model
	}
	if !isFlagSet("permission") && cfg.Permission.Mode != "" {
		*permMode = cfg.Permission.Mode
	}

	// Resolve API key: CLI flag → config → env vars
	localProviders := map[string]bool{"ollama": true, "llamacpp": true}
	key := *apiKey
	if key == "" {
		if cfgKey, ok := cfg.Provider.APIKeys[*providerName]; ok && cfgKey != "" {
			key = cfgKey
		}
	}
	if key == "" {
		key = resolveAPIKey(*providerName)
	}
	if key == "" && !localProviders[*providerName] {
		fmt.Fprintf(os.Stderr, "error: no API key for provider %q\nSet %s environment variable or use --api-key\n",
			*providerName, envKeyFor(*providerName))
		os.Exit(1)
	}

	// Resolve base URL from config endpoints
	baseURL := cfg.Provider.Endpoints[*providerName]

	// Create provider
	prov, err := createProvider(*providerName, key, *model, baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Create tool registry
	reg := buildToolRegistry()

	// Harvest shell aliases
	aliases, err := bash.HarvestAliases(context.Background())
	if err != nil {
		logger.Debug("alias harvest failed (non-fatal)", "error", err)
	} else {
		logger.Debug("harvested aliases", "count", aliases.Len())
	}

	// Harvest system inventory
	inventory := bash.HarvestInventory(context.Background())
	logger.Debug("system inventory",
		"tools", len(inventory.Tools),
		"runtimes", len(inventory.Runtimes),
	)

	// Re-register bash tool with aliases and config timeout
	bashOpts := []bash.Option{bash.WithAliases(aliases)}
	if cfg.Tools.BashTimeout.Duration() > 0 {
		bashOpts = append(bashOpts, bash.WithTimeout(cfg.Tools.BashTimeout.Duration()))
	}
	reg.Register(bash.New(bashOpts...))

	// Register system_info tool backed by the inventory
	reg.Register(sysinfo.New(inventory))

	// Create router and register the provider as a single arm
	// (M4 foundation: one provider from CLI. Multi-provider routing comes with config.)
	rtr := router.New(router.Config{Logger: logger})
	armModel := *model
	if armModel == "" {
		armModel = prov.DefaultModel()
	}
	armID := router.NewArmID(*providerName, armModel)
	rtr.RegisterArm(&router.Arm{
		ID:        armID,
		Provider:  prov,
		ModelName: armModel,
		IsLocal:   localProviders[*providerName],
		Capabilities: provider.Capabilities{ToolUse: true}, // trust CLI provider
	})
	rtr.ForceArm(armID)

	// Discover local models (ollama + llama.cpp) and register as additional arms
	localModels := router.DiscoverLocalModels(context.Background(), logger,
		cfg.Provider.Endpoints["ollama"],
		cfg.Provider.Endpoints["llamacpp"],
	)
	router.RegisterDiscoveredModels(rtr, localModels, func(provName, model string) provider.Provider {
		p, err := createProvider(provName, "", model, cfg.Provider.Endpoints[provName])
		if err != nil {
			return nil
		}
		return p
	})
	if len(localModels) > 0 {
		logger.Debug("local models discovered", "count", len(localModels))
	}

	// Create firewall
	fw := security.NewFirewall(security.FirewallConfig{
		ScanOutgoing:     true,
		ScanToolResults:  true,
		EntropyThreshold: 4.5,
		Logger:           logger,
	})

	// Incognito mode
	if *incognito {
		fw.Incognito().Activate()
		logger.Debug("incognito mode enabled")
	}

	// Permission checker with console prompt for pipe mode
	pipePromptFn := func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		fmt.Fprintf(os.Stderr, "⚠ Tool %s wants to execute. Allow? [y/N] ", toolName)
		var response string
		fmt.Scanln(&response)
		return strings.ToLower(response) == "y" || strings.ToLower(response) == "yes", nil
	}
	// Convert config rules to permission rules
	var permRules []permission.Rule
	for _, r := range cfg.Permission.Rules {
		permRules = append(permRules, permission.Rule{
			Tool:    r.Tool,
			Pattern: r.Pattern,
			Action:  permission.Action(r.Action),
		})
	}
	permChecker := permission.NewChecker(permission.Mode(*permMode), permRules, pipePromptFn)

	// Build system prompt with compact inventory summary
	systemPrompt := *system
	if summary := inventory.Summary(); summary != "" {
		systemPrompt = systemPrompt + "\n\n" + summary
	}

	// Create engine
	eng, err := engine.New(engine.Config{
		Provider:    prov,
		Router:      rtr,
		Tools:       reg,
		Firewall:    fw,
		Permissions: permChecker,
		System:      systemPrompt,
		Model:       *model,
		MaxTurns:    *maxTurns,
		Logger:   logger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Detect mode: TUI (interactive TTY) or pipe mode
	input, err := readInput(flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if input != "" {
		// Pipe mode: single input → stream to stdout
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		cb := func(evt stream.Event) {
			switch evt.Type {
			case stream.EventTextDelta:
				if evt.Text != "" {
					fmt.Print(evt.Text)
				}
			case stream.EventToolResult:
				fmt.Printf("\n[%s] %s\n", evt.ToolName, evt.ToolOutput)
			}
		}

		_, err = eng.Submit(ctx, input, cb)
		fmt.Println()

		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "\ninterrupted")
				os.Exit(130)
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// TUI mode: permission prompts via channels
		permCh := make(chan bool)                      // TUI → engine: y/n response
		permReqCh := make(chan string, 1)              // engine → TUI: tool name requesting permission
		permChecker.SetPromptFunc(func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
			// Notify TUI that a permission prompt is needed
			select {
			case permReqCh <- toolName:
			default:
			}
			// Block until TUI responds
			select {
			case approved := <-permCh:
				return approved, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		})

		armModel := *model
		if armModel == "" {
			armModel = prov.DefaultModel()
		}
		sess := session.NewLocal(eng, *providerName, armModel)
		defer sess.Close()

		m := tui.New(sess, tui.Config{
			Firewall:    fw,
			Engine:      eng,
			Permissions: permChecker,
			PermCh:      permCh,
			PermReqCh:   permReqCh,
		})
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

func readInput(args []string) (string, error) {
	// Positional args
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}

	// Stdin (pipe mode)
	stat, _ := os.Stdin.Stat()
	if stat.Mode()&os.ModeCharDevice == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	return "", nil
}

func envKeyFor(providerName string) string {
	switch providerName {
	case "mistral":
		return "MISTRAL_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "google":
		return "GEMINI_API_KEY"
	default:
		return strings.ToUpper(providerName) + "_API_KEY"
	}
}

func resolveAPIKey(providerName string) string {
	// Try primary env var
	primary := envKeyFor(providerName)
	if key := os.Getenv(primary); key != "" {
		return key
	}
	// Try common alternatives
	alternatives := map[string][]string{
		"anthropic": {"ANTHROPICS_API_KEY"},
		"google":    {"GOOGLE_API_KEY"},
	}
	for _, alt := range alternatives[providerName] {
		if key := os.Getenv(alt); key != "" {
			return key
		}
	}
	return ""
}

func isFlagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func createProvider(name, apiKey, model, baseURL string) (provider.Provider, error) {
	cfg := provider.ProviderConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	}

	switch name {
	case "mistral":
		return mistral.New(cfg)
	case "anthropic":
		return anthropicprov.New(cfg)
	case "openai":
		return oaiprov.New(cfg)
	case "google":
		return googleprov.New(cfg)
	case "ollama":
		return openaicompat.NewOllama(cfg)
	case "llamacpp":
		return openaicompat.NewLlamaCpp(cfg)
	default:
		return nil, fmt.Errorf("unknown provider %q (supports: mistral, anthropic, openai, google, ollama, llamacpp)", name)
	}
}

func buildToolRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.Register(bash.New())
	reg.Register(fs.NewReadTool())
	reg.Register(fs.NewWriteTool())
	reg.Register(fs.NewEditTool())
	reg.Register(fs.NewGlobTool())
	reg.Register(fs.NewGrepTool())
	reg.Register(fs.NewLSTool())
	return reg
}

const defaultSystem = `You are gnoma, a provider-agnostic agentic coding assistant.
You help users with software engineering tasks by reading files, writing code, and executing commands.
Be concise and direct. Use tools when needed to accomplish the task.`
