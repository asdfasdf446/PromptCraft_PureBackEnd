# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
go build -o game ./cmd/game/   # Build the binary
go run ./cmd/game/             # Build and run
go test ./...                  # Run all tests
```

## Architecture

This is a CLI text adventure game using a **client-server architecture over WebSocket** — the server runs the game engine, the client is a Bubble Tea TUI. Both run in the same process (the binary starts the server on a random port, then the TUI connects to it).

### Package layout

```
cmd/game/          Entry point. Starts WebSocket server on localhost:0, then launches the TUI client.
internal/engine/   Game engine. Owns all mutable world state (30×30 grid, players, logs). All game logic
                   lives here — ProcessCommand dispatches player input and mutates state.
internal/network/  WebSocket server. Accepts connections, deserializes JSON Command messages,
                   calls engine.ProcessCommand, then broadcasts the updated WorldState to all clients.
internal/tui/      Bubble Tea TUI client. Connects to the local WebSocket server, renders the map/logs/input,
                   and sends commands typed by the user. Receives state broadcasts as tea.Msg.
pkg/models/        Shared data types (Point, Player, Cell, WorldState, Command) used by all packages.
```

### Data flow

1. User types a command in the TUI input (e.g. `move north`)
2. TUI marshals it as `{"type":"command","payload":"move north"}` and sends over WebSocket
3. Server unmarshals, calls `engine.ProcessCommand(payload)`, which mutates game state
4. Server calls `engine.GetState()` and broadcasts the full `WorldState` (JSON) to all connected clients
5. TUI receives the state broadcast, re-renders the map and logs

### Key details

- All state is in `engine.GameEngine`, protected by `sync.RWMutex` — reads use `RLock`, writes use `Lock`
- The 30×30 map is a fixed-size 2D slice of `Cell` structs, each holding terrain text and entity names
- Only one player exists currently (hardcoded ID `"p1"` at position 5,5)
- World state is broadcast to ALL clients after every command (not just the sender)
- Log buffer is capped at 100 entries
- The TUI uses `tea.WindowSizeMsg` for responsive layout, dividing the terminal into map area, log panel, and input bar
