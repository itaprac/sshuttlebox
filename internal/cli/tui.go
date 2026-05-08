package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/history"
)

type tuiScreen int

const (
	tuiScreenMain tuiScreen = iota
	tuiScreenForm
	tuiScreenRemove
	tuiScreenPreview
	tuiScreenKeySelect
)

const (
	tuiFieldName = iota
	tuiFieldHost
	tuiFieldUser
	tuiFieldPassword
	tuiFieldPort
	tuiFieldIdentityFile
	tuiFieldCount
)

const (
	tuiSectionRecent = iota
	tuiSectionAll
)

type tuiHostItem struct {
	name    string
	host    config.Host
	section int
}

type tuiModel struct {
	path        string
	cfg         config.Config
	names       []string
	hist        history.Log
	cursor      int
	width       int
	height      int
	screen      tuiScreen
	status      string
	err         error
	connectName string

	filter    textinput.Model
	inputs    []textinput.Model
	focus     int
	editOld   string
	keyPick   []identityFileChoice
	keyCursor int
	help      help.Model
	keys      tuiKeyMap
}

type tuiKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Connect  key.Binding
	Preview  key.Binding
	Add      key.Binding
	Edit     key.Binding
	Remove   key.Binding
	Filter   key.Binding
	ReuseKey key.Binding
	Save     key.Binding
	Cancel   key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func newTUIKeyMap() tuiKeyMap {
	return tuiKeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "down")),
		Connect:  key.NewBinding(key.WithKeys("enter", "c"), key.WithHelp("enter/c", "connect")),
		Preview:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "print")),
		Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Remove:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "remove")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ReuseKey: key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "reuse key")),
		Save:     key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		Cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k tuiKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Connect, k.Preview, k.Add, k.Edit, k.Remove, k.Filter, k.Help, k.Quit}
}

func (k tuiKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Connect, k.Preview},
		{k.Add, k.Edit, k.Remove, k.Filter},
		{k.ReuseKey, k.Save, k.Cancel, k.Help, k.Quit},
	}
}

func (a App) runUI(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: shbx ui")
	}

	model := newTUIModel()
	program := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := program.Run()
	if err != nil {
		return err
	}

	tui, ok := finalModel.(tuiModel)
	if !ok || tui.connectName == "" {
		return nil
	}
	return a.runConnect([]string{tui.connectName})
}

func newTUIModel() tuiModel {
	path, _, err := config.Init()
	if err != nil {
		return newTUIModelWithState("", config.Default(), err)
	}
	cfg, err := config.Load()
	return newTUIModelWithState(path, cfg, err)
}

func newTUIModelWithState(path string, cfg config.Config, err error) tuiModel {
	filter := textinput.New()
	filter.Placeholder = "filter hosts"
	filter.Width = 28
	filter.Prompt = "/ "

	h := help.New()
	keys := newTUIKeyMap()
	hist, _ := history.Load()
	model := tuiModel{
		path:   path,
		cfg:    cfg,
		hist:   hist,
		screen: tuiScreenMain,
		filter: filter,
		help:   h,
		keys:   keys,
	}
	if model.cfg.Hosts == nil {
		model.cfg.Hosts = map[string]config.Host{}
	}
	model.reloadNames()
	if err != nil {
		model.err = err
	}
	return model
}

func (m tuiModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		m.filter.Width = maxInt(12, minInt(38, msg.Width/3))
		for i := range m.inputs {
			m.inputs[i].Width = maxInt(18, minInt(52, msg.Width-24))
		}
	case tea.KeyMsg:
		switch m.screen {
		case tuiScreenMain:
			return m.updateMain(msg)
		case tuiScreenForm:
			return m.updateForm(msg)
		case tuiScreenRemove:
			return m.updateRemove(msg)
		case tuiScreenPreview:
			return m.updatePreview(msg)
		case tuiScreenKeySelect:
			return m.updateKeySelect(msg)
		}
	}

	if m.screen == tuiScreenMain && m.filter.Focused() {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.ensureCursor()
		return m, cmd
	}
	return m, nil
}

