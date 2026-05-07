package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	mrand "math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/hook"
	"somegit.dev/Owlibou/gnoma/internal/skill"
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
	gnomactx "somegit.dev/Owlibou/gnoma/internal/context"
	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/tokenizer"
	anthropicprov "somegit.dev/Owlibou/gnoma/internal/provider/anthropic"
	"somegit.dev/Owlibou/gnoma/internal/provider/mistral"
	googleprov "somegit.dev/Owlibou/gnoma/internal/provider/google"
	oaiprov "somegit.dev/Owlibou/gnoma/internal/provider/openai"
	"somegit.dev/Owlibou/gnoma/internal/provider/openaicompat"
	subprocprov "somegit.dev/Owlibou/gnoma/internal/provider/subprocess"
	"somegit.dev/Owlibou/gnoma/internal/session"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tui"

	tea "charm.land/bubbletea/v2"
	"somegit.dev/Owlibou/gnoma/internal/elf"
	"somegit.dev/Owlibou/gnoma/internal/mcp"
	"somegit.dev/Owlibou/gnoma/internal/plugin"
	"somegit.dev/Owlibou/gnoma/internal/tool/agent"
	"somegit.dev/Owlibou/gnoma/internal/tool/bash"
	"somegit.dev/Owlibou/gnoma/internal/tool/fs"
	"somegit.dev/Owlibou/gnoma/internal/tool/sysinfo"
)

// Set by goreleaser ldflags.
var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

