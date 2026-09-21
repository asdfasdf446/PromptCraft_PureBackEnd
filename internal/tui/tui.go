package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"promptcraft/internal/arena"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	green   = lipgloss.Color("#62D26F")
	cyan    = lipgloss.Color("#55DDE0")
	amber   = lipgloss.Color("#F2B84B")
	red     = lipgloss.Color("#FF5F56")
	muted   = lipgloss.Color("#6C7086")
	white   = lipgloss.Color("#E6E9EF")
	soil    = lipgloss.Color("#A67C52")
	blue    = lipgloss.Color("#5B8FF9")
	magenta = lipgloss.Color("#C678DD")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(green)
	dimStyle   = lipgloss.NewStyle().Foreground(muted)
	keyStyle   = lipgloss.NewStyle().Bold(true).Foreground(cyan)
	goodStyle  = lipgloss.NewStyle().Foreground(green)
	badStyle   = lipgloss.NewStyle().Foreground(red)
	warnStyle  = lipgloss.NewStyle().Foreground(amber)
)

var agentColors = []lipgloss.Color{blue, magenta, amber, cyan, green, red}

type eventMsg arena.LogEvent
type tickMsg time.Time
type streamClosedMsg struct{}

type Model struct {
	config    arena.Config
	events    <-chan arena.LogEvent
	cancel    context.CancelFunc
	snapshot  arena.Snapshot
	haveState bool
	thinking  map[string]time.Time
	lastMove  map[string]string
	logs      []string
	width     int
	height    int
	finished  bool
	summary   arena.Summary
	startedAt time.Time
	now       time.Time
}

func NewModel(config arena.Config, events <-chan arena.LogEvent, cancel context.CancelFunc) Model {
	now := time.Now()
	return Model{
		config: config, events: events, cancel: cancel,
		thinking: make(map[string]time.Time), lastMove: make(map[string]string),
		width: 110, height: 36, startedAt: now, now: now,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.events), nextTick())
}

func waitForEvent(events <-chan arena.LogEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return streamClosedMsg{}
		}
		return eventMsg(event)
	}
}

func nextTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.now = time.Time(msg)
		if m.finished {
			return m, nil
		}
		return m, nextTick()
	case eventMsg:
		m.applyEvent(arena.LogEvent(msg))
		return m, waitForEvent(m.events)
	case streamClosedMsg:
		if !m.finished {
			m.logs = append(m.logs, "event stream closed")
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) applyEvent(event arena.LogEvent) {
	switch event.Type {
	case "state_snapshot":
		if json.Unmarshal(event.Data, &m.snapshot) == nil {
			m.haveState = true
		}
	case "decision_started":
		m.thinking[event.ActorID] = time.Now()
		m.appendLog(fmt.Sprintf("%s  ▶ %s is thinking", virtualTime(event.VirtualMS), event.ActorID))
	case "decision_completed":
		delete(m.thinking, event.ActorID)
		var data struct {
			Action    string  `json:"action"`
			LatencyMS int64   `json:"latency_ms"`
			CostUSD   float64 `json:"cost_usd"`
			Error     string  `json:"error"`
			Finish    string  `json:"finish_reason"`
		}
		_ = json.Unmarshal(event.Data, &data)
		m.lastMove[event.ActorID] = data.Action
		line := fmt.Sprintf("%s  ◀ %s → %s  %dms  $%.6f", virtualTime(event.VirtualMS), event.ActorID, data.Action, data.LatencyMS, data.CostUSD)
		if data.Error != "" {
			line += "  ⚠ " + data.Error
		}
		m.appendLog(line)
	case "action_applied":
		var data struct {
			Action  string `json:"action"`
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(event.Data, &data)
		marker := "✓"
		if !data.Success {
			marker = "×"
		}
		m.appendLog(fmt.Sprintf("%s  %s %s  %s — %s", virtualTime(event.VirtualMS), marker, event.ActorID, data.Action, data.Message))
	case "agent_died":
		var data struct {
			Cause string `json:"cause"`
		}
		_ = json.Unmarshal(event.Data, &data)
		delete(m.thinking, event.ActorID)
		m.appendLog(fmt.Sprintf("%s  ☠ %s died: %s", virtualTime(event.VirtualMS), event.ActorID, data.Cause))
	case "food_spawned":
		var data struct {
			Position arena.Point `json:"position"`
		}
		_ = json.Unmarshal(event.Data, &data)
		m.appendLog(fmt.Sprintf("%s  + food spawned at (%d,%d)", virtualTime(event.VirtualMS), data.Position.X, data.Position.Y))
	case "run_finished":
		if json.Unmarshal(event.Data, &m.summary) == nil {
			m.finished = true
			m.thinking = make(map[string]time.Time)
			if m.summary.Winner == "" {
				m.appendLog(fmt.Sprintf("%s  ■ no winner (%s)", virtualTime(event.VirtualMS), m.summary.StopReason))
			} else {
				m.appendLog(fmt.Sprintf("%s  ★ winner: %s (%s)", virtualTime(event.VirtualMS), m.summary.Winner, m.summary.StopReason))
			}
		}
	}
}

func (m *Model) appendLog(line string) {
	m.logs = append(m.logs, line)
	if len(m.logs) > 200 {
		m.logs = m.logs[len(m.logs)-200:]
	}
}

func (m Model) View() string {
	if !m.haveState {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			titleStyle.Render("SURVIVALBENCH")+"\n\n"+dimStyle.Render("initializing arena…"))
	}

	header := m.renderHeader()
	mapPanel := panel("WORLD", green, m.renderMap())
	agentPanel := panel("AGENTS", blue, m.renderAgents())
	var upper string
	if m.width >= 74 {
		upper = lipgloss.JoinHorizontal(lipgloss.Top, mapPanel, "  ", agentPanel)
	} else {
		upper = lipgloss.JoinVertical(lipgloss.Left, mapPanel, agentPanel)
	}
	logHeight := max(2, m.height-lipgloss.Height(header)-lipgloss.Height(upper)-4)
	logs := panel("LIVE EVENT STREAM", amber, m.renderLogs(logHeight))
	footer := m.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, header, upper, logs, footer)
}