func (m tuiModel) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filter.Focused() {
		switch {
		case key.Matches(msg, m.keys.Cancel):
			m.filter.Blur()
			return m, nil
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.ensureCursor()
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
	case key.Matches(msg, m.keys.Filter):
		return m.focusFilter()
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.filteredItems())-1 {
			m.cursor++
		}
	case key.Matches(msg, m.keys.Connect):
		item, ok := m.selectedItem()
		if !ok {
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		m.connectName = item.name
		return m, tea.Quit
	case key.Matches(msg, m.keys.Preview):
		item, ok := m.selectedItem()
		if !ok {
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		m.status = connectCommandString(item.host, buildSSHArgs(item.host))
		m.err = nil
		m.screen = tuiScreenPreview
	case key.Matches(msg, m.keys.Add):
		return m.openForm("", config.Host{})
	case key.Matches(msg, m.keys.Edit):
		item, ok := m.selectedItem()
		if !ok {
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		return m.openForm(item.name, item.host)
	case key.Matches(msg, m.keys.Remove):
		if _, ok := m.selectedItem(); !ok {
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		m.screen = tuiScreenRemove
		m.status = ""
		m.err = nil
	}
	return m, nil
}

func (m tuiModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.screen = tuiScreenMain
		m.inputs = nil
		m.keyPick = nil
		m.status = "Cancelled."
		m.err = nil
		return m, nil
	case key.Matches(msg, m.keys.ReuseKey):
		if !m.openKeySelect() {
			m.setError(errors.New("no saved identity files to reuse"))
			return m, nil
		}
		m.status = ""
		m.err = nil
		return m, nil
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	}

	switch msg.String() {
	case "tab", "down":
		m.focusNext()
		return m, nil
	case "shift+tab", "up":
		m.focusPrev()
		return m, nil
	case "enter", "ctrl+s":
		if err := m.saveForm(); err != nil {
			m.setError(err)
			return m, nil
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m tuiModel) updateKeySelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Cancel):
		m.screen = tuiScreenForm
		m.status = "Key selection cancelled."
		m.err = nil
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.keyCursor > 0 {
			m.keyCursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.keyCursor < len(m.keyPick)-1 {
			m.keyCursor++
		}
	case key.Matches(msg, m.keys.Connect), msg.String() == "enter":
		if len(m.keyPick) == 0 || m.keyCursor < 0 || m.keyCursor >= len(m.keyPick) {
			m.screen = tuiScreenForm
			m.setError(errors.New("no identity file selected"))
			return m, nil
		}
		m.inputs[m.focus].Blur()
		m.inputs[tuiFieldIdentityFile].SetValue(m.keyPick[m.keyCursor].path)
		m.focus = tuiFieldIdentityFile
		m.inputs[m.focus].Focus()
		m.screen = tuiScreenForm
		m.status = "Reused identity file."
		m.err = nil
	}
	return m, nil
}

func (m tuiModel) updateRemove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		item, ok := m.selectedItem()
		if !ok {
			m.screen = tuiScreenMain
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		delete(m.cfg.Hosts, item.name)
		if err := config.Save(m.path, m.cfg); err != nil {
			m.screen = tuiScreenMain
			m.setError(err)
			return m, nil
		}
		m.reloadNames()
		m.ensureCursor()
		m.screen = tuiScreenMain
		m.status = fmt.Sprintf("Removed host %q", item.name)
		m.err = nil
	case "n", "N", "esc", "q":
		m.screen = tuiScreenMain
		m.status = "Cancelled."
		m.err = nil
	}
	return m, nil
}

func (m tuiModel) updatePreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Cancel), key.Matches(msg, m.keys.Preview):
		m.screen = tuiScreenMain
	}
	return m, nil
}

func (m tuiModel) View() string {
	if m.err != nil {
		m.status = "Error: " + m.err.Error()
	}

	var body string
	switch m.screen {
	case tuiScreenForm:
		body = m.formView()
	case tuiScreenRemove:
		body = m.removeView()
	case tuiScreenPreview:
		body = m.previewView()
	case tuiScreenKeySelect:
		body = m.keySelectView()
	default:
		body = m.mainView()
	}

	footer := statusStyle.Render(m.status)
	if m.status == "" {
		footer = mutedStyle.Render("Ready.")
	}
	helpView := mutedStyle.Render(m.help.View(m.keys))

	parts := make([]string, 0, 6)
	if banner := m.bannerView(); banner != "" {
		parts = append(parts, banner, "")
	}
	parts = append(parts, body, "", footer, helpView)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m tuiModel) bannerView() string {
	if m.screen != tuiScreenMain {
		return ""
	}
	if m.width > 0 && m.width < bannerMinWidth {
		return ""
	}
	if m.height > 0 && m.height < bannerMinHeight {
		return ""
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Bottom,
		renderGradientBanner(),
		"  ",
		bannerTaglineStyle.Render("sshuttlebox "+Version) + "\n",
	)
}

