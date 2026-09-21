package arena

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const nanoUSDPerUSD int64 = 1_000_000_000

type TerminationMode string

const (
	TerminationBounded      TerminationMode = "bounded"
	TerminationLastSurvivor TerminationMode = "last_survivor"
)

// Config is deliberately self-contained so a run can archive the exact rules
// and pricing assumptions used to produce its result.
type Config struct {
	RunName           string          `json:"run_name"`
	TerminationMode   TerminationMode `json:"termination_mode"`
	Seed              int64           `json:"seed"`
	Width             int             `json:"width"`
	Height            int             `json:"height"`
	MaxVirtualMS      int64           `json:"max_virtual_ms"`
	MaxActions        int             `json:"max_actions"`
	ActionCooldownMS  int64           `json:"action_cooldown_ms"`
	HungerTickMS      int64           `json:"hunger_tick_ms"`
	FoodSpawnMS       int64           `json:"food_spawn_ms"`
	StartingHP        int             `json:"starting_hp"`
	StartingHunger    int             `json:"starting_hunger"`
	StartingEnergy    int             `json:"starting_energy"`
	StartingBudgetUSD float64         `json:"starting_budget_usd"`
	Pricing           Pricing         `json:"pricing"`
	Agents            []AgentConfig   `json:"agents"`
}

type Pricing struct {
	InputUSDPerMillion  float64 `json:"input_usd_per_million"`
	OutputUSDPerMillion float64 `json:"output_usd_per_million"`
}

type AgentConfig struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Model             string `json:"model,omitempty"`
	BaseURL           string `json:"base_url,omitempty"`
	APIKeyEnv         string `json:"api_key_env,omitempty"`
	TimeoutMS         int64  `json:"timeout_ms,omitempty"`
	MaxOutputTokens   int    `json:"max_output_tokens,omitempty"`
	DecisionLatencyMS int64  `json:"decision_latency_ms,omitempty"`
}

func (c *Config) ApplyDefaults() {
	if c.RunName == "" {
		c.RunName = "survivalbench"
	}
	if c.Seed == 0 {
		c.Seed = 1
	}
	if c.Width == 0 {
		c.Width = 10
	}
	if c.Height == 0 {
		c.Height = 10
	}
	if c.TerminationMode == "" {
		c.TerminationMode = TerminationBounded
	}
	if c.TerminationMode == TerminationBounded {
		if c.MaxVirtualMS == 0 {
			c.MaxVirtualMS = 120_000
		}
		if c.MaxActions == 0 {
			c.MaxActions = 200
		}
	}
	if c.ActionCooldownMS == 0 {
		c.ActionCooldownMS = 500
	}
	if c.HungerTickMS == 0 {
		c.HungerTickMS = 5_000
	}
	if c.FoodSpawnMS == 0 {
		c.FoodSpawnMS = 3_000
	}
	if c.StartingHP == 0 {
		c.StartingHP = 10
	}
	if c.StartingHunger == 0 {
		c.StartingHunger = 10
	}
	if c.StartingEnergy == 0 {
		c.StartingEnergy = 10
	}
	if c.StartingBudgetUSD == 0 {
		c.StartingBudgetUSD = 1
	}
	for i := range c.Agents {
		if c.Agents[i].TimeoutMS == 0 {
			c.Agents[i].TimeoutMS = 30_000
		}
		if c.Agents[i].MaxOutputTokens == 0 && c.Agents[i].Kind == "openai" {
			c.Agents[i].MaxOutputTokens = 512
		}
		if c.Agents[i].DecisionLatencyMS == 0 && c.Agents[i].Kind != "openai" {
			c.Agents[i].DecisionLatencyMS = 20
		}
	}
}

