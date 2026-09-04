package cli

import (
	"github.com/itaprac/sshuttlebox/internal/config"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type tuiPaneRow struct {
	text    string
	section bool
	item    int
	isItem  bool
}

func tuiVisibleRows(count, limit, selected int) (int, int) {
	if limit <= 0 || count <= limit {
		return 0, count
	}
	start := 0
	if selected >= limit {
		start = selected - limit + 1
	}
	start = minInt(start, count-limit)
	return start, start + limit
}

func tuiGroupLess(left, right string) bool {
	left, right = sectionLabel(left), sectionLabel(right)
	if left == right {
		return false
	}
	if left == "ungrouped" {
		return false
	}
	if right == "ungrouped" {
		return true
	}
	return left < right
}

func (m tuiModel) visibleFormFields() []int {
	fields := make([]int, 0, len(m.inputs))
	dynamic := m.screen == tuiScreenTunnelForm && m.inputs[tuiTunnelFieldType].Value() == "dynamic"
	for i := range m.inputs {
		if dynamic && (i == tuiTunnelFieldRemoteHost || i == tuiTunnelFieldRemotePort) {
			continue
		}
		fields = append(fields, i)
	}
	return fields
}

func (m *tuiModel) moveFormFocus(direction int) {
	fields := m.visibleFormFields()
	for i, field := range fields {
		if field != m.focus {
			continue
		}
		m.inputs[m.focus].Blur()
		m.focus = fields[(i+direction+len(fields))%len(fields)]
		m.inputs[m.focus].Focus()
		return
	}
}

func (m *tuiModel) cycleTunnelType(reverse bool) {
	types := []string{"local", "remote", "dynamic"}
	for i, value := range types {
		if m.inputs[tuiTunnelFieldType].Value() != value {
			continue
		}
		step := 1
		if reverse {
			step = len(types) - 1
		}
		m.inputs[tuiTunnelFieldType].SetValue(types[(i+step)%len(types)])
		return
	}
	m.inputs[tuiTunnelFieldType].SetValue("local")
}

func (m tuiModel) formFieldsView(title string, labels []string, fields []int, helpText string) string {
	focused := 0
	for i, field := range fields {
		if field == m.focus {
			focused = i
		}
	}
	limit := len(fields)
	if m.height > 0 {
		// Title, spacing, help, and the fixed status footer use six lines.
		limit = maxInt(1, (m.height-6)/2)
	}
	start, end := tuiVisibleRows(len(fields), limit, focused)
	lines := []string{titleStyle.Render(title), ""}
	for _, field := range fields[start:end] {
		prefix := "  "
		if field == m.focus {
			prefix = "> "
		}
		lines = append(lines, labelStyle.Render(truncate(prefix+labels[field], m.contentWidth())))
		value := m.inputs[field].View()
		if m.screen == tuiScreenTunnelForm && field == tuiTunnelFieldType {
			value = "‹ " + m.inputs[field].Value() + " ›"
		}
		lines = append(lines, value)
	}
	lines = append(lines, "", mutedStyle.Render(truncate(helpText, m.contentWidth())))
	return lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"))
}

func tuiHostPort(host config.Host) string {
	if host.Port == 0 && host.SSHConfigFile != "" {
		return "inherited"
	}
	return strconv.Itoa(effectivePort(host))
}

func (m tuiModel) contentWidth() int {
	if m.width <= 0 {
		return 100
	}
	return m.width
}