func renderGradientBanner() string {
	lines := strings.Split(bannerArt, "\n")
	width := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	if width == 0 {
		return bannerStyle.Render(bannerArt)
	}

	out := make([]string, len(lines))
	for li, line := range lines {
		runes := []rune(line)
		var b strings.Builder
		segStart := 0
		segColor := bannerColorAt(0, li, width)
		for i := 1; i <= len(runes); i++ {
			var nextColor lipgloss.Color
			if i < len(runes) {
				nextColor = bannerColorAt(i, li, width)
			}
			if i == len(runes) || nextColor != segColor {
				style := lipgloss.NewStyle().Foreground(segColor).Bold(true)
				b.WriteString(style.Render(string(runes[segStart:i])))
				if i < len(runes) {
					segStart = i
					segColor = nextColor
				}
			}
		}
		out[li] = b.String()
	}
	return strings.Join(out, "\n")
}

func bannerColorAt(col, row, width int) lipgloss.Color {
	if width <= 0 {
		return bannerPalette[0]
	}
	pos := col + row
	span := width + len(bannerPalette)
	idx := pos * len(bannerPalette) / span
	if idx < 0 {
		idx = 0
	}
	if idx >= len(bannerPalette) {
		idx = len(bannerPalette) - 1
	}
	return bannerPalette[idx]
}

func (m tuiModel) mainView() string {
	if m.width != 0 && m.width < tuiNarrowWidth {
		return m.narrowMainView()
	}
	return m.wideMainView()
}