func main() {
	var resumeFlag string
	var (
		providerName = flag.String("provider", "mistral", "LLM provider")
		model        = flag.String("model", "", "model name (empty = provider default)")
		system       = flag.String("system", defaultSystem, "system prompt")
		apiKey       = flag.String("api-key", "", "API key (or set MISTRAL_API_KEY env)")
		maxTurns     = flag.Int("max-turns", 50, "max tool-calling rounds per turn")
		permMode     = flag.String("permission", "auto", "permission mode (default, accept_edits, bypass, deny, plan, auto)")
		incognito    = flag.Bool("incognito", false, "incognito mode — no persistence, no learning")
		verbose      = flag.Bool("verbose", false, "enable debug logging")
		version      = flag.Bool("version", false, "print version and exit")
	)
	flag.StringVar(&resumeFlag, "resume", "", "resume session by ID (omit ID to list sessions)")
	flag.StringVar(&resumeFlag, "r", "", "resume session (shorthand)")
	flag.Parse()

	if *version {
		fmt.Printf("gnoma %s (%s, %s)\n", buildVersion, buildCommit, buildDate)
		os.Exit(0)
	}

	// Logger — detect TUI mode early so logs don't bleed into the terminal UI.
	// TUI = stdin is a character device (interactive TTY) with no positional args.
	logLevel := slog.LevelWarn
	if *verbose {
		logLevel = slog.LevelDebug
	}
	isTUI := func() bool {
		if len(flag.Args()) > 0 {
			return false
		}
		stat, _ := os.Stdin.Stat()
		return stat.Mode()&os.ModeCharDevice != 0
	}()
	var logOut io.Writer = os.Stderr
	if isTUI {
		if *verbose {
			if f, err := os.CreateTemp("", "gnoma-*.log"); err == nil {
				logOut = f
				defer f.Close()
				fmt.Fprintf(os.Stderr, "logging to %s\n", f.Name())
			}
		} else {
			logOut = io.Discard
		}
	}
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

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
	knownProviders := map[string]bool{
		"mistral": true, "anthropic": true, "openai": true,
		"google": true, "ollama": true, "llamacpp": true,
	}
	localProviders := map[string]bool{"ollama": true, "llamacpp": true}

	if !knownProviders[*providerName] {
		fmt.Fprintf(os.Stderr, "error: unknown provider %q\n  available: mistral, anthropic, openai, google, ollama, llamacpp\n  usage:     gnoma --provider <name>\n", *providerName)
		os.Exit(1)
	}

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
		envVar := envKeyFor(*providerName)
		fmt.Fprintf(os.Stderr, "error: no API key for provider %q\n\n", *providerName)
		fmt.Fprintf(os.Stderr, "  Option 1: export %s=<your-key>\n", envVar)
		fmt.Fprintf(os.Stderr, "  Option 2: gnoma --api-key <your-key>\n")
		fmt.Fprintf(os.Stderr, "  Option 3: add to .gnoma/config.toml:\n")
		fmt.Fprintf(os.Stderr, "            [provider.api_keys]\n")
		fmt.Fprintf(os.Stderr, "            %s = \"<your-key>\"\n\n", *providerName)
		fmt.Fprintf(os.Stderr, "For local models (no API key needed): gnoma --provider ollama\n")
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
	if cfg.Tools.MaxFileSize > 0 {
		reg.Register(fs.NewWriteTool(fs.WithMaxFileSize(cfg.Tools.MaxFileSize)))
	}

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

	// Elf manager (created now, agent tool registered after router exists)
	// We'll register the agent tool after the router is created below

	// Create session store
	sessStore := session.NewSessionStore(gnomacfg.ProjectRoot(), cfg.Session.MaxKeep, logger)

	// Create router and register the provider as a single arm
	// (M4 foundation: one provider from CLI. Multi-provider routing comes with config.)
	rtr := router.New(router.Config{Logger: logger})

	// Restore QualityTracker data from disk (best-effort)
	{
		userCfgDir, _ := os.UserConfigDir()
		qualityPath := filepath.Join(userCfgDir, "gnoma", "quality.json")
		if data, err := os.ReadFile(qualityPath); err == nil {
			var snap router.QualitySnapshot
			if err := json.Unmarshal(data, &snap); err == nil {
				rtr.QualityTracker().Restore(snap)
				logger.Debug("quality data restored", "path", qualityPath)
			}
		}
	}

	// Save QualityTracker data on exit (best-effort, suppressed in incognito)
	defer func() {
		if *incognito {
			return
		}
		snap := rtr.QualityTracker().Snapshot()
		data, err := json.Marshal(snap)
		if err != nil {
			return
		}
		userCfgDir, err := os.UserConfigDir()
		if err != nil {
			logger.Warn("quality save skipped: no user config dir", "error", err)
			return
		}
		dir := filepath.Join(userCfgDir, "gnoma")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "quality.json"), data, 0o644)
	}()
	armModel := *model
	if armModel == "" {
		armModel = prov.DefaultModel()
	}
	// When the provider returns a placeholder ("default"), query that specific
	// provider's server for the real model name before registering the arm.
	if armModel == "default" && localProviders[*providerName] {
		if resolved := discoverActiveModel(*providerName, cfg, logger); resolved != "" {
			logger.Debug("resolved placeholder model name", "from", armModel, "to", resolved)
			armModel = resolved
		}
	}
	armID := router.NewArmID(*providerName, armModel)
	armProvider := limitedProvider(prov, *providerName, armModel, cfg)
	arm := &router.Arm{
		ID:        armID,
		Provider:  armProvider,
		ModelName: armModel,
		IsLocal:   localProviders[*providerName],
		Capabilities: provider.Capabilities{ToolUse: true}, // trust CLI provider
	}
	arm.Pools = resolveRateLimitPools(armID, *providerName, armModel, cfg)
	rtr.RegisterArm(arm)
	rtr.ForceArm(armID)
	if len(arm.Pools) > 0 {
		logger.Debug("rate limit pools attached", "arm", armID, "pools", len(arm.Pools))
	}

	// Discover local models (ollama + llama.cpp) and register as additional arms
	localModels := router.DiscoverLocalModels(context.Background(), logger,
		cfg.Provider.Endpoints["ollama"],
		cfg.Provider.Endpoints["llamacpp"],
		nil, // no cache for initial one-shot discovery
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

	// Discover CLI agents (claude, gemini, vibe) and register as arms.
	cliAgents := subprocprov.DiscoverCLIAgents(context.Background())
	for _, agent := range cliAgents {
		cliArmID := router.NewArmID("subprocess", agent.Name)
		if _, exists := rtr.LookupArm(cliArmID); !exists {
			rtr.RegisterArm(&router.Arm{
				ID:           cliArmID,
				Provider:     subprocprov.New(agent),
				ModelName:    agent.Name,
				IsCLIAgent:   true,
				Capabilities: agent.Capabilities,
			})
			logger.Debug("registered CLI agent", "name", agent.Name, "version", agent.Version)
		}
	}
	if len(cliAgents) > 0 {
		logger.Debug("CLI agents discovered", "count", len(cliAgents))
	}

	// Start background discovery polling (30s interval).
	// modelUpdater is set after the session is created so the discovery loop
	// can update the displayed model name when it reconciles the forced arm.
	// modelUpdateCh notifies the TUI to re-render when the model changes.
	var modelMu sync.Mutex
	var modelUpdater func(string)
	modelUpdateCh := make(chan struct{}, 1)
	discoveryCtx, discoveryCancel := context.WithCancel(context.Background())
	defer discoveryCancel()
	providerFactory := func(provName, model string) provider.Provider {
		p, err := createProvider(provName, "", model, cfg.Provider.Endpoints[provName])
		if err != nil {
			return nil
		}
		return p
	}
	router.StartDiscoveryLoop(discoveryCtx, rtr, logger,
		cfg.Provider.Endpoints["ollama"],
		cfg.Provider.Endpoints["llamacpp"],
		providerFactory, 30*time.Second,
		func(newID router.ArmID) {
			modelMu.Lock()
			fn := modelUpdater
			modelMu.Unlock()
			if fn != nil {
				fn(newID.Model())
			}
			select {
			case modelUpdateCh <- struct{}{}:
			default:
			}
		},
	)

	// Create firewall
	entropyThreshold := 4.5
	if cfg.Security.EntropyThreshold > 0 {
		entropyThreshold = cfg.Security.EntropyThreshold
	}
	fw := security.NewFirewall(security.FirewallConfig{
		ScanOutgoing:     true,
		ScanToolResults:  true,
		EntropyThreshold: entropyThreshold,
		Logger:           logger,
	})
	// Wire custom scanner patterns from config
	for _, p := range cfg.Security.Patterns {
		action := security.ActionRedact
		switch p.Action {
		case "block":
			action = security.ActionBlock
		case "warn":
			action = security.ActionWarn
		}
		if err := fw.Scanner().AddPattern(p.Name, p.Regex, action); err != nil {
			logger.Warn("invalid security pattern", "name", p.Name, "error", err)
		}
	}

	// Incognito mode
	if *incognito {
		fw.Incognito().Activate()
		logger.Debug("incognito mode enabled")
	}

	// Permission checker with console prompt for pipe mode.
	// In pure pipe mode (no TTY on stdin), auto-deny — Scanln would block on EOF.
	pipePromptFn := func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		stat, _ := os.Stdin.Stat()
		if stat.Mode()&os.ModeCharDevice == 0 {
			fmt.Fprintf(os.Stderr, "⚠ Tool %s denied (no TTY for prompt, use --permission bypass to allow)\n", toolName)
			return false, nil
		}
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

	// Generate session-scoped ID for /tmp artifact directory
	sessionID := fmt.Sprintf("%s-%06x",
		time.Now().Format("20060102-150405"),
		mrand.Int63()&0xffffff,
	)
	store := persist.New(sessionID)
	logger.Debug("session store initialized", "dir", store.Dir())

	// Create elf manager and register agent tools.
	// Must be created after fw and permChecker so elfs inherit security layers.
	elfMgr := elf.NewManager(elf.ManagerConfig{
		Router:      rtr,
		Tools:       reg,
		Permissions: permChecker,
		Firewall:    fw,
		Store:       store,
		Logger:      logger,
	})
	elfProgressCh := make(chan elf.Progress, 16)
	agentTool := agent.New(elfMgr, store)
	agentTool.SetProgressCh(elfProgressCh)
	reg.Register(agentTool)
	batchTool := agent.NewBatch(elfMgr, store)
	batchTool.SetProgressCh(elfProgressCh)
	reg.Register(batchTool)
	reg.Register(agent.NewListResultsTool(store))
	reg.Register(agent.NewReadResultTool(store))

	// Discover plugins and merge their capabilities.
	pluginLoader := plugin.NewLoader(logger)
	globalPluginDir := filepath.Join(gnomacfg.GlobalConfigDir(), "plugins")
	projectPluginDir := filepath.Join(gnomacfg.ProjectRoot(), ".gnoma", "plugins")
	discoveredPlugins, err := pluginLoader.Discover(globalPluginDir, projectPluginDir)
	if err != nil {
		logger.Warn("plugin discovery error", "error", err)
	}
	enabledSet := resolveEnabledPlugins(cfg.Plugins, discoveredPlugins)
	pluginResult, err := pluginLoader.Load(discoveredPlugins, enabledSet)
	if err != nil {
		logger.Warn("plugin load error", "error", err)
	}

	// Build hook dispatcher from config + plugin hooks.
	// Streamer adapter wraps the router for prompt hooks.
	// ElfSpawnFn closure wraps elfMgr for agent hooks.
	allHooks := make([]gnomacfg.HookConfig, 0, len(cfg.Hooks)+len(pluginResult.Hooks))
	allHooks = append(allHooks, cfg.Hooks...)
	allHooks = append(allHooks, pluginResult.Hooks...)
	hookDefs, err := hook.ParseHookDefs(allHooks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook config error: %v\n", err)
		os.Exit(1)
	}
	hookStreamer := &routerStreamer{router: rtr}
	hookSpawnFn := hook.ElfSpawnFn(func(ctx context.Context, prompt string) (string, error) {
		e, spawnErr := elfMgr.Spawn(ctx, router.TaskReview, prompt, "", 5)
		if spawnErr != nil {
			return "", spawnErr
		}
		result := e.Wait()
		return result.Output, result.Error
	})
	dispatcher, err := hook.NewDispatcher(hookDefs, hookStreamer, hookSpawnFn, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook dispatcher error: %v\n", err)
		os.Exit(1)
	}

	// Start MCP servers (config + plugin) and register tools in the tool registry.
	allMCPServers := make([]gnomacfg.MCPServerConfig, 0, len(cfg.MCPServers)+len(pluginResult.MCPServers))
	allMCPServers = append(allMCPServers, cfg.MCPServers...)
	allMCPServers = append(allMCPServers, pluginResult.MCPServers...)
	var mcpMgr *mcp.Manager
	if len(allMCPServers) > 0 {
		serverCfgs, err := mcp.ParseServerConfigs(allMCPServers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp config error: %v\n", err)
			os.Exit(1)
		}
		mcpMgr = mcp.NewManager(logger)
		if err := mcpMgr.StartAll(context.Background(), serverCfgs, reg); err != nil {
			fmt.Fprintf(os.Stderr, "mcp startup error: %v\n", err)
			os.Exit(1)
		}
		defer mcpMgr.Shutdown()
	}

	// Build skill registry: bundled → user → plugins → project (precedence order).
	skillReg := skill.NewRegistry()
	skillReg.LoadBundled()                                                                 //nolint:errcheck
	skillReg.LoadDir(filepath.Join(gnomacfg.GlobalConfigDir(), "skills"), "user")          //nolint:errcheck
	for _, ps := range pluginResult.Skills {
		skillReg.LoadDir(ps.Dir, ps.Source)                                                //nolint:errcheck
	}
	skillReg.LoadDir(filepath.Join(gnomacfg.ProjectRoot(), ".gnoma", "skills"), "project") //nolint:errcheck

	// Build system prompt with cwd + compact inventory summary
	systemPrompt := *system
	if cwd, err := os.Getwd(); err == nil {
		systemPrompt = systemPrompt + "\n\nWorking directory: " + cwd
	}
	if summary := inventory.Summary(); summary != "" {
		systemPrompt = systemPrompt + "\n\n" + summary
	}
	if aliasSummary := aliases.AliasSummary(); aliasSummary != "" {
		systemPrompt = systemPrompt + "\n" + aliasSummary
	}

	// Load project docs as immutable context prefix
	var prefixMsgs []message.Message
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".gnoma/GNOMA.md"} {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		prefixMsgs = append(prefixMsgs,
			message.NewUserText(fmt.Sprintf("[Project docs: %s]\n\n%s", name, string(data))),
			message.NewAssistantText("I've read the project documentation and will follow these guidelines."),
		)
		logger.Debug("loaded project docs as context prefix", "file", name, "size", len(data))
	}

	// Derive context window size from registered arm capabilities (accurate) or fall back to heuristic
	contextWindowSize := int64(cfg.Provider.MaxTokens) * 20
	if arm, ok := rtr.LookupArm(armID); ok && arm.Capabilities.ContextWindow > 0 {
		contextWindowSize = int64(arm.Capabilities.ContextWindow)
		logger.Debug("context window from arm capabilities", "arm", armID, "context_window", contextWindowSize)
	}

	// Create context window with summarize strategy (falls back to truncation)
	var compactStrategy gnomactx.Strategy
	compactStrategy = gnomactx.NewSummarizeStrategy(prov)
	ctxWindow := gnomactx.NewWindow(gnomactx.WindowConfig{
		MaxTokens:      contextWindowSize,
		Strategy:       compactStrategy,
		PrefixMessages: prefixMsgs,
		Logger:         logger,
		OnPreCompact: func(msgs []message.Message) {
			dispatcher.Fire(hook.PreCompact, hook.MarshalPreCompactPayload(len(msgs), 0)) //nolint:errcheck
		},
	})

	// Wire tokenizer and seed tracker with prefix cost
	tok := tokenizer.ForProvider(prov.Name())
	ctxWindow.Tracker().SetTokenizer(tok)
	if len(prefixMsgs) > 0 {
		prefixTokens := ctxWindow.Tracker().CountMessages(prefixMsgs)
		ctxWindow.Tracker().Set(prefixTokens)
		logger.Debug("prefix token baseline set", "tokens", prefixTokens)
	}

	// Create engine
	eng, err := engine.New(engine.Config{
		Provider:    prov,
		Router:      rtr,
		Tools:       reg,
		Firewall:    fw,
		Permissions: permChecker,
		Context:     ctxWindow,
		System:      systemPrompt,
		Model:       *model,
		Temperature: cfg.Provider.Temperature,
		MaxTurns:    *maxTurns,
		Store:       store,
		Hooks:       dispatcher,
		Logger:      logger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Resume logic: --resume/-r flag
	resumedTurnCount := 0
	openResumePicker := false
	resumeRequested := isFlagSet("resume") || isFlagSet("r")
	if resumeRequested {
		var snap session.Snapshot
		var loadErr error
		if resumeFlag != "" {
			snap, loadErr = sessStore.Load(resumeFlag)
		}
		if resumeFlag == "" || loadErr != nil {
			// No specific ID given (or ID not found): open interactive picker in TUI,
			// or fall back to text list in pipe mode.
			if isTUI {
				openResumePicker = true
			} else {
				sessions, listErr := sessStore.List()
				if listErr != nil || len(sessions) == 0 {
					fmt.Fprintln(os.Stderr, "no saved sessions found")
				} else {
					fmt.Fprintln(os.Stderr, "Saved sessions:")
					fmt.Fprintln(os.Stderr, "")
					for _, m := range sessions {
						fmt.Fprintf(os.Stderr, "  %s  %s/%s  %d turns  %s\n",
							m.ID, m.Provider, m.Model, m.TurnCount,
							m.UpdatedAt.Format("2006-01-02 15:04"),
						)
					}
					if loadErr != nil {
						fmt.Fprintf(os.Stderr, "\nsession %q not found\n", resumeFlag)
					}
				}
				os.Exit(0)
			}
		} else {
			// Valid session found — restore engine state
			eng.SetHistory(snap.Messages)
			eng.SetUsage(snap.Metadata.Usage)
			sessionID = snap.ID
			resumedTurnCount = snap.Metadata.TurnCount
			logger.Info("session resumed", "id", snap.ID, "turns", snap.Metadata.TurnCount)
		}
	}

	// Detect mode: TUI (interactive TTY) or pipe mode
	input, err := readInput(flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Fire SessionStart / SessionEnd lifecycle hooks.
	mode := "tui"
	if input != "" {
		mode = "pipe"
	}
	dispatcher.Fire(hook.SessionStart, hook.MarshalSessionStartPayload(sessionID, mode)) //nolint:errcheck
	defer dispatcher.Fire(hook.SessionEnd, hook.MarshalSessionEndPayload(sessionID, 0)) //nolint:errcheck

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

		// Resolve skill invocations in pipe mode (/skillname args).
		submitInput := input
		if strings.HasPrefix(input, "/") {
			parts := strings.Fields(input)
			name := strings.TrimPrefix(parts[0], "/")
			if sk := skillReg.Get(name); sk != nil {
				args := strings.Join(parts[1:], " ")
				cwd, _ := os.Getwd()
				rendered, renderErr := sk.Render(skill.TemplateData{
					Args:        args,
					Cwd:         cwd,
					ProjectRoot: gnomacfg.ProjectRoot(),
					Local:       localProviders[*providerName],
				})
				if renderErr != nil {
					fmt.Fprintf(os.Stderr, "skill %q: %v\n", name, renderErr)
					os.Exit(1)
				}
				submitInput = rendered
			}
		}

		_, err = eng.Submit(ctx, submitInput, cb)
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
		permCh := make(chan bool)                               // TUI → engine: y/n response
		permReqCh := make(chan tui.PermReqMsg, 1)              // engine → TUI: tool requesting permission
		permChecker.SetPromptFunc(func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
			// Notify TUI that a permission prompt is needed
			select {
			case permReqCh <- tui.PermReqMsg{ToolName: toolName, Args: args}:
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
		sess := session.NewLocal(session.LocalConfig{
			Engine:    eng,
			Provider:  *providerName,
			Model:     armModel,
			SessionID: sessionID,
			TurnCount: resumedTurnCount,
			Store:     sessStore,
			Incognito: fw.Incognito(),
			Logger:    logger,
		})
		defer sess.Close()
		modelMu.Lock()
		modelUpdater = sess.SetModel
		modelMu.Unlock()

		m := tui.New(sess, tui.Config{
			Firewall:              fw,
			Engine:                eng,
			Permissions:           permChecker,
			Router:                rtr,
			ElfManager:            elfMgr,
			PermCh:                permCh,
			PermReqCh:             permReqCh,
			ElfProgress:           elfProgressCh,
			SessionStore:          sessStore,
			StartWithResumePicker: openResumePicker,
			Skills:                skillReg,
		PluginInfos:           buildPluginInfos(discoveredPlugins, enabledSet),
			Version:               buildVersion,
			ModelUpdateCh:         modelUpdateCh,
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

// discoverActiveModel queries the specific local provider's server for the model
// it's currently serving. Returns the model ID or "" if discovery fails.
func discoverActiveModel(provName string, cfg *gnomacfg.Config, logger *slog.Logger) string {
	ctx := context.Background()
	var models []router.DiscoveredModel
	var err error

	switch provName {
	case "llamacpp":
		models, err = router.DiscoverLlamaCpp(ctx, cfg.Provider.Endpoints["llamacpp"])
	case "ollama":
		models, err = router.DiscoverOllama(ctx, cfg.Provider.Endpoints["ollama"], nil)
	default:
		return ""
	}
	if err != nil {
		logger.Debug("active model discovery failed (non-fatal)", "provider", provName, "error", err)
		return ""
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
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
		return nil, fmt.Errorf("unknown provider %q\n  available: mistral, anthropic, openai, google, ollama, llamacpp\n  usage:     gnoma --provider <name>", name)
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

// resolveRateLimitPools builds limit pools for an arm from provider defaults + config overrides.
func resolveRateLimitPools(armID router.ArmID, provName, modelName string, cfg *gnomacfg.Config) []*router.LimitPool {
	defaults := provider.DefaultRateLimits(provName)
	rl, _ := defaults.LookupModel(modelName)

	// Apply config overrides
	if cfg.RateLimits != nil {
		if override, ok := cfg.RateLimits[provName]; ok {
			if override.RPS > 0 {
				rl.RPS = override.RPS
			}
			if override.RPM > 0 {
				rl.RPM = override.RPM
			}
			if override.RPD > 0 {
				rl.RPD = override.RPD
			}
			if override.TPM > 0 {
				rl.TPM = override.TPM
			}
			if override.ITPM > 0 {
				rl.ITPM = override.ITPM
			}
			if override.OTPM > 0 {
				rl.OTPM = override.OTPM
			}
			if override.TokensMonth > 0 {
				rl.TokensMonth = override.TokensMonth
			}
			if override.SpendCap > 0 {
				rl.SpendCap = override.SpendCap
			}
		}
	}

	return router.PoolsFromRateLimits(armID, rl)
}

// limitedProvider wraps p with a concurrency semaphore derived from rate limits.
// All engines (main and elf) sharing the same arm share the same semaphore.
func limitedProvider(p provider.Provider, provName, modelName string, cfg *gnomacfg.Config) provider.Provider {
	defaults := provider.DefaultRateLimits(provName)
	rl, _ := defaults.LookupModel(modelName)
	if cfg.RateLimits != nil {
		if override, ok := cfg.RateLimits[provName]; ok {
			if override.RPS > 0 {
				rl.RPS = override.RPS
			}
			if override.RPM > 0 {
				rl.RPM = override.RPM
			}
		}
	}
	return provider.WithConcurrency(p, rl.MaxConcurrent())
}

// routerStreamer adapts *router.Router to the hook.Streamer interface.
// PromptExecutor needs only a simple Stream(ctx, prompt) call; this adapter
// wraps the full router.Stream signature, using TaskReview for hook evaluation.
type routerStreamer struct {
	router *router.Router
}

func (rs *routerStreamer) Stream(ctx context.Context, prompt string) (stream.Stream, error) {
	req := provider.Request{
		Messages: []message.Message{message.NewUserText(prompt)},
	}
	s, decision, err := rs.router.Stream(ctx, router.Task{Type: router.TaskReview}, req)
	if err != nil {
		return nil, err
	}
	decision.Commit(0)
	return s, nil
}

const defaultSystem = `You are gnoma, a provider-agnostic agentic coding assistant.
You help users with software engineering tasks by reading files, writing code, and executing commands.
Be concise and direct. Use tools when needed to accomplish the task.

When a task involves 2 or more independent sub-tasks, use the spawn_elfs tool to run them in parallel. Examples:
- "fix the tests and update the docs" → spawn 2 elfs (one for tests, one for docs)
- "analyze files A, B, and C" → spawn 3 elfs (one per file)
- "refactor this function" → single sequential workflow (one dependent task)

When using spawn_elfs, list all tasks in one call — do NOT spawn one elf at a time.`

// buildPluginInfos converts discovered plugins into TUI display info.
func buildPluginInfos(plugins []plugin.Plugin, enabledSet map[string]bool) []tui.PluginInfo {
	infos := make([]tui.PluginInfo, 0, len(plugins))
	for _, p := range plugins {
		infos = append(infos, tui.PluginInfo{
			Name:    p.Manifest.Name,
			Version: p.Manifest.Version,
			Scope:   p.Scope,
			Enabled: enabledSet[p.Manifest.Name],
		})
	}
	return infos
}

// resolveEnabledPlugins determines which plugins are enabled based on config.
// If Enabled is empty, all plugins are enabled by default (opt-out via Disabled).
// If Enabled is non-empty, only listed plugins are enabled (opt-in).
// Disabled always takes precedence (veto).
func resolveEnabledPlugins(cfg gnomacfg.PluginsSection, plugins []plugin.Plugin) map[string]bool {
	disabled := make(map[string]bool, len(cfg.Disabled))
	for _, name := range cfg.Disabled {
		disabled[name] = true
	}

	result := make(map[string]bool, len(plugins))

	if len(cfg.Enabled) == 0 {
		// Opt-out mode: all plugins enabled unless in disabled list.
		for _, p := range plugins {
			if !disabled[p.Manifest.Name] {
				result[p.Manifest.Name] = true
			}
		}
	} else {
		// Opt-in mode: only listed plugins enabled.
		for _, name := range cfg.Enabled {
			if !disabled[name] {
				result[name] = true
			}
		}
	}

	return result
}
