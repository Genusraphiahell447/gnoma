package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"somegit.dev/Owlibou/gnoma/internal/slm"
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
		providerName = flag.String("provider", "", "LLM provider (mistral, anthropic, openai, google, ollama, llamacpp)")
		model        = flag.String("model", "", "model name (empty = provider default)")
		system       = flag.String("system", "", "system prompt override (empty = built-in default)")
		apiKey       = flag.String("api-key", "", "API key (or set MISTRAL_API_KEY env)")
		maxTurns     = flag.Int("max-turns", 50, "max tool-calling rounds per turn")
		permMode     = flag.String("permission", "auto", "permission mode (default, accept_edits, bypass, deny, plan, auto)")
		incognito    = flag.Bool("incognito", false, "incognito mode — no persistence, no learning")
		profileFlag  = flag.String("profile", "", "config profile to load (empty = default_profile from base config)")
		verbose      = flag.Bool("verbose", false, "enable debug logging")
		version      = flag.Bool("version", false, "print version and exit")
	)
	flag.StringVar(&resumeFlag, "resume", "", "resume session by ID (omit ID to list sessions)")
	flag.StringVar(&resumeFlag, "r", "", "resume session (shorthand)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  gnoma [flags]           open interactive TUI\n")
		fmt.Fprintf(os.Stderr, "  gnoma [flags] <prompt>  run a single prompt (pipe mode)\n")
		fmt.Fprintf(os.Stderr, "\nSubcommands:\n")
		fmt.Fprintf(os.Stderr, "  gnoma providers         list all discovered providers and models\n")
		fmt.Fprintf(os.Stderr, "  gnoma profile list      list configured profiles\n")
		fmt.Fprintf(os.Stderr, "  gnoma profile show NAME show the effective config a profile produces\n")
		fmt.Fprintf(os.Stderr, "  gnoma slm setup         download and verify the llamafile model\n")
		fmt.Fprintf(os.Stderr, "  gnoma slm status        show SLM setup state\n")
		fmt.Fprintf(os.Stderr, "  gnoma router stats      show router quality + classifier telemetry\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}
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
				defer func() { _ = f.Close() }()
				fmt.Fprintf(os.Stderr, "logging to %s\n", f.Name())
			}
		} else {
			logOut = io.Discard
		}
	}
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// Load config (defaults → global base → profile → project → env vars).
	// Profile mode engages only when ~/.config/gnoma/profiles/ exists.
	cfg, profile, err := gnomacfg.LoadWithProfile(*profileFlag)
	if err != nil {
		// `gnoma profile list/show` is the recovery affordance for
		// profile-resolution errors — it must run even when the user's
		// profile setup is broken. Other commands fail fast.
		args := flag.Args()
		introspectingProfiles := len(args) > 0 && args[0] == "profile"
		if errors.Is(err, gnomacfg.ErrProfileResolution) && !introspectingProfiles {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(2)
		}
		if errors.Is(err, gnomacfg.ErrProfileResolution) {
			// Fall through with a base-only load so `profile list/show`
			// can still surface default_profile and diagnose what's
			// broken. LoadBase ignores missing-file errors.
			base, _ := gnomacfg.LoadBase()
			cfg = base
			profile = gnomacfg.Profile{}
		} else {
			fmt.Fprintf(os.Stderr, "warning: config load: %v\n", err)
			defaults := gnomacfg.Defaults()
			cfg = &defaults
		}
	}
	logger.Debug("config loaded",
		"provider", cfg.Provider.Default,
		"model", cfg.Provider.Model,
		"keys", len(cfg.Provider.APIKeys),
		"perm_mode", cfg.Permission.Mode,
		"perm_rules", len(cfg.Permission.Rules),
		"profile_active", profile.Active,
		"profile_name", profile.Name,
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

	// Subcommand dispatch (before provider creation)
	if cliArgs := flag.Args(); len(cliArgs) > 0 {
		switch cliArgs[0] {
		case "providers":
			os.Exit(runProvidersCommand(cfg, logger))
		case "slm":
			os.Exit(runSLMCommand(cliArgs[1:], cfg, logger))
		case "router":
			os.Exit(runRouterCommand(cliArgs[1:], profile))
		case "profile":
			os.Exit(runProfileCommand(cliArgs[1:], cfg, profile))
		}
	}

	knownProviders := map[string]bool{
		"mistral": true, "anthropic": true, "openai": true,
		"google": true, "ollama": true, "llamacpp": true,
	}
	localProviders := map[string]bool{"ollama": true, "llamacpp": true}

	// Create the primary provider.
	// In TUI mode errors are non-fatal: a stubProvider lets the TUI start and surfaces
	// the problem on the first request. CLI agents (claude, gemini) wired in below as
	// router arms will work regardless of the primary provider.
	var prov provider.Provider
	primaryProviderOK := false

	if *providerName == "" {
		// No provider configured at all.
		if !isTUI {
			fmt.Fprintln(os.Stderr, "error: no provider configured")
			fmt.Fprintln(os.Stderr, "\nSet a provider in ~/.config/gnoma/config.toml:")
			fmt.Fprintln(os.Stderr, "  [provider]")
			fmt.Fprintln(os.Stderr, `  default = "anthropic"`)
			fmt.Fprintln(os.Stderr, `  [provider.api_keys]`)
			fmt.Fprintln(os.Stderr, `  anthropic = "sk-ant-..."`)
			fmt.Fprintln(os.Stderr, "\nOr use a local model:  gnoma --provider ollama")
			fmt.Fprintln(os.Stderr, "Or use the claude CLI: gnoma --provider subprocess (auto-detected)")
			os.Exit(1)
		}
		prov = newStubProvider("no provider configured — set [provider] in ~/.config/gnoma/config.toml")
		logger.Warn("no provider configured; CLI agents will be used if available")
	} else {
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
			if !isTUI {
				fmt.Fprintf(os.Stderr, "error: no API key for provider %q\n\n", *providerName)
				fmt.Fprintf(os.Stderr, "  Option 1: export %s=<your-key>\n", envVar)
				fmt.Fprintf(os.Stderr, "  Option 2: gnoma --api-key <your-key>\n")
				fmt.Fprintf(os.Stderr, "  Option 3: add to ~/.config/gnoma/config.toml:\n")
				fmt.Fprintf(os.Stderr, "            [provider]\n")
				fmt.Fprintf(os.Stderr, "            default = \"%s\"\n", *providerName)
				fmt.Fprintf(os.Stderr, "            [provider.api_keys]\n")
				fmt.Fprintf(os.Stderr, "            %s = \"<your-key>\"\n\n", *providerName)
				fmt.Fprintf(os.Stderr, "For local models (no API key needed): gnoma --provider ollama\n")
				os.Exit(1)
			}
			prov = newStubProvider(fmt.Sprintf("no API key for %s — export %s or set it in config", *providerName, envVar))
			logger.Warn("no API key configured", "provider", *providerName, "env", envVar)
		} else {
			baseURL := cfg.Provider.Endpoints[*providerName]
			p, err := createProvider(*providerName, key, *model, baseURL)
			if err != nil {
				if !isTUI {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					os.Exit(1)
				}
				prov = newStubProvider(err.Error())
				logger.Warn("provider creation failed", "provider", *providerName, "error", err)
			} else {
				prov = p
				primaryProviderOK = true
			}
		}
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		fmt.Fprintf(os.Stderr, "error: cannot resolve working directory: %v\n", cwdErr)
		os.Exit(1)
	}
	fsGuard, err := fs.NewGuard(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: workspace guard: %v\n", err)
		os.Exit(1)
	}

	// Create tool registry
	reg := buildToolRegistry(fsGuard)
	if cfg.Tools.MaxFileSize > 0 {
		w := fs.NewWriteTool(fs.WithMaxFileSize(cfg.Tools.MaxFileSize))
		w.SetGuard(fsGuard)
		reg.Register(w)
	}

	// Harvest aliases, inventory, CLI agents, and local models in parallel.
	// These are independent and together account for most startup latency.
	var (
		aliases     *bash.AliasMap
		inventory   *bash.SystemInventory
		cliAgents   []subprocprov.DiscoveredAgent
		localModels []router.DiscoveredModel
	)
	{
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			a, err := bash.HarvestAliases(context.Background())
			if err != nil {
				logger.Debug("alias harvest failed (non-fatal)", "error", err)
			} else {
				aliases = a
			}
		}()
		go func() {
			defer wg.Done()
			inventory = bash.HarvestInventory(context.Background())
		}()
		go func() {
			defer wg.Done()
			cliAgents = subprocprov.DiscoverCLIAgents(context.Background(), cfg.CLIAgents)
		}()
		go func() {
			defer wg.Done()
			localModels = router.DiscoverLocalModels(context.Background(), logger,
				cfg.Provider.Endpoints["ollama"],
				cfg.Provider.Endpoints["llamacpp"],
				nil,
			)
		}()
		wg.Wait()
	}
	if aliases != nil {
		logger.Debug("harvested aliases", "count", aliases.Len())
	}
	if inventory != nil {
		logger.Debug("system inventory", "tools", len(inventory.Tools), "runtimes", len(inventory.Runtimes))
	} else {
		inventory = &bash.SystemInventory{}
	}

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

	// Create session store. Per-profile session dir keeps work/private
	// sessions from cross-contaminating the resume list.
	sessStore := session.NewSessionStoreAt(profile.SessionDir(gnomacfg.ProjectRoot()), cfg.Session.MaxKeep, logger)

	// FirewallRef holds the *Firewall via atomic.Pointer so it can be
	// installed into SafeProvider wrappers before NewFirewall runs below
	// (~line 509). Until Set, wrappers pass through unmodified — matching
	// pre-firewall behaviour for early-init code paths.
	fwRef := new(security.FirewallRef)

	// Create router and register the provider as a single arm
	// (M4 foundation: one provider from CLI. Multi-provider routing comes with config.)
	rtr := router.New(router.Config{Logger: logger})

	// Restore QualityTracker data from disk (best-effort). Per-profile
	// path avoids bandit cross-contamination between work/private/etc.
	// Skipped under --incognito to keep prior learned quality out of the
	// session's selection biases. (TUI-runtime incognito can't fire this
	// early — the firewall doesn't exist yet — so the CLI flag is the
	// only source of truth at restore time.)
	if !*incognito {
		qualityPath := profile.QualityFile(gnomacfg.GlobalConfigDir())
		if data, err := os.ReadFile(qualityPath); err == nil {
			var snap router.QualitySnapshot
			if err := json.Unmarshal(data, &snap); err == nil {
				rtr.QualityTracker().Restore(snap)
				logger.Debug("quality data restored", "path", qualityPath)
			}
		}
	}

	// Save QualityTracker data on exit (best-effort, suppressed in
	// incognito). Lifted to a named closure so the /profile switch path
	// can fire it explicitly before syscall.Exec, since defers don't run
	// after a successful exec.
	//
	// Two gates: the CLI flag is the unconditional first check (covers
	// the window between `defer saveQuality()` here and `fwRef.Set(fw)`
	// downstream, where fwRef.Get() still returns nil). The firewall
	// state is the second check so TUI-runtime Ctrl+X correctly
	// suppresses the snapshot on exit.
	saveQuality := func() {
		if *incognito {
			return
		}
		if fw := fwRef.Get(); fw != nil && !fw.Incognito().ShouldLearn() {
			return
		}
		snap := rtr.QualityTracker().Snapshot()
		data, err := json.Marshal(snap)
		if err != nil {
			return
		}
		qualityPath := profile.QualityFile(gnomacfg.GlobalConfigDir())
		_ = os.MkdirAll(filepath.Dir(qualityPath), 0o700)
		_ = os.WriteFile(qualityPath, data, 0o600)
	}
	defer saveQuality()
	var armID router.ArmID
	if primaryProviderOK {
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
		armID = router.NewArmID(*providerName, armModel)
		armProvider := security.WrapProvider(limitedProvider(prov, *providerName, armModel, cfg), fwRef)
		arm := &router.Arm{
			ID:        armID,
			Provider:  armProvider,
			ModelName: armModel,
			IsLocal:   localProviders[*providerName],
			Capabilities: provider.Capabilities{ToolUse: true},
		}
		arm.Pools = resolveRateLimitPools(armID, *providerName, armModel, cfg)
		rtr.RegisterArm(arm)
		// Pin this arm only when the user passed --provider explicitly on
		// the command line. A config-default provider is *one* registered
		// arm among many — the router still picks by tier and score, which
		// is what lets the SLM arm win trivial tasks. Without this guard,
		// every gnoma launch with [provider].default set short-circuits
		// the whole routing tree to one arm.
		if isFlagSet("provider") {
			rtr.ForceArm(armID)
			logger.Info("provider pinned via --provider flag", "arm", armID)
		}
		if len(arm.Pools) > 0 {
			logger.Debug("rate limit pools attached", "arm", armID, "pools", len(arm.Pools))
		}
	}

	// Register local models discovered above in parallel.
	router.RegisterDiscoveredModels(rtr, localModels, func(provName, model string) router.SecureProvider {
		p, err := createProvider(provName, "", model, cfg.Provider.Endpoints[provName])
		if err != nil {
			return nil
		}
		return security.WrapProvider(p, fwRef)
	})
	if len(localModels) > 0 {
		logger.Debug("local models discovered", "count", len(localModels))
	}

	// Register CLI agents discovered above in parallel.
	for _, agent := range cliAgents {
		cliArmID := router.NewArmID("subprocess", agent.Name)
		if _, exists := rtr.LookupArm(cliArmID); !exists {
			rtr.RegisterArm(&router.Arm{
				ID:           cliArmID,
				Provider:     security.WrapProvider(subprocprov.New(agent), fwRef),
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

	// Apply [[arms]] overrides (strengths, cost_weight) now that all initial
	// arms are registered. Late-discovered arms (background polling) won't
	// pick these up — by design: overrides target arms the user knows exist.
	if len(cfg.Arms) > 0 {
		overrides := make([]router.ArmOverride, 0, len(cfg.Arms))
		for _, ac := range cfg.Arms {
			overrides = append(overrides, router.ArmOverride{
				ID:         ac.ID,
				Strengths:  ac.Strengths,
				CostWeight: ac.CostWeight,
			})
		}
		if unknown := rtr.ApplyArmOverrides(overrides); len(unknown) > 0 {
			logger.Warn("[[arms]] config references unregistered arm IDs",
				"ids", unknown,
				"hint", "run `gnoma providers` to see registered arms",
			)
		}
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
		return security.WrapProvider(p, fwRef)
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
	// Install into the ref so every SafeProvider wrapper sees scanning
	// from this point on. Wrappers created before this Set call
	// pass through; wrappers created after see the active firewall.
	fwRef.Set(fw)
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

	// Incognito mode. Both flags must move together — the firewall owns
	// the intent flag, the router owns the local-only enforcement, and
	// they go out of sync if either is set in isolation. We also reject
	// the --incognito + --provider <cloud-arm> combination here rather
	// than silently routing to a forced cloud arm under an "incognito"
	// badge (audit finding W2-1).
	if *incognito {
		if forced := rtr.ForcedArm(); forced != "" {
			if arm, ok := rtr.LookupArm(forced); ok && !arm.IsLocal {
				fmt.Fprintf(os.Stderr,
					"error: --incognito conflicts with --provider %s (non-local arm). "+
						"Clear the provider pin or drop --incognito.\n",
					forced,
				)
				os.Exit(1)
			}
		}
		fw.Incognito().Activate()
		rtr.SetLocalOnly(true)
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
		// Scanln error → empty response → falls through to deny below.
		_, _ = fmt.Scanln(&response)
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
	// Pass the firewall's incognito mode so Save no-ops while incognito
	// is active. Mode is consulted on every Save (dynamic), so TUI
	// runtime toggles take effect without reconstructing the store.
	store := persist.New(sessionID, fw.Incognito())
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
	pinStorePath := filepath.Join(gnomacfg.GlobalConfigDir(), "plugins.pins.toml")
	pinStore, pinErr := plugin.NewFilePinStore(pinStorePath)
	if pinErr != nil {
		logger.Warn("plugin pin store unavailable; plugins will load without trust pinning", "error", pinErr)
		pinStore = nil
	}
	pluginResult, err := pluginLoader.Load(discoveredPlugins, enabledSet, pinStore)
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
		defer func() {
			if err := mcpMgr.Shutdown(); err != nil {
				logger.Warn("mcp shutdown error", "error", err)
			}
		}()
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
	if systemPrompt == "" {
		systemPrompt = defaultSystem
	}
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

	// Create context window with summarize strategy (falls back to truncation).
	// The summarizer talks to the provider directly (no engine.buildRequest in
	// between), so it needs the SafeProvider wrapper to inherit firewall
	// scanning. The engine itself still scans inline at buildRequest().
	var compactStrategy gnomactx.Strategy = gnomactx.NewSummarizeStrategy(security.WrapProvider(prov, fwRef))
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

	// Wire SLM through the pluggable backend factory. The user picks a
	// backend (ollama / llamacpp / llamafile / openaicompat / auto / disabled)
	// in [slm].backend; the factory returns a ready provider or nil if the
	// chosen backend isn't reachable. See docs/slm-backends.md.
	lazy := &lazyClassifier{logger: logger}
	var engineClassifier router.TaskClassifier = lazy
	var slmCleanup func() error
	var slmInfo tui.SLMInfo
	if cfg.SLM.Enabled {
		slmInfo.Enabled = true
		bcfg := slm.BackendConfig{
			Backend:        slm.Backend(cfg.SLM.Backend),
			Model:          cfg.SLM.Model,
			BaseURL:        cfg.SLM.BaseURL,
			ModelURL:       cfg.SLM.ModelURL,
			DataDir:        cfg.SLM.DataDir,
			StartupTimeout: cfg.SLM.StartupTimeout.Duration(),
		}
		fmt.Fprintln(os.Stderr, "Starting SLM...")
		boot, bootErr := slm.StartBackend(context.Background(), bcfg, logger)
		switch {
		case bootErr != nil:
			fmt.Fprintf(os.Stderr, "SLM unavailable: %v — using heuristic classifier.\n", bootErr)
		case boot == nil:
			fmt.Fprintln(os.Stderr, "SLM unavailable: no backend reachable — using heuristic classifier.")
		default:
			// Wrap once — the SLM provider is used both as the classifier
			// transport and as a router arm. Both paths route through the
			// firewall after fwRef.Set fires above.
			slmProvider := security.WrapProvider(boot.Provider, fwRef)
			lazy.set(slm.NewClassifier(slmProvider, boot.Model, logger))
			// ToolUse comes from the live probe of the actual model. For
			// completion-only models (e.g. TinyLlama), the SLM arm only
			// handles knowledge-only prompts where the trivial-prompt
			// heuristic flipped RequiresTools=false. For tool-capable
			// models, the SLM also covers simple file reads etc., gated
			// by MaxComplexity=0.3.
			rtr.RegisterArm(&router.Arm{
				ID:            router.ArmID("slm/" + string(boot.Backend)),
				Provider:      slmProvider,
				ModelName:     boot.Model,
				IsLocal:       true,
				MaxComplexity: 0.3,
				Capabilities:  provider.Capabilities{ToolUse: boot.ToolSupport},
			})
			slmCleanup = boot.Close
			slmInfo.Active = true
			slmInfo.Backend = string(boot.Backend)
			slmInfo.Model = boot.Model
			slmInfo.Tools = boot.ToolSupport
			toolNote := "no tools"
			if boot.ToolSupport {
				toolNote = "tools"
			}
			fmt.Fprintf(os.Stderr, "SLM ready (%s, model=%s, %s, boot=%s)\n",
				boot.Backend, boot.Model, toolNote, boot.BootTime.Round(time.Millisecond))
			logger.Info("SLM ready",
				"backend", boot.Backend,
				"model", boot.Model,
				"tool_support", boot.ToolSupport,
				"boot_time", boot.BootTime,
			)
		}
	}
	if slmCleanup != nil {
		defer func() { _ = slmCleanup() }()
	}

	// Create engine
	eng, err := engine.New(engine.Config{
		// Wrap even though the engine's own buildRequest scans inline —
		// belt-and-suspenders so a future engine path that bypasses
		// buildRequest still routes through the firewall.
		Provider:    security.WrapProvider(prov, fwRef),
		Router:      rtr,
		Classifier:  engineClassifier,
		Tools:       reg,
		Firewall:    fw,
		Permissions: permChecker,
		Context:     ctxWindow,
		System:      systemPrompt,
		Model:       *model,
		Temperature: cfg.Provider.Temperature,
		MaxTurns:    *maxTurns,
		Store:              store,
		Hooks:              dispatcher,
		Logger:             logger,
		ForceTwoStageTools: cfg.Router.ForceTwoStage,
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
		var submitOpts engine.TurnOptions
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
				submitOpts = engine.TurnOptions{
					AllowedTools: sk.Frontmatter.AllowedTools,
					AllowedPaths: sk.Frontmatter.Paths,
				}
			}
		}

		_, err = eng.SubmitWithOptions(ctx, submitInput, submitOpts, cb)
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
		defer func() {
			if err := sess.Close(); err != nil {
				logger.Warn("session close error", "error", err)
			}
		}()
		modelMu.Lock()
		modelUpdater = sess.SetModel
		modelMu.Unlock()

		profileNames, _ := gnomacfg.ListProfiles()
		var switchTarget string

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
			SLM:                   slmInfo,
			Profile:               tui.ProfileInfo{Active: profile.Active, Name: profile.Name},
			ProfileNames:          profileNames,
			SwitchProfile:         func(name string) { switchTarget = name },
		})
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		// Re-exec for profile switching. syscall.Exec replaces this
		// process image so defers do NOT run — fire the cleanups that
		// matter (quality snapshot, SLM subprocess, session state)
		// explicitly before handing off.
		if switchTarget != "" {
			saveQuality()
			if slmCleanup != nil {
				_ = slmCleanup()
			}
			if err := sess.Close(); err != nil {
				logger.Warn("session close error before profile switch", "error", err)
			}
			if err := reExecForProfileSwitch(switchTarget); err != nil {
				fmt.Fprintf(os.Stderr, "profile switch failed: %v\n", err)
				os.Exit(1)
			}
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

func buildToolRegistry(guard *fs.Guard) *tool.Registry {
	reg := tool.NewRegistry()
	reg.Register(bash.New())

	read := fs.NewReadTool()
	read.SetGuard(guard)
	reg.Register(read)

	write := fs.NewWriteTool()
	write.SetGuard(guard)
	reg.Register(write)

	edit := fs.NewEditTool()
	edit.SetGuard(guard)
	reg.Register(edit)

	glob := fs.NewGlobTool()
	glob.SetGuard(guard)
	reg.Register(glob)

	grep := fs.NewGrepTool()
	grep.SetGuard(guard)
	reg.Register(grep)

	ls := fs.NewLSTool()
	ls.SetGuard(guard)
	reg.Register(ls)

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

// lazyClassifier wraps a TaskClassifier that is set after startup.
// Falls back to HeuristicClassifier until the real one is available.
// Logs the first deferred-fallback at INFO so operators can tell when the
// SLM was not yet ready vs. unconfigured.
type lazyClassifier struct {
	mu              sync.Mutex
	inner           router.TaskClassifier
	deferredLogged  bool
	logger          *slog.Logger
}

func (l *lazyClassifier) set(c router.TaskClassifier) {
	l.mu.Lock()
	l.inner = c
	l.mu.Unlock()
}

func (l *lazyClassifier) Classify(ctx context.Context, prompt string, history []message.Message) (router.Task, error) {
	l.mu.Lock()
	c := l.inner
	logger := l.logger
	logFirst := !l.deferredLogged && c == nil
	if logFirst {
		l.deferredLogged = true
	}
	l.mu.Unlock()
	if c != nil {
		return c.Classify(ctx, prompt, history)
	}
	if logFirst && logger != nil {
		logger.Info("classifier: SLM not yet ready, using heuristic fallback")
	}
	return router.HeuristicClassifier{}.Classify(ctx, prompt, history)
}

// stubProvider is a no-op provider used when no real provider is configured.
// It lets gnoma start in TUI mode so CLI agent arms (claude, gemini, etc.) can
// still be used via the router. Stream returns a user-visible error.
type stubProvider struct{ reason string }

func newStubProvider(reason string) provider.Provider { return &stubProvider{reason: reason} }

func (s *stubProvider) Name() string { return "none" }
func (s *stubProvider) DefaultModel() string { return "none" }
func (s *stubProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, fmt.Errorf("%s", s.reason)
}
func (s *stubProvider) Stream(_ context.Context, _ provider.Request) (stream.Stream, error) {
	return nil, fmt.Errorf("%s", s.reason)
}

// runProvidersCommand handles `gnoma providers`.
// Prints all auto-discovered providers, CLI agents, local models, and SLM status.
func runProvidersCommand(cfg *gnomacfg.Config, logger *slog.Logger) int {
	ctx := context.Background()

	fmt.Println("Configured provider:")
	if cfg.Provider.Default != "" {
		key := cfg.Provider.APIKeys[cfg.Provider.Default]
		keyHint := "(no key)"
		if key != "" {
			if len(key) > 8 {
				keyHint = key[:4] + "..." + key[len(key)-4:]
			} else {
				keyHint = "(set)"
			}
		}
		model := cfg.Provider.Model
		if model == "" {
			model = "(default)"
		}
		fmt.Printf("  %-12s  model: %-30s  key: %s\n", cfg.Provider.Default, model, keyHint)
	} else {
		fmt.Println("  (none — set [provider] in ~/.config/gnoma/config.toml)")
	}

	fmt.Println("\nCLI agents (auto-discovered):")
	agents := subprocprov.DiscoverCLIAgents(ctx, cfg.CLIAgents)
	if len(agents) == 0 {
		fmt.Println("  (none found on PATH)")
	}
	for _, a := range agents {
		label := a.Name
		if a.OverrideBinary != "" {
			label = fmt.Sprintf("%s (via [cli_agents].%s)", a.OverrideBinary, a.Name)
		}
		fmt.Printf("  %-32s  version: %-20s  path: %s\n", label, a.Version, a.Path)
	}

	fmt.Println("\nLocal models (ollama / llama.cpp):")
	localModels := router.DiscoverLocalModels(ctx, logger,
		cfg.Provider.Endpoints["ollama"],
		cfg.Provider.Endpoints["llamacpp"],
		nil,
	)
	if len(localModels) == 0 {
		fmt.Println("  (none running)")
	}
	for _, m := range localModels {
		tools := "no tools"
		if m.SupportsTools {
			tools = "tools"
		}
		fmt.Printf("  %-12s  model: %-30s  %s\n", m.Provider, m.ID, tools)
	}

	fmt.Println("\nSLM (llamafile):")
	slmDataDir := cfg.SLM.DataDir
	if slmDataDir == "" {
		slmDataDir = slm.DefaultDataDir()
	}
	slmMgr := slm.New(slm.Config{DataDir: slmDataDir, ModelURL: cfg.SLM.ModelURL}, logger)
	status := slmMgr.Status()
	enabled := "disabled"
	if cfg.SLM.Enabled {
		enabled = "enabled"
	}
	fmt.Printf("  status: %-10s  (%s)\n", status, enabled)
	if mf := slmMgr.Manifest(); mf != nil {
		fmt.Printf("  file:   %s (%s)\n", mf.FilePath, humanBytes(mf.Size))
	}

	fmt.Println("\nTo set a provider:")
	fmt.Println("  gnoma --provider anthropic --model claude-opus-4-5  (one-off)")
	fmt.Println("  gnoma config set provider.default anthropic          (permanent)")

	return 0
}

// runSLMCommand handles `gnoma slm <subcommand>`.
// Returns an exit code.
func runSLMCommand(args []string, cfg *gnomacfg.Config, logger *slog.Logger) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: gnoma slm <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  setup   download and verify the llamafile model")
		fmt.Fprintln(os.Stderr, "  status  show current setup state")
		return 1
	}

	dataDir := cfg.SLM.DataDir
	if dataDir == "" {
		dataDir = slm.DefaultDataDir()
	}
	mgr := slm.New(slm.Config{
		DataDir:        dataDir,
		ModelURL:       cfg.SLM.ModelURL,
		ExpectedSHA256: cfg.SLM.ExpectedSHA256,
	}, logger)

	switch args[0] {
	case "setup":
		if cfg.SLM.ModelURL == "" {
			fmt.Printf("no model_url configured; writing default to %s\n", gnomacfg.GlobalConfigPath())
			if err := gnomacfg.SetGlobalConfig("slm.model_url", slm.DefaultModelURL); err != nil {
				fmt.Fprintf(os.Stderr, "error: could not write config: %v\n", err)
				fmt.Fprintln(os.Stderr, "Set model_url manually:")
				fmt.Fprintln(os.Stderr, "  [slm]")
				fmt.Fprintf(os.Stderr, "  model_url = %q\n", slm.DefaultModelURL)
				return 1
			}
			cfg.SLM.ModelURL = slm.DefaultModelURL
			if cfg.SLM.ExpectedSHA256 == "" {
				cfg.SLM.ExpectedSHA256 = slm.DefaultModelSHA256
			}
			mgr = slm.New(slm.Config{
				DataDir:        dataDir,
				ModelURL:       cfg.SLM.ModelURL,
				ExpectedSHA256: cfg.SLM.ExpectedSHA256,
			}, logger)
		}
		if mgr.Status() == slm.StatusReady {
			mf := mgr.Manifest()
			fmt.Printf("already set up: %s (%s)\n", mf.FilePath, humanBytes(mf.Size))
			return 0
		}
		fmt.Printf("downloading %s\n", cfg.SLM.ModelURL)
		var start = time.Now()
		err := mgr.Setup(context.Background(), func(downloaded, total int64) {
			elapsed := time.Since(start).Seconds()
			speed := float64(downloaded) / elapsed
			if total > 0 {
				pct := float64(downloaded) / float64(total) * 100
				fmt.Printf("\r  %5.1f%%  %s / %s  (%s/s)   ",
					pct, humanBytes(downloaded), humanBytes(total), humanBytes(int64(speed)))
			} else {
				fmt.Printf("\r  %s  (%s/s)   ", humanBytes(downloaded), humanBytes(int64(speed)))
			}
		})
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Println("verifying... done")
		fmt.Printf("stored at: %s\n", dataDir)
		fmt.Println("")
		fmt.Println("To enable, add to your config:")
		fmt.Println("  [slm]")
		fmt.Println("  enabled = true")
		return 0

	case "status":
		backend := cfg.SLM.Backend
		if backend == "" {
			backend = "auto"
		}
		fmt.Printf("slm enabled: %v\n", cfg.SLM.Enabled)
		fmt.Printf("slm backend: %s\n", backend)
		if cfg.SLM.Model != "" {
			fmt.Printf("  model:   %s\n", cfg.SLM.Model)
		}
		if cfg.SLM.BaseURL != "" {
			fmt.Printf("  url:     %s\n", cfg.SLM.BaseURL)
		}

		// Backend-specific reachability / setup state.
		switch slm.Backend(backend) {
		case slm.BackendLlamafile, slm.BackendAuto:
			status := mgr.Status()
			fmt.Printf("\nllamafile status: %s\n", status)
			if mf := mgr.Manifest(); mf != nil {
				fmt.Printf("  file:    %s\n", mf.FilePath)
				fmt.Printf("  size:    %s\n", humanBytes(mf.Size))
				fmt.Printf("  sha256:  %s\n", mf.SHA256[:16]+"...")
				fmt.Printf("  setup:   %s\n", mf.SetupAt.Format("2006-01-02 15:04 UTC"))
			}
			switch status {
			case slm.StatusNotSetUp:
				fmt.Println("  run: gnoma slm setup")
			case slm.StatusMissing:
				fmt.Println("  file is missing; run: gnoma slm setup")
			}
		}

		// Live probe: try to actually boot the configured backend.
		fmt.Println("\nlive probe:")
		bcfg := slm.BackendConfig{
			Backend:        slm.Backend(backend),
			Model:          cfg.SLM.Model,
			BaseURL:        cfg.SLM.BaseURL,
			ModelURL:       cfg.SLM.ModelURL,
			DataDir:        dataDir,
			StartupTimeout: cfg.SLM.StartupTimeout.Duration(),
		}
		probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		boot, perr := slm.StartBackend(probeCtx, bcfg, logger)
		switch {
		case perr != nil:
			fmt.Printf("  ✗ %v\n", perr)
		case boot == nil:
			fmt.Println("  - no backend available (heuristic classifier will be used)")
		default:
			tools := "no tools"
			if boot.ToolSupport {
				tools = "tools"
			}
			fmt.Printf("  ✓ %s ready (model=%s, %s, boot=%s)\n",
				boot.Backend, boot.Model, tools, boot.BootTime.Round(time.Millisecond))
			_ = boot.Close()
		}
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown slm command: %s\n", args[0])
		return 1
	}
}

// humanBytes formats a byte count as a human-readable string.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n2 := n / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
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