func (c Config) Validate() error {
	if c.Width < 5 || c.Height < 5 {
		return fmt.Errorf("map must be at least 5x5")
	}
	switch c.TerminationMode {
	case TerminationBounded:
		if c.MaxVirtualMS <= 0 || c.MaxActions <= 0 {
			return fmt.Errorf("bounded matches need positive max_virtual_ms and max_actions")
		}
	case TerminationLastSurvivor:
		// Safety limits are deliberately ignored. The caller can still cancel the context.
	default:
		return fmt.Errorf("unsupported termination_mode %q", c.TerminationMode)
	}
	if c.ActionCooldownMS <= 0 || c.HungerTickMS <= 0 || c.FoodSpawnMS <= 0 {
		return fmt.Errorf("game intervals must be positive")
	}
	if c.StartingHP <= 0 || c.StartingHunger <= 0 || c.StartingEnergy <= 0 || c.StartingBudgetUSD <= 0 {
		return fmt.Errorf("starting resources must be positive")
	}
	if len(c.Agents) < 2 {
		return fmt.Errorf("at least two agents are required")
	}
	seen := make(map[string]bool)
	for _, a := range c.Agents {
		if strings.TrimSpace(a.ID) == "" {
			return fmt.Errorf("agent id cannot be empty")
		}
		if seen[a.ID] {
			return fmt.Errorf("duplicate agent id %q", a.ID)
		}
		seen[a.ID] = true
		switch a.Kind {
		case "greedy", "random", "wait":
			if a.DecisionLatencyMS <= 0 {
				return fmt.Errorf("agent %q needs a positive decision_latency_ms", a.ID)
			}
		case "openai":
			if a.Model == "" || a.BaseURL == "" || a.APIKeyEnv == "" {
				return fmt.Errorf("openai agent %q needs model, base_url, and api_key_env", a.ID)
			}
		default:
			return fmt.Errorf("agent %q has unsupported kind %q", a.ID, a.Kind)
		}
	}
	if c.Pricing.InputUSDPerMillion < 0 || c.Pricing.OutputUSDPerMillion < 0 {
		return fmt.Errorf("pricing cannot be negative")
	}
	return nil
}

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Player struct {
	ID                string `json:"id"`
	Pos               Point  `json:"pos"`
	HP                int    `json:"hp"`
	Hunger            int    `json:"hunger"`
	Energy            int    `json:"energy"`
	BudgetNanoUSD     int64  `json:"budget_nano_usd"`
	Alive             bool   `json:"alive"`
	Defending         bool   `json:"defending"`
	DeathCause        string `json:"death_cause,omitempty"`
	SurvivalMS        int64  `json:"survival_ms"`
	Decisions         int    `json:"decisions"`
	SuccessfulActions int    `json:"successful_actions"`
	TotalLatencyMS    int64  `json:"total_latency_ms"`
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	SpentNanoUSD      int64  `json:"spent_nano_usd"`
}

func (p Player) BudgetUSD() float64 { return float64(p.BudgetNanoUSD) / float64(nanoUSDPerUSD) }
func (p Player) SpentUSD() float64  { return float64(p.SpentNanoUSD) / float64(nanoUSDPerUSD) }

type CellView struct {
	Terrain string   `json:"terrain"`
	Food    bool     `json:"food"`
	Players []string `json:"players,omitempty"`
}

type Observation struct {
	VirtualMS    int64             `json:"virtual_ms"`
	Self         Player            `json:"self"`
	Players      []Player          `json:"players"`
	MapRows      []string          `json:"map_rows"`
	Food         []Point           `json:"food"`
	Legend       map[string]string `json:"legend"`
	LegalActions []string          `json:"legal_actions"`
}

type Decision struct {
	Action string `json:"action"`
	Raw    string `json:"raw,omitempty"`
}

type Receipt struct {
	LatencyMS    int64  `json:"latency_ms"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ProviderID   string `json:"provider_id,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type Adapter interface {
	Decide(context.Context, Observation) (Decision, Receipt, error)
}

type LogEvent struct {
	Sequence  int64           `json:"sequence"`
	VirtualMS int64           `json:"virtual_ms"`
	Type      string          `json:"type"`
	ActorID   string          `json:"actor_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// EventObserver receives each event after it has been persisted to JSONL.
// Observers must return quickly; the TUI forwards events into a buffered channel.
type EventObserver func(LogEvent)

type Snapshot struct {
	VirtualMS int64    `json:"virtual_ms"`
	MapRows   []string `json:"map_rows"`
	Players   []Player `json:"players"`
	MaxHP     int      `json:"max_hp"`
	MaxHunger int      `json:"max_hunger"`
	MaxEnergy int      `json:"max_energy"`
}

type Standing struct {
	Rank              int     `json:"rank"`
	AgentID           string  `json:"agent_id"`
	Alive             bool    `json:"alive"`
	SurvivalMS        int64   `json:"survival_ms"`
	DeathCause        string  `json:"death_cause,omitempty"`
	HP                int     `json:"hp"`
	Hunger            int     `json:"hunger"`
	BudgetUSD         float64 `json:"budget_usd"`
	SpentUSD          float64 `json:"spent_usd"`
	Decisions         int     `json:"decisions"`
	SuccessfulActions int     `json:"successful_actions"`
	MeanLatencyMS     float64 `json:"mean_latency_ms"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
}

type Summary struct {
	RunName        string     `json:"run_name"`
	Seed           int64      `json:"seed"`
	Winner         string     `json:"winner"`
	StopReason     string     `json:"stop_reason"`
	VirtualMS      int64      `json:"virtual_ms"`
	ActionsApplied int        `json:"actions_applied"`
	Standings      []Standing `json:"standings"`
}

func USDToNano(v float64) int64 { return int64(math.Round(v * float64(nanoUSDPerUSD))) }

func CostNanoUSD(pricing Pricing, receipt Receipt) int64 {
	input := float64(receipt.InputTokens) * pricing.InputUSDPerMillion * 1000
	output := float64(receipt.OutputTokens) * pricing.OutputUSDPerMillion * 1000
	return int64(math.Round(input + output))
}
