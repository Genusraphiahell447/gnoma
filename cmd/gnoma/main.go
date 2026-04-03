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
	"somegit.dev/Owlibou/gnoma/internal/provider"
	anthropicprov "somegit.dev/Owlibou/gnoma/internal/provider/anthropic"
	"somegit.dev/Owlibou/gnoma/internal/provider/mistral"
	"somegit.dev/Owlibou/gnoma/internal/stream"
	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tool/bash"
	"somegit.dev/Owlibou/gnoma/internal/tool/fs"
)

func main() {
	var (
		providerName = flag.String("provider", "mistral", "LLM provider")
		model        = flag.String("model", "", "model name (empty = provider default)")
		system       = flag.String("system", defaultSystem, "system prompt")
		apiKey       = flag.String("api-key", "", "API key (or set MISTRAL_API_KEY env)")
		maxTurns     = flag.Int("max-turns", 50, "max tool-calling rounds per turn")
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

	// Resolve API key
	key := *apiKey
	if key == "" {
		key = resolveAPIKey(*providerName)
	}
	if key == "" {
		fmt.Fprintf(os.Stderr, "error: no API key for provider %q\nSet %s environment variable or use --api-key\n",
			*providerName, envKeyFor(*providerName))
		os.Exit(1)
	}

	// Create provider
	prov, err := createProvider(*providerName, key, *model)
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

	// Re-register bash tool with aliases
	reg.Register(bash.New(bash.WithAliases(aliases)))

	// Create engine
	eng, err := engine.New(engine.Config{
		Provider: prov,
		Tools:    reg,
		System:   *system,
		Model:    *model,
		MaxTurns: *maxTurns,
		Logger:   logger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Read input
	input, err := readInput(flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if input == "" {
		fmt.Fprintln(os.Stderr, "error: no input provided")
		fmt.Fprintln(os.Stderr, "usage: echo 'prompt' | gnoma")
		fmt.Fprintln(os.Stderr, "   or: gnoma 'prompt'")
		os.Exit(1)
	}

	// Context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Callback: stream text deltas to stdout
	cb := func(evt stream.Event) {
		if evt.Type == stream.EventTextDelta && evt.Text != "" {
			fmt.Print(evt.Text)
		}
	}

	// Submit and run
	_, err = eng.Submit(ctx, input, cb)
	fmt.Println() // final newline

	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "\ninterrupted")
			os.Exit(130)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
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
	}
	for _, alt := range alternatives[providerName] {
		if key := os.Getenv(alt); key != "" {
			return key
		}
	}
	return ""
}

func createProvider(name, apiKey, model string) (provider.Provider, error) {
	cfg := provider.ProviderConfig{
		APIKey: apiKey,
		Model:  model,
	}

	switch name {
	case "mistral":
		return mistral.New(cfg)
	case "anthropic":
		return anthropicprov.New(cfg)
	default:
		return nil, fmt.Errorf("unknown provider %q (supports: mistral, anthropic)", name)
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
