package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	gnomacfg "somegit.dev/Owlibou/gnoma/internal/config"
	"somegit.dev/Owlibou/gnoma/internal/router"
)

// runRouterCommand handles `gnoma router <subcommand>`. Returns an exit code.
func runRouterCommand(args []string, profile gnomacfg.Profile) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: gnoma router <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  stats   show quality scores + classifier telemetry")
		return 1
	}
	switch args[0] {
	case "stats":
		return runRouterStats(profile)
	default:
		fmt.Fprintf(os.Stderr, "unknown router command: %s\n", args[0])
		return 1
	}
}

func runRouterStats(profile gnomacfg.Profile) int {
	path := profile.QualityFile(gnomacfg.GlobalConfigDir())
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no router data yet — run some prompts first.")
			fmt.Printf("(expected at: %s)\n", path)
			return 0
		}
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", path, err)
		return 1
	}

	var snap router.QualitySnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse %s: %v\n", path, err)
		return 1
	}

	if profile.Active {
		fmt.Printf("Profile: %s\n\n", profile.Name)
	}
	printArmTable(snap)
	fmt.Println()
	printClassifierTable(snap)
	return 0
}

func printArmTable(snap router.QualitySnapshot) {
	fmt.Println("Arm × Task quality (success EMA / observation count):")
	if len(snap.Scores) == 0 {
		fmt.Println("  (no outcomes recorded yet)")
		return
	}

	armIDs := make([]string, 0, len(snap.Scores))
	for arm := range snap.Scores {
		armIDs = append(armIDs, arm)
	}
	sort.Strings(armIDs)

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  ARM\tTASK\tEMA\tCOUNT")
	for _, arm := range armIDs {
		tasks := snap.Scores[arm]
		taskNames := make([]string, 0, len(tasks))
		for tn := range tasks {
			taskNames = append(taskNames, tn)
		}
		sort.Strings(taskNames)
		for _, tn := range taskNames {
			s := tasks[tn]
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%.2f\t%d\n", arm, tn, s.Value, s.Count)
		}
	}
	_ = tw.Flush()
}

func printClassifierTable(snap router.QualitySnapshot) {
	fmt.Println("Classifier source breakdown:")
	counts := snap.ClassifierCounts
	if len(counts) == 0 {
		fmt.Println("  (no classifier telemetry recorded yet)")
		return
	}

	total := 0
	for _, n := range counts {
		total += n
	}

	// Print in a stable order: heuristic, slm, slm_fallback, then anything else.
	order := []string{"slm", "slm_fallback", "heuristic"}
	seen := make(map[string]bool, len(order))

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  SOURCE\tCOUNT\tSHARE")
	for _, src := range order {
		if n, ok := counts[src]; ok {
			share := 0.0
			if total > 0 {
				share = float64(n) / float64(total) * 100
			}
			_, _ = fmt.Fprintf(tw, "  %s\t%d\t%.1f%%\n", src, n, share)
			seen[src] = true
		}
	}
	for src, n := range counts {
		if seen[src] {
			continue
		}
		share := float64(n) / float64(total) * 100
		_, _ = fmt.Fprintf(tw, "  %s\t%d\t%.1f%%\n", src, n, share)
	}
	_ = tw.Flush()
	fmt.Printf("  total observations: %d\n", total)

	// Phase-4 trust hint.
	slmShare := 0.0
	if total > 0 {
		slmShare = float64(counts["slm"]) / float64(total) * 100
	}
	switch {
	case total < 50:
		fmt.Println("  hint: < 50 observations — too sparse for Phase 4 trust signal yet.")
	case counts["slm"] == 0:
		fmt.Println("  hint: SLM has never classified — check that llamafile boots before short-lived runs end.")
	case slmShare < 50:
		fmt.Printf("  hint: SLM share is %.0f%% — fallback is doing most of the work.\n", slmShare)
	}
}
