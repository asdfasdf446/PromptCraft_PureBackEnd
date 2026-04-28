package main

import (
	"log"
	"net"
	"net/http"
	"promptcraft/internal/engine"
	"promptcraft/internal/network"
	"promptcraft/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// 1. Initialize Engine
	gameEngine := engine.NewGameEngine()

	// 2. Find an available port and start WebSocket Server
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("Failed to find an available port: %v", err)
	}
	addr := ln.Addr().String()

	server := network.NewServer(gameEngine)
	http.HandleFunc("/ws", server.HandleConnections)

	go func() {
		// No need to print "Server starting on..." to stdout as TUI will take over
		if err := http.Serve(ln, nil); err != nil {
			// In a real app, we'd handle this more gracefully
		}
	}()

	// 3. Start TUI Client
	p := tea.NewProgram(tui.NewModel(addr), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("TUI Error: %v", err)
	}
}