func (m Model) renderHeader() string {
	status := warnStyle.Render("● LIVE")
	if m.finished {
		status = goodStyle.Render("■ FINISHED")
	}
	wall := m.now.Sub(m.startedAt).Round(100 * time.Millisecond)
	left := titleStyle.Render("SURVIVALBENCH") + "  " + dimStyle.Render(m.config.RunName)
	right := fmt.Sprintf("%s   virtual %s   wall %s", status, virtualTime(m.snapshot.VirtualMS), wall)
	space := max(2, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", space) + right + "\n"
}

func (m Model) renderMap() string {
	if len(m.snapshot.MapRows) == 0 {
		return "waiting for map"
	}
	labels := m.agentLabels()
	positions := make(map[arena.Point]string)
	for _, player := range m.snapshot.Players {
		if player.Alive {
			positions[player.Pos] = labels[player.ID]
		}
	}
	var output strings.Builder
	output.WriteString("    ")
	for x := range m.snapshot.MapRows[0] {
		output.WriteString(fmt.Sprintf("%-2d", x))
	}
	output.WriteByte('\n')
	for y, row := range m.snapshot.MapRows {
		output.WriteString(fmt.Sprintf("%2d  ", y))
		for x := range row {
			if label, ok := positions[arena.Point{X: x, Y: y}]; ok {
				output.WriteString(m.agentStyle(label).Render(label + " "))
				continue
			}
			switch row[x] {
			case '#':
				output.WriteString(lipgloss.NewStyle().Foreground(red).Render("██"))
			case ':':
				output.WriteString(lipgloss.NewStyle().Foreground(soil).Render("▒▒"))
			case 'F':
				output.WriteString(lipgloss.NewStyle().Foreground(green).Bold(true).Render("● "))
			default:
				output.WriteString(dimStyle.Render("· "))
			}
		}
		if y < len(m.snapshot.MapRows)-1 {
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func (m Model) renderAgents() string {
	labels := m.agentLabels()
	players := append([]arena.Player(nil), m.snapshot.Players...)
	sort.SliceStable(players, func(i, j int) bool {
		return m.agentIndex(players[i].ID) < m.agentIndex(players[j].ID)
	})
	var blocks []string
	for _, player := range players {
		label := labels[player.ID]
		state := goodStyle.Render("READY")
		if started, ok := m.thinking[player.ID]; ok {
			seconds := max(0, m.now.Sub(started).Seconds())
			state = warnStyle.Render(fmt.Sprintf("THINKING %.1fs", seconds))
		}
		if !player.Alive {
			state = badStyle.Render("DEAD: " + player.DeathCause)
		}
		name := m.agentStyle(label).Render(label+" "+player.ID) + "  " + state
		hp := renderBar(player.HP, m.snapshot.MaxHP, red)
		hunger := renderBar(player.Hunger, m.snapshot.MaxHunger, amber)
		energy := renderBar(player.Energy, m.snapshot.MaxEnergy, cyan)
		last := m.lastMove[player.ID]
		if last == "" {
			last = "—"
		}
		meanLatency := float64(0)
		if player.Decisions > 0 {
			meanLatency = float64(player.TotalLatencyMS) / float64(player.Decisions)
		}
		blocks = append(blocks, strings.Join([]string{
			name,
			"HP  " + hp + fmt.Sprintf(" %d/%d   HUN ", player.HP, m.snapshot.MaxHP) + hunger + fmt.Sprintf(" %d/%d", player.Hunger, m.snapshot.MaxHunger),
			"ENG " + energy + fmt.Sprintf(" %d/%d   $ %.6f", player.Energy, m.snapshot.MaxEnergy, player.BudgetUSD()),
			fmt.Sprintf("pos (%d,%d)   turns %d   mean %.0fms", player.Pos.X, player.Pos.Y, player.Decisions, meanLatency),
			fmt.Sprintf("last %s   spent $%.6f", last, player.SpentUSD()),
		}, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func (m Model) renderLogs(height int) string {
	if len(m.logs) == 0 {
		return dimStyle.Render("waiting for decisions…")
	}
	count := min(height, len(m.logs))
	lines := m.logs[len(m.logs)-count:]
	width := max(30, m.width-6)
	styled := make([]string, 0, len(lines))
	for _, line := range lines {
		line = clip(line, width)
		switch {
		case strings.Contains(line, " ☠ "), strings.Contains(line, " ⚠ "), strings.Contains(line, " × "):
			styled = append(styled, badStyle.Render(line))
		case strings.Contains(line, " ✓ "), strings.Contains(line, " ★ "):
			styled = append(styled, goodStyle.Render(line))
		case strings.Contains(line, " ▶ "):
			styled = append(styled, warnStyle.Render(line))
		default:
			styled = append(styled, line)
		}
	}
	return strings.Join(styled, "\n")
}

func (m Model) renderFooter() string {
	legend := dimStyle.Render("██ obstacle  ▒▒ soil  ● food")
	if m.finished {
		if m.summary.Winner == "" {
			legend += "    " + warnStyle.Render("no winner: "+m.summary.StopReason)
		} else {
			legend += "    " + goodStyle.Render("winner "+m.summary.Winner)
		}
	}
	return legend + "    " + keyStyle.Render("q") + dimStyle.Render(" quit")
}

func (m Model) agentLabels() map[string]string {
	labels := make(map[string]string, len(m.config.Agents))
	for i, agent := range m.config.Agents {
		labels[agent.ID] = string(rune('A' + i))
	}
	return labels
}

func (m Model) agentIndex(id string) int {
	for index, agent := range m.config.Agents {
		if agent.ID == id {
			return index
		}
	}
	return len(m.config.Agents)
}

func (m Model) agentStyle(label string) lipgloss.Style {
	index := int([]rune(label)[0] - 'A')
	return lipgloss.NewStyle().Bold(true).Foreground(agentColors[index%len(agentColors)])
}

func renderBar(value, maximum int, color lipgloss.Color) string {
	const width = 8
	if maximum <= 0 {
		maximum = 1
	}
	value = max(0, min(value, maximum))
	filled := value * width / maximum
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", width-filled))
}

func panel(title string, color lipgloss.Color, content string) string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(color).Render(title)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).Padding(0, 1).Render(heading + "\n" + content)
}

func virtualTime(milliseconds int64) string {
	return fmt.Sprintf("%02d:%06.3f", milliseconds/60_000, float64(milliseconds%60_000)/1000)
}

func clip(value string, width int) string {
	if width <= 1 || lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
