package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"promptcraft/internal/arena"
)

func TestModelRendersSnapshotAndLiveDecision(t *testing.T) {
	config := arena.Config{RunName: "ui-test", Agents: []arena.AgentConfig{{ID: "alpha"}, {ID: "beta"}}}
	model := NewModel(config, nil, nil)
	model.width, model.height = 110, 30
	snapshot := arena.Snapshot{
		VirtualMS: 1200,
		MapRows:   []string{"..F", ".#.", ":.."},
		Players: []arena.Player{
			{ID: "alpha", Pos: arena.Point{X: 0, Y: 0}, HP: 8, Hunger: 7, Energy: 6, BudgetNanoUSD: arena.USDToNano(0.9), Alive: true},
			{ID: "beta", Pos: arena.Point{X: 2, Y: 2}, HP: 5, Hunger: 4, Energy: 3, BudgetNanoUSD: arena.USDToNano(0.8), Alive: true},
		},
		MaxHP: 10, MaxHunger: 10, MaxEnergy: 10,
	}
	model.applyEvent(eventWithData(t, "state_snapshot", "", snapshot))
	model.applyEvent(eventWithData(t, "decision_started", "alpha", nil))
	model.now = time.Now().Add(time.Second)

	view := model.View()
	for _, expected := range []string{"SURVIVALBENCH", "WORLD", "AGENTS", "alpha", "beta", "THINKING", "LIVE EVENT STREAM"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view does not contain %q", expected)
		}
	}
	if _, ok := model.thinking["alpha"]; !ok {
		t.Fatal("decision_started did not mark agent as thinking")
	}
}

func TestRunFinishedStopsThinkingAndShowsWinner(t *testing.T) {
	config := arena.Config{RunName: "ui-test", Agents: []arena.AgentConfig{{ID: "alpha"}, {ID: "beta"}}}
	model := NewModel(config, nil, nil)
	model.thinking["alpha"] = time.Now()
	summary := arena.Summary{Winner: "alpha", StopReason: "last_agent_alive", VirtualMS: 4200}
	model.applyEvent(eventWithData(t, "run_finished", "", summary))
	if !model.finished || model.summary.Winner != "alpha" {
		t.Fatalf("run result was not applied: %#v", model.summary)
	}
	if len(model.thinking) != 0 {
		t.Fatal("thinking state was not cleared")
	}
}

func eventWithData(t *testing.T, eventType, actorID string, data any) arena.LogEvent {
	t.Helper()
	var raw json.RawMessage
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		raw = encoded
	}
	return arena.LogEvent{Type: eventType, ActorID: actorID, Data: raw}
}
