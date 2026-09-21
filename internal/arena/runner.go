package arena

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

type scheduledEvent struct {
	at       int64
	sequence int64
	kind     string
	agentID  string
	decision Decision
	receipt  Receipt
	errText  string
}

type eventQueue []scheduledEvent

func (q eventQueue) Len() int { return len(q) }
func (q eventQueue) Less(i, j int) bool {
	if q[i].at != q[j].at {
		return q[i].at < q[j].at
	}
	return q[i].sequence < q[j].sequence
}
func (q eventQueue) Swap(i, j int)   { q[i], q[j] = q[j], q[i] }
func (q *eventQueue) Push(value any) { *q = append(*q, value.(scheduledEvent)) }
func (q *eventQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

type eventWriter struct {
	encoder  *json.Encoder
	sequence int64
}

func newEventWriter(w io.Writer) *eventWriter {
	if w == nil {
		w = io.Discard
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return &eventWriter{encoder: encoder}
}

func (w *eventWriter) write(at int64, eventType, actorID string, data any) error {
	w.sequence++
	var raw json.RawMessage
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		raw = encoded
	}
	return w.encoder.Encode(LogEvent{
		Sequence: w.sequence, VirtualMS: at, Type: eventType, ActorID: actorID, Data: raw,
	})
}

func Run(ctx context.Context, config Config, adapters map[string]Adapter, logOutput io.Writer) (Summary, error) {
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return Summary{}, err
	}
	for _, agent := range config.Agents {
		if adapters[agent.ID] == nil {
			return Summary{}, fmt.Errorf("missing adapter for agent %q", agent.ID)
		}
	}
	world, err := NewWorld(config)
	if err != nil {
		return Summary{}, err
	}
	log := newEventWriter(logOutput)
	if err := log.write(0, "run_started", "", map[string]any{"config": config}); err != nil {
		return Summary{}, err
	}

	queue := &eventQueue{}
	heap.Init(queue)
	var scheduleSequence int64
	schedule := func(event scheduledEvent) {
		scheduleSequence++
		event.sequence = scheduleSequence
		heap.Push(queue, event)
	}
	for _, agent := range config.Agents {
		schedule(scheduledEvent{at: 0, kind: "decide", agentID: agent.ID})
	}

	stopReason := "event_queue_empty"
	actionsApplied := 0
	currentMS := int64(0)
	for queue.Len() > 0 {
		if err := ctx.Err(); err != nil {
			stopReason = "context_cancelled"
			break
		}
		event := heap.Pop(queue).(scheduledEvent)
		if event.at > config.MaxVirtualMS {
			for _, worldEvent := range world.Advance(config.MaxVirtualMS) {
				if err := writeWorldEvent(log, config.MaxVirtualMS, worldEvent); err != nil {
					return Summary{}, err
				}
			}
			currentMS = config.MaxVirtualMS
			stopReason = "max_virtual_time"
			break
		}
		currentMS = event.at
		for _, worldEvent := range world.Advance(currentMS) {
			if err := writeWorldEvent(log, currentMS, worldEvent); err != nil {
				return Summary{}, err
			}
		}
		if world.AliveCount() <= 1 {
			stopReason = "last_agent_alive"
			break
		}
		player := world.Player(event.agentID)
		if player == nil || !player.Alive {
			continue
		}

		switch event.kind {
		case "decide":
			if err := log.write(currentMS, "decision_started", event.agentID, nil); err != nil {
				return Summary{}, err
			}
			decision, receipt, decideErr := adapters[event.agentID].Decide(ctx, world.Observation(event.agentID))
			if receipt.LatencyMS <= 0 {
				receipt.LatencyMS = 1
			}
			if decideErr != nil {
				decision.Action = "wait"
			}
			errText := ""
			if decideErr != nil {
				errText = decideErr.Error()
			}
			schedule(scheduledEvent{
				at: currentMS + receipt.LatencyMS, kind: "complete", agentID: event.agentID,
				decision: decision, receipt: receipt, errText: errText,
			})
		case "complete":
			cost := CostNanoUSD(config.Pricing, event.receipt)
			solvent, worldEvents := world.Charge(event.agentID, cost, event.receipt)
			if err := log.write(currentMS, "decision_completed", event.agentID, map[string]any{
				"action": event.decision.Action, "raw": event.decision.Raw, "latency_ms": event.receipt.LatencyMS,
				"input_tokens": event.receipt.InputTokens, "output_tokens": event.receipt.OutputTokens,
				"cost_usd": float64(cost) / float64(nanoUSDPerUSD), "remaining_budget_usd": world.Player(event.agentID).BudgetUSD(),
				"provider_id": event.receipt.ProviderID, "finish_reason": event.receipt.FinishReason, "error": event.errText,
			}); err != nil {
				return Summary{}, err
			}
			for _, worldEvent := range worldEvents {
				if err := writeWorldEvent(log, currentMS, worldEvent); err != nil {
					return Summary{}, err
				}
			}
			if !solvent {
				continue
			}
			success, message, actionEvents := world.Apply(event.agentID, event.decision.Action)
			actionsApplied++
			if err := log.write(currentMS, "action_applied", event.agentID, map[string]any{
				"action": event.decision.Action, "success": success, "message": message,
			}); err != nil {
				return Summary{}, err
			}
			for _, worldEvent := range actionEvents {
				if err := writeWorldEvent(log, currentMS, worldEvent); err != nil {
					return Summary{}, err
				}
			}
			if world.AliveCount() <= 1 {
				stopReason = "last_agent_alive"
				queue.Init()
				break
			}
			if actionsApplied >= config.MaxActions {
				stopReason = "max_actions"
				queue.Init()
				break
			}
			schedule(scheduledEvent{at: currentMS + config.ActionCooldownMS, kind: "decide", agentID: event.agentID})
		default:
			return Summary{}, fmt.Errorf("unknown scheduled event %q", event.kind)
		}
	}

	world.Finalize(currentMS)
	summary := buildSummary(config, world, stopReason, currentMS, actionsApplied)
	if err := log.write(currentMS, "run_finished", "", summary); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func writeWorldEvent(log *eventWriter, at int64, event WorldEvent) error {
	return log.write(at, event.Type, event.ActorID, event.Data)
}

func buildSummary(config Config, world *World, stopReason string, virtualMS int64, actionsApplied int) Summary {
	players := sortedPlayers(world.Players())
	standings := make([]Standing, 0, len(players))
	for index, player := range players {
		meanLatency := float64(0)
		if player.Decisions > 0 {
			meanLatency = float64(player.TotalLatencyMS) / float64(player.Decisions)
		}
		standings = append(standings, Standing{
			Rank: index + 1, AgentID: player.ID, Alive: player.Alive, SurvivalMS: player.SurvivalMS,
			DeathCause: player.DeathCause, HP: player.HP, Hunger: player.Hunger,
			BudgetUSD: player.BudgetUSD(), SpentUSD: player.SpentUSD(), Decisions: player.Decisions,
			SuccessfulActions: player.SuccessfulActions, MeanLatencyMS: meanLatency,
			InputTokens: player.InputTokens, OutputTokens: player.OutputTokens,
		})
	}
	winner := ""
	if len(standings) > 0 {
		winner = standings[0].AgentID
	}
	return Summary{
		RunName: config.RunName, Seed: config.Seed, Winner: winner, StopReason: stopReason,
		VirtualMS: virtualMS, ActionsApplied: actionsApplied, Standings: standings,
	}
}

// Init clears a queue while preserving its allocated storage.
func (q *eventQueue) Init() {
	*q = (*q)[:0]
	heap.Init(q)
}

func StableSummaryJSON(summary Summary) ([]byte, error) {
	copyOfSummary := summary
	sort.SliceStable(copyOfSummary.Standings, func(i, j int) bool {
		return copyOfSummary.Standings[i].Rank < copyOfSummary.Standings[j].Rank
	})
	return json.MarshalIndent(copyOfSummary, "", "  ")
}