func (m tuiModel) wideMainView() string {
	leftContentWidth := maxInt(42, minInt(64, m.width/3))
	rightContentWidth := maxInt(38, m.width-leftContentWidth-12)
	if m.width == 0 {
		leftContentWidth = 52
		rightContentWidth = 54
	}

	left := panelStyle.Width(leftContentWidth).Render(m.listView(leftContentWidth))
	detailsPanel := panelStyle.Width(rightContentWidth).Render(m.detailsView(rightContentWidth))
	logPanel := panelStyle.Width(rightContentWidth).Render(m.connectLogView(rightContentWidth))
	right := lipgloss.JoinVertical(lipgloss.Left, detailsPanel, logPanel)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func (m tuiModel) narrowMainView() string {
	contentWidth := maxInt(36, m.width-8)
	list := panelStyle.Width(contentWidth).Render(m.listView(contentWidth))
	details := panelStyle.Width(contentWidth).Render(m.detailsView(contentWidth))
	logPanel := panelStyle.Width(contentWidth).Render(m.connectLogView(contentWidth))
	return lipgloss.JoinVertical(lipgloss.Left, list, details, logPanel)
}

func (m tuiModel) listView(width int) string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("HOSTS"))
	b.WriteString("\n")
	if m.filter.Focused() || m.filter.Value() != "" {
		b.WriteString(m.filter.View())
	} else {
		b.WriteString(mutedStyle.Render("press / to filter"))
	}
	b.WriteString("\n\n")

	items := m.filteredItems()
	if len(items) == 0 {
		b.WriteString(mutedStyle.Render("No saved hosts. Press a to add one."))
		return b.String()
	}

	prevSection := -1
	for i, item := range items {
		if item.section != prevSection {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(subSectionStyle.Render(sectionLabel(item.section)))
			b.WriteString("\n")
			prevSection = item.section
		}
		if i == m.cursor {
			line := hostRow(item, width-2, selectedTargetStyle)
			b.WriteString(selectedStyle.Render(fitRow("> "+line, width)))
		} else {
			line := hostRow(item, width-2, mutedStyle)
			b.WriteString(normalRowStyle.Render(fitRow("  "+line, width)))
		}
		if i < len(items)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func sectionLabel(section int) string {
	switch section {
	case tuiSectionRecent:
		return "▸ recent"
	default:
		return "▸ all"
	}
}

func (m tuiModel) connectLogView(width int) string {
	rows := []string{sectionTitleStyle.Render("RECENT CONNECTS")}
	events := m.hist.Recent(5)
	if len(events) == 0 {
		rows = append(rows, "", mutedStyle.Render("No connections yet."))
		return strings.Join(rows, "\n")
	}
	rows = append(rows, "")
	maxLine := width - 4
	if maxLine < 12 {
		maxLine = 12
	}
	now := time.Now()
	for _, ev := range events {
		ago := relativeTime(now, ev.At)
		nameMax := maxLine - lipgloss.Width(ago) - 3
		if nameMax < 4 {
			nameMax = 4
		}
		name := truncate(ev.HostName, nameMax)
		gap := maxLine - lipgloss.Width(name) - lipgloss.Width(ago)
		if gap < 1 {
			gap = 1
		}
		rows = append(rows, name+strings.Repeat(" ", gap)+mutedStyle.Render(ago))
	}
	return strings.Join(rows, "\n")
}

func relativeTime(now, t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

func (m tuiModel) detailsView(width int) string {
	item, ok := m.selectedItem()
	if !ok {
		return sectionTitleStyle.Render("DETAILS") + "\n\n" + mutedStyle.Render("Select or add a host.")
	}

	rows := []string{
		sectionTitleStyle.Render("DETAILS"),
		titleStyle.Render(truncate(item.name, width)),
		"",
		labelValue("Host", item.host.Host),
		labelValue("User", optionalValue(item.host.User)),
		labelValue("Port", strconv.Itoa(effectivePort(item.host))),
		labelValue("Identity file", optionalValue(item.host.IdentityFile)),
		labelValue("Password", passwordStatus(item.host)),
		"",
		mutedStyle.Render(truncate(connectCommandString(item.host, buildSSHArgs(item.host)), width)),
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) formView() string {
	title := "Add host"
	if m.editOld != "" {
		title = "Edit host"
	}
	lines := []string{titleStyle.Render(title), ""}
	labels := []string{"Name", "Host", "User", "Password", "Port", "Identity file"}
	hasReusableKeys := len(identityFileChoices(m.cfg.Hosts, m.editOld)) > 0
	for i, input := range m.inputs {
		label := labels[i]
		if i == tuiFieldIdentityFile && hasReusableKeys {
			label += " " + mutedStyle.Render("(ctrl+k)")
		}
		if i == m.focus {
			label = "> " + label
		} else {
			label = "  " + label
		}
		lines = append(lines, labelStyle.Render(label), input.View())
	}
	helpText := "tab/down: next  shift+tab/up: previous  enter/ctrl+s: save  esc: cancel"
	lines = append(lines, "", mutedStyle.Render(helpText))
	return panelStyle.Width(maxInt(54, minInt(76, m.width-4))).Render(strings.Join(lines, "\n"))
}

func (m tuiModel) keySelectView() string {
	width := maxInt(56, minInt(84, m.width-8))
	if m.width == 0 {
		width = 68
	}

	lines := []string{titleStyle.Render("Choose identity file"), ""}
	if len(m.keyPick) == 0 {
		lines = append(lines, mutedStyle.Render("No saved identity files."))
	} else {
		for i, choice := range m.keyPick {
			line := fitRow(identityFileChoiceLabel(choice), width-6)
			if i == m.keyCursor {
				lines = append(lines, selectedStyle.Render(fitRow("> "+line, width-2)))
			} else {
				lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width-2)))
			}
		}
	}
	lines = append(lines, "", mutedStyle.Render("up/down: select  enter: use  esc: back"))
	return panelStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (m tuiModel) removeView() string {
	item, ok := m.selectedItem()
	name := ""
	if ok {
		name = item.name
	}
	text := fmt.Sprintf("Remove host %q?\n\nPress y/enter to remove, n/esc to cancel.", name)
	return panelStyle.Width(52).Render(titleStyle.Render("Confirm remove") + "\n\n" + text)
}

func (m tuiModel) previewView() string {
	return panelStyle.Width(maxInt(60, minInt(90, m.width-4))).Render(titleStyle.Render("SSH command") + "\n\n" + m.status + "\n\n" + mutedStyle.Render("esc/p: back  q: quit"))
}

func (m *tuiModel) reloadNames() {
	m.names = m.names[:0]
	for name := range m.cfg.Hosts {
		m.names = append(m.names, name)
	}
	sort.Strings(m.names)
	m.ensureCursor()
}

func (m *tuiModel) ensureCursor() {
	items := m.filteredItems()
	if len(items) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
}

func (m tuiModel) filteredItems() []tuiHostItem {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	matches := func(name string, host config.Host) bool {
		if query == "" {
			return true
		}
		haystack := strings.ToLower(strings.Join([]string{name, host.Host, host.User, host.IdentityFile, formatListTarget(host)}, " "))
		return strings.Contains(haystack, query)
	}

	recentNames := m.hist.RecentNames(5, func(name string) bool {
		_, ok := m.cfg.Hosts[name]
		return ok
	})
	inRecent := make(map[string]bool, len(recentNames))

	items := make([]tuiHostItem, 0, len(m.names)+len(recentNames))
	for _, name := range recentNames {
		host := m.cfg.Hosts[name]
		if !matches(name, host) {
			continue
		}
		inRecent[name] = true
		items = append(items, tuiHostItem{name: name, host: host, section: tuiSectionRecent})
	}
	for _, name := range m.names {
		if inRecent[name] {
			continue
		}
		host := m.cfg.Hosts[name]
		if !matches(name, host) {
			continue
		}
		items = append(items, tuiHostItem{name: name, host: host, section: tuiSectionAll})
	}
	return items
}

func (m tuiModel) selectedItem() (tuiHostItem, bool) {
	items := m.filteredItems()
	if len(items) == 0 || m.cursor < 0 || m.cursor >= len(items) {
		return tuiHostItem{}, false
	}
	return items[m.cursor], true
}

func (m tuiModel) focusFilter() (tea.Model, tea.Cmd) {
	cmd := m.filter.Focus()
	m.status = "Filtering hosts."
	m.err = nil
	return m, cmd
}

func (m tuiModel) openForm(name string, host config.Host) (tea.Model, tea.Cmd) {
	inputs := make([]textinput.Model, tuiFieldCount)
	placeholders := []string{"prod", "192.0.2.10", "deploy", "optional", "22", "~/.ssh/id_ed25519"}
	values := []string{name, host.Host, host.User, host.Password, "", host.IdentityFile}
	if host.Port != 0 {
		values[tuiFieldPort] = strconv.Itoa(host.Port)
	}

	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Placeholder = placeholders[i]
		inputs[i].Width = maxInt(18, minInt(52, m.width-24))
		inputs[i].SetValue(values[i])
		if i == tuiFieldPassword {
			inputs[i].EchoMode = textinput.EchoPassword
			inputs[i].EchoCharacter = '*'
		}
	}
	cmd := inputs[0].Focus()

	m.inputs = inputs
	m.focus = 0
	m.editOld = name
	m.keyPick = nil
	m.keyCursor = 0
	m.screen = tuiScreenForm
	m.status = ""
	m.err = nil
	return m, cmd
}

