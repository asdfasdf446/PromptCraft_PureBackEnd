package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"promptcraft/internal/arena"
	"promptcraft/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

type runOutcome struct {
	summary arena.Summary
	err     error
}

func main() {
	configPath := flag.String("config", "configs/local-smoke.json", "path to an arena JSON configuration")
	outputPath := flag.String("output", "", "run output directory (default: runs/<timestamp>-<run-name>)")
	headless := flag.Bool("headless", false, "disable the live spectator TUI")
	flag.Parse()

	configData, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var config arena.Config
	if err := json.Unmarshal(configData, &config); err != nil {
		log.Fatalf("decode config: %v", err)
	}
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	adapters, err := arena.BuildAdapters(config)
	if err != nil {
		log.Fatalf("build model adapters: %v", err)
	}
	runDir := *outputPath
	if runDir == "" {
		runDir = filepath.Join("runs", time.Now().Format("20060102-150405")+"-"+safeName(config.RunName))
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		log.Fatalf("create run directory: %v", err)
	}
	normalizedConfig, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Fatalf("encode normalized config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "config.json"), append(normalizedConfig, '\n'), 0o644); err != nil {
		log.Fatalf("write normalized config: %v", err)
	}
	eventFile, err := os.Create(filepath.Join(runDir, "events.jsonl"))
	if err != nil {
		log.Fatalf("create event log: %v", err)
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalContext)
	defer cancel()

	var outcome runOutcome
	if *headless {
		outcome.summary, outcome.err = arena.Run(ctx, config, adapters, eventFile)
		if closeErr := eventFile.Close(); outcome.err == nil {
			outcome.err = closeErr
		}
	} else {
		events := make(chan arena.LogEvent, 512)
		done := make(chan struct{})
		go func() {
			outcome.summary, outcome.err = arena.RunWithObserver(ctx, config, adapters, eventFile, func(event arena.LogEvent) {
				select {
				case events <- event:
				case <-ctx.Done():
				}
			})
			if closeErr := eventFile.Close(); outcome.err == nil {
				outcome.err = closeErr
			}
			close(events)
			close(done)
		}()
		program := tea.NewProgram(tui.NewModel(config, events, cancel), tea.WithAltScreen())
		_, tuiErr := program.Run()
		cancel()
		<-done
		if tuiErr != nil {
			log.Fatalf("run spectator TUI: %v", tuiErr)
		}
	}
	if outcome.err != nil {
		log.Fatalf("run arena: %v", outcome.err)
	}
	summary := outcome.summary
	summaryJSON, err := arena.StableSummaryJSON(summary)
	if err != nil {
		log.Fatalf("encode summary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "summary.json"), append(summaryJSON, '\n'), 0o644); err != nil {
		log.Fatalf("write summary: %v", err)
	}

	winner := summary.Winner
	if winner == "" {
		winner = "none"
	}
	fmt.Printf("Run: %s  Winner: %s  Stop: %s  Virtual time: %d ms\n", summary.RunName, winner, summary.StopReason, summary.VirtualMS)
	fmt.Println("RANK  AGENT                 ALIVE  SURVIVAL  BUDGET($)  SPENT($)   MEAN LATENCY")
	for _, standing := range summary.Standings {
		fmt.Printf("%-5d %-21s %-6t %-9d %-10.6f %-10.6f %.1f ms\n",
			standing.Rank, standing.AgentID, standing.Alive, standing.SurvivalMS,
			standing.BudgetUSD, standing.SpentUSD, standing.MeanLatencyMS)
	}
	fmt.Printf("Artifacts: %s\n", runDir)
}

func safeName(value string) string {
	value = strings.ToLower(value)
	var result strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			result.WriteRune(r)
		case r == '-', r == '_':
			result.WriteRune(r)
		case unicode.IsSpace(r):
			result.WriteByte('-')
		}
	}
	if result.Len() == 0 {
		return "arena"
	}
	return result.String()
}
