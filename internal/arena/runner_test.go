package arena

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type fixedAdapter struct {
	action  string
	receipt Receipt
}

func (a fixedAdapter) Decide(context.Context, Observation) (Decision, Receipt, error) {
	return Decision{Action: a.action}, a.receipt, nil
}

func testConfig() Config {
	return Config{
		RunName:           "test",
		Seed:              42,
		Width:             8,
		Height:            8,
		MaxVirtualMS:      20_000,
		MaxActions:        30,
		ActionCooldownMS:  500,
		HungerTickMS:      2_000,
		FoodSpawnMS:       1_500,
		StartingHP:        5,
		StartingHunger:    5,
		StartingEnergy:    5,
		StartingBudgetUSD: 1,
		Pricing:           Pricing{InputUSDPerMillion: 1, OutputUSDPerMillion: 4},
		Agents: []AgentConfig{
			{ID: "a", Kind: "greedy", DecisionLatencyMS: 30},
			{ID: "b", Kind: "random", DecisionLatencyMS: 90},
		},
	}
}

func TestRunIsDeterministicWithFixedAdapters(t *testing.T) {
	config := testConfig()
	makeAdapters := func() map[string]Adapter {
		adapters, err := BuildAdapters(config)
		if err != nil {
			t.Fatal(err)
		}
		return adapters
	}
	var firstLog, secondLog bytes.Buffer
	first, err := Run(context.Background(), config, makeAdapters(), &firstLog)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), config, makeAdapters(), &secondLog)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("summaries differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if firstLog.String() != secondLog.String() {
		t.Fatal("event logs differ for identical seed and adapters")
	}
}

func TestBudgetExhaustionKillsBeforeAction(t *testing.T) {
	config := testConfig()
	config.StartingBudgetUSD = 0.001
	config.MaxActions = 5
	adapters := map[string]Adapter{
		"a": fixedAdapter{action: "wait", receipt: Receipt{LatencyMS: 10, InputTokens: 2_000}},
		"b": fixedAdapter{action: "wait", receipt: Receipt{LatencyMS: 1_000}},
	}
	var log bytes.Buffer
	summary, err := Run(context.Background(), config, adapters, &log)
	if err != nil {
		t.Fatal(err)
	}
	var agentA Standing
	for _, standing := range summary.Standings {
		if standing.AgentID == "a" {
			agentA = standing
		}
	}
	if agentA.Alive || agentA.DeathCause != "budget_exhausted" {
		t.Fatalf("expected economic death, got %#v", agentA)
	}
	if agentA.SuccessfulActions != 0 {
		t.Fatalf("bankrupt agent applied %d actions", agentA.SuccessfulActions)
	}
	if !strings.Contains(log.String(), `"type":"agent_died"`) {
		t.Fatal("event log does not contain death event")
	}
}

func TestOpenAIAdapterUsesCompatibleChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("unexpected authorization header %q", got)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "test-model" || len(request.Messages) != 2 {
			t.Errorf("unexpected request: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "id":"response-1",
          "choices":[{"message":{"role":"assistant","content":"{\"action\":\"move_north\"}"}}],
          "usage":{"prompt_tokens":123,"completion_tokens":9}
        }`))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter(server.URL, "test-key", "test-model", 0)
	decision, receipt, err := adapter.Decide(context.Background(), Observation{
		Self: Player{ID: "a", Alive: true}, LegalActions: legalActions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "move_north" {
		t.Fatalf("unexpected decision %#v", decision)
	}
	if receipt.InputTokens != 123 || receipt.OutputTokens != 9 || receipt.ProviderID != "response-1" {
		t.Fatalf("unexpected receipt %#v", receipt)
	}
	if receipt.LatencyMS < 1 {
		t.Fatalf("invalid measured latency %d", receipt.LatencyMS)
	}
}