func (m *tuiModel) focusNext() {
	m.inputs[m.focus].Blur()
	m.focus = (m.focus + 1) % len(m.inputs)
	m.inputs[m.focus].Focus()
}

func (m *tuiModel) focusPrev() {
	m.inputs[m.focus].Blur()
	m.focus--
	if m.focus < 0 {
		m.focus = len(m.inputs) - 1
	}
	m.inputs[m.focus].Focus()
}

func (m *tuiModel) openKeySelect() bool {
	choices := identityFileChoices(m.cfg.Hosts, m.editOld)
	if len(choices) == 0 {
		return false
	}

	current := strings.TrimSpace(m.inputs[tuiFieldIdentityFile].Value())
	cursor := 0
	for i, choice := range choices {
		if choice.path == current {
			cursor = i
			break
		}
	}

	m.keyPick = choices
	m.keyCursor = cursor
	m.screen = tuiScreenKeySelect
	return true
}

func (m *tuiModel) saveForm() error {
	name := strings.TrimSpace(m.inputs[tuiFieldName].Value())
	host := config.Host{
		Host:         strings.TrimSpace(m.inputs[tuiFieldHost].Value()),
		User:         strings.TrimSpace(m.inputs[tuiFieldUser].Value()),
		Password:     m.inputs[tuiFieldPassword].Value(),
		IdentityFile: strings.TrimSpace(m.inputs[tuiFieldIdentityFile].Value()),
	}
	if name == "" {
		return errors.New("host name cannot be empty")
	}
	if host.Host == "" {
		return errors.New("SSH host cannot be empty")
	}
	portText := strings.TrimSpace(m.inputs[tuiFieldPort].Value())
	if portText != "" {
		port, err := strconv.Atoi(portText)
		if err != nil {
			return fmt.Errorf("invalid port %q", portText)
		}
		host.Port = port
	}
	if host.Port < 0 || host.Port > 65535 {
		return fmt.Errorf("invalid port %d", host.Port)
	}
	if m.editOld == "" {
		if _, exists := m.cfg.Hosts[name]; exists {
			return fmt.Errorf("host %q already exists", name)
		}
	} else if name != m.editOld {
		if _, exists := m.cfg.Hosts[name]; exists {
			return fmt.Errorf("host %q already exists", name)
		}
		delete(m.cfg.Hosts, m.editOld)
	}

	m.cfg.Hosts[name] = host
	if err := config.Save(m.path, m.cfg); err != nil {
		return err
	}
	m.reloadNames()
	for i, existing := range m.filteredItems() {
		if existing.name == name {
			m.cursor = i
			break
		}
	}
	m.screen = tuiScreenMain
	m.inputs = nil
	m.keyPick = nil
	action := "Added"
	if m.editOld != "" {
		action = "Updated"
	}
	m.status = fmt.Sprintf("%s host %q", action, name)
	if warning := identityFileWarning(host.IdentityFile); warning != "" {
		m.status += ". " + warning
	}
	m.err = nil
	return nil
}

