package arena

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

var legalActions = []string{
	"move_north", "move_south", "move_east", "move_west",
	"eat_here", "eat_north", "eat_south", "eat_east", "eat_west",
	"attack_north", "attack_south", "attack_east", "attack_west",
	"defend", "wait",
}

var legalActionSet = func() map[string]bool {
	result := make(map[string]bool, len(legalActions))
	for _, action := range legalActions {
		result[action] = true
	}
	return result
}()

func BuildAdapters(config Config) (map[string]Adapter, error) {
	result := make(map[string]Adapter, len(config.Agents))
	for i, agent := range config.Agents {
		switch agent.Kind {
		case "greedy":
			result[agent.ID] = &GreedyAdapter{latencyMS: agent.DecisionLatencyMS}
		case "random":
			result[agent.ID] = &RandomAdapter{
				rng:       rand.New(rand.NewSource(config.Seed + int64(i+1)*7919)),
				latencyMS: agent.DecisionLatencyMS,
			}
		case "wait":
			result[agent.ID] = fixedActionAdapter{action: "wait", latencyMS: agent.DecisionLatencyMS}
		case "openai":
			key := os.Getenv(agent.APIKeyEnv)
			if key == "" {
				return nil, fmt.Errorf("agent %q: environment variable %s is empty", agent.ID, agent.APIKeyEnv)
			}
			adapter := NewOpenAIAdapter(agent.BaseURL, key, agent.Model, time.Duration(agent.TimeoutMS)*time.Millisecond)
			adapter.maxTokens = agent.MaxOutputTokens
			result[agent.ID] = adapter
		default:
			return nil, fmt.Errorf("agent %q: unsupported adapter %q", agent.ID, agent.Kind)
		}
	}
	return result, nil
}

type GreedyAdapter struct{ latencyMS int64 }

type fixedActionAdapter struct {
	action    string
	latencyMS int64
}

func (a fixedActionAdapter) Decide(_ context.Context, _ Observation) (Decision, Receipt, error) {
	return Decision{Action: a.action}, Receipt{LatencyMS: a.latencyMS}, nil
}

func (a *GreedyAdapter) Decide(_ context.Context, obs Observation) (Decision, Receipt, error) {
	action := greedyAction(obs)
	return Decision{Action: action}, Receipt{LatencyMS: a.latencyMS}, nil
}

func greedyAction(obs Observation) string {
	x, y := obs.Self.Pos.X, obs.Self.Pos.Y
	if foodAt(obs, x, y) {
		return "eat_here"
	}
	directions := []struct {
		name   string
		dx, dy int
	}{{"north", 0, -1}, {"south", 0, 1}, {"east", 1, 0}, {"west", -1, 0}}
	for _, d := range directions {
		if otherAliveAt(obs, x+d.dx, y+d.dy) {
			return "attack_" + d.name
		}
	}
	for _, d := range directions {
		if foodAt(obs, x+d.dx, y+d.dy) {
			return "eat_" + d.name
		}
	}
	type candidate struct {
		action   string
		distance int
	}
	best := candidate{action: "wait", distance: 1 << 30}
	for _, d := range directions {
		nx, ny := x+d.dx, y+d.dy
		cell := cellAt(obs.MapRows, nx, ny)
		if cell == 0 || cell == '#' || cell == 'A' {
			continue
		}
		distance := nearestFoodDistance(obs.Food, nx, ny)
		if distance < best.distance {
			best = candidate{action: "move_" + d.name, distance: distance}
		}
	}
	if best.action != "wait" {
		return best.action
	}
	return "defend"
}

func nearestFoodDistance(food []Point, x, y int) int {
	best := 1 << 30
	for _, pos := range food {
		distance := abs(x-pos.X) + abs(y-pos.Y)
		if distance < best {
			best = distance
		}
	}
	return best
}

func foodAt(obs Observation, x, y int) bool {
	for _, food := range obs.Food {
		if food.X == x && food.Y == y {
			return true
		}
	}
	return false
}

