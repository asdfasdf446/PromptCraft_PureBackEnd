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
)

func main() {
	configPath := flag.String("config", "configs/local-smoke.json", "path to an arena JSON configuration")
	outputPath := flag.String("output", "", "run output directory (default: runs/<timestamp>-<run-name>)")
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	summary, runErr := arena.Run(ctx, config, adapters, eventFile)
	closeErr := eventFile.Close()
	if runErr != nil {
		log.Fatalf("run arena: %v", runErr)
	}
	if closeErr != nil {
		log.Fatalf("close event log: %v", closeErr)
	}
	summaryJSON, err := arena.StableSummaryJSON(summary)
	if err != nil {
		log.Fatalf("encode summary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "summary.json"), append(summaryJSON, '\n'), 0o644); err != nil {
		log.Fatalf("write summary: %v", err)
	}

	fmt.Printf("Run: %s  Winner: %s  Stop: %s  Virtual time: %d ms\n", summary.RunName, summary.Winner, summary.StopReason, summary.VirtualMS)
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