func (m *tuiModel) setError(err error) {
	m.err = err
	m.status = ""
}

func identityFileWarning(path string) string {
	if path == "" {
		return ""
	}
	expandedPath := expandHomePath(path)
	info, err := os.Stat(expandedPath)
	if err == nil {
		if info.IsDir() {
			return "Warning: identity file is a directory: " + path
		}
		return ""
	}
	if errors.Is(err, os.ErrNotExist) {
		return "Warning: identity file does not exist: " + path
	}
	return fmt.Sprintf("Warning: cannot check identity file %s: %v", path, err)
}

func effectivePort(host config.Host) int {
	if host.Port == 0 {
		return defaultSSHPort
	}
	return host.Port
}

func optionalValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func passwordStatus(host config.Host) string {
	if host.Password == "" {
		return "-"
	}
	return "set"
}

func labelValue(label, value string) string {
	return labelStyle.Render(label+": ") + value
}

func hostRow(item tuiHostItem, width int, targetStyle lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	name := item.name
	target := formatListTarget(item.host)
	full := name + "  " + targetStyle.Render(target)
	if lipgloss.Width(name)+2+lipgloss.Width(target) <= width {
		return full
	}

	minTargetWidth := minInt(24, width/2)
	if width >= 34 {
		targetWidth := maxInt(minTargetWidth, width-lipgloss.Width(name)-2)
		nameWidth := width - targetWidth - 2
		if nameWidth >= 10 {
			return truncate(name, nameWidth) + "  " + targetStyle.Render(truncate(target, targetWidth))
		}
	}
	return truncate(name+"  "+target, width)
}

func fitRow(value string, width int) string {
	value = truncate(value, width)
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if width <= 3 || lipgloss.Width(value) <= width {
		return value
	}
	return value[:width-3] + "..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

const (
	bannerArt = `███████╗██╗  ██╗██████╗ ██╗  ██╗
██╔════╝██║  ██║██╔══██╗╚██╗██╔╝
███████╗███████║██████╔╝ ╚███╔╝
╚════██║██╔══██║██╔══██╗ ██╔██╗
███████║██║  ██║██████╔╝██╔╝ ██╗
╚══════╝╚═╝  ╚═╝╚═════╝ ╚═╝  ╚═╝`
	bannerMinWidth  = 60
	bannerMinHeight = 24
	tuiNarrowWidth  = 100
)

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(1, 2)
	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("69"))
	subSectionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("105")).
			Bold(true)
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("63"))
	bannerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("63")).
			Bold(true)
	bannerPalette = []lipgloss.Color{
		lipgloss.Color("63"),
		lipgloss.Color("99"),
		lipgloss.Color("105"),
		lipgloss.Color("141"),
		lipgloss.Color("177"),
		lipgloss.Color("213"),
	}
	bannerTaglineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("69")).
				Italic(true)
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("63"))
	selectedTargetStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("63"))
	normalRowStyle = lipgloss.NewStyle()
	mutedStyle     = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))
)
