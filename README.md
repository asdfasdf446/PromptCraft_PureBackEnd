# SurvivalBench

SurvivalBench is a text/API-first arena for comparing decision models under
three constraints at once: survival, inference cost, and decision latency.
The simulation is deterministic for local adapters and uses virtual-time
events, so a slower model completes actions later and receives fewer turns.

## Run locally

```bash
go run ./cmd/game -config configs/local-smoke.json
```

The command opens a live spectator TUI with the map, agent vitals, economic
budget, measured thinking time, actions, and deaths. Press `q`, `Esc`, or
`Ctrl+C` to leave. After a match ends, the final frame remains visible until
you quit.

For CI, scripts, or plain logs, disable the spectator:

```bash
go run ./cmd/game -headless -config configs/local-smoke.json
```

## Last-survivor deathmatch

`last_survivor` mode ignores `max_actions` and `max_virtual_ms`. It ends only
when one agent remains alive, all agents die on the same world tick, or the
operator cancels the run.

```bash
# Instant local validation
go run ./cmd/game -config configs/local-deathmatch.json

# Live LLM versus the starvation sanity baseline
go run ./cmd/game -config configs/deepseek-deathmatch.json
```

A bounded cutoff is now a no-contest: its summary has no winner. Only
`last_agent_alive` produces a winner.

Each run writes a normalized `config.json`, an append-only `events.jsonl`, and
a `summary.json` leaderboard under `runs/`.

## Run the OpenAI-compatible adapter

Keep credentials out of config files and shell history:

```bash
read -s SURVIVALBENCH_API_KEY
export SURVIVALBENCH_API_KEY
go run ./cmd/game -config configs/deepseek-smoke.json
unset SURVIVALBENCH_API_KEY
```

The sample points at `https://aigw.net-swift.com`, uses
`deepseek-v4.1-flash`, and pairs it with a non-attacking wait adapter so the
smoke test isolates protocol behavior. Its token prices are benchmark
assumptions, not a claim about the gateway invoice. Freeze the desired rates
in each experiment config.

## Core rules

- Every agent starts with independent HP and a USD budget.
- Hunger falls with virtual time; at zero hunger, HP falls.
- Food is scarce, initially placed and periodically respawned on soil.
- Actions are `move_*`, `eat_*`, `attack_*`, `defend`, and `wait`.
- Each inference receipt deducts token cost. Reaching zero budget is death.
- Measured provider latency determines when an action lands and when that
  agent may request its next decision.
- The full map is supplied in every observation as compact ASCII rows.

## Validation

```bash
go test ./...
```