func otherAliveAt(obs Observation, x, y int) bool {
	for _, p := range obs.Players {
		if p.ID != obs.Self.ID && p.Alive && p.Pos.X == x && p.Pos.Y == y {
			return true
		}
	}
	return false
}

func cellAt(rows []string, x, y int) byte {
	if y < 0 || y >= len(rows) || x < 0 || x >= len(rows[y]) {
		return 0
	}
	return rows[y][x]
}

type RandomAdapter struct {
	rng       *rand.Rand
	latencyMS int64
}

func (a *RandomAdapter) Decide(_ context.Context, _ Observation) (Decision, Receipt, error) {
	return Decision{Action: legalActions[a.rng.Intn(len(legalActions))]}, Receipt{LatencyMS: a.latencyMS}, nil
}

type OpenAIAdapter struct {
	baseURL   string
	apiKey    string
	model     string
	client    *http.Client
	maxTokens int
}

func NewOpenAIAdapter(baseURL, apiKey, model string, timeout time.Duration) *OpenAIAdapter {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAIAdapter{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		model:     model,
		client:    &http.Client{Timeout: timeout},
		maxTokens: 512,
	}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (a *OpenAIAdapter) Decide(ctx context.Context, obs Observation) (Decision, Receipt, error) {
	observationJSON, err := json.Marshal(obs)
	if err != nil {
		return Decision{}, Receipt{}, fmt.Errorf("encode observation: %w", err)
	}
	payload := chatRequest{
		Model: a.model,
		Messages: []chatMessage{
			{
				Role:    "system",
				Content: "You control one agent in a survival arena. Stay alive, obtain food, conserve budget, and attack when useful. Return one JSON object only, exactly like {\"action\":\"move_north\"}. The action must be one of legal_actions. Coordinates use x rightward and y downward.",
			},
			{Role: "user", Content: string(observationJSON)},
		},
		Temperature: 0,
		MaxTokens:   a.maxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Decision{}, Receipt{}, fmt.Errorf("encode chat request: %w", err)
	}
	endpoint := a.baseURL + "/v1/chat/completions"
	if strings.HasSuffix(a.baseURL, "/v1") {
		endpoint = a.baseURL + "/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Decision{}, Receipt{}, fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	started := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		receipt := Receipt{LatencyMS: max(int64(1), time.Since(started).Milliseconds())}
		return Decision{}, receipt, fmt.Errorf("chat completion: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	receipt := Receipt{LatencyMS: max(int64(1), time.Since(started).Milliseconds())}
	if err != nil {
		return Decision{}, receipt, fmt.Errorf("read chat response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Decision{}, receipt, fmt.Errorf("chat completion returned %s: %s", resp.Status, truncate(string(responseBody), 300))
	}
	var decoded chatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Decision{}, receipt, fmt.Errorf("decode chat response: %w", err)
	}
	receipt.InputTokens = decoded.Usage.PromptTokens
	receipt.OutputTokens = decoded.Usage.CompletionTokens
	receipt.ProviderID = decoded.ID
	if len(decoded.Choices) == 0 {
		return Decision{}, receipt, fmt.Errorf("chat response has no choices")
	}
	receipt.FinishReason = decoded.Choices[0].FinishReason
	raw := strings.TrimSpace(decoded.Choices[0].Message.Content)
	decision, err := parseDecision(raw)
	decision.Raw = truncate(raw, 300)
	if err != nil {
		return decision, receipt, err
	}
	return decision, receipt, nil
}

func parseDecision(raw string) (Decision, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	start, end := strings.Index(cleaned, "{"), strings.LastIndex(cleaned, "}")
	if start < 0 || end < start {
		return Decision{Action: "wait"}, fmt.Errorf("model output is not a JSON object")
	}
	var decision Decision
	if err := json.Unmarshal([]byte(cleaned[start:end+1]), &decision); err != nil {
		return Decision{Action: "wait"}, fmt.Errorf("decode model decision: %w", err)
	}
	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	if !legalActionSet[decision.Action] {
		return Decision{Action: "wait"}, fmt.Errorf("model returned invalid action %q", decision.Action)
	}
	return decision, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
