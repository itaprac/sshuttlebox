package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
)

type tuiScreen int

const (
	tuiScreenMain tuiScreen = iota
	tuiScreenForm
	tuiScreenTunnelForm
	tuiScreenRemove
	tuiScreenPreview
	tuiScreenPalette
	tuiScreenKeySelect
	tuiScreenHostSelect
	tuiScreenGroupSelect
)

type tuiMode int

const (
	tuiModeHosts tuiMode = iota
	tuiModeTunnels
)

const (
	tuiFieldName = iota
	tuiFieldHost
	tuiFieldUser
	tuiFieldPassword
	tuiFieldPort
	tuiFieldIdentityFile
	tuiFieldGroup
	tuiFieldCount
)

const (
	tuiTunnelFieldName = iota
	tuiTunnelFieldHost
	tuiTunnelFieldType
	tuiTunnelFieldBind
	tuiTunnelFieldLocalPort
	tuiTunnelFieldRemoteHost
	tuiTunnelFieldRemotePort
	tuiTunnelFieldGroup
	tuiTunnelFieldCount
)

type tuiHostItem struct {
	name  string
	host  config.Host
	group string
}

type tuiTunnelItem struct {
	name   string
	tunnel config.Tunnel
	group  string
}

type tuiPaletteAction int

const (
	tuiPaletteConnect tuiPaletteAction = iota
	tuiPalettePreview
	tuiPaletteSFTP
	tuiPaletteSFTPPreview
	tuiPaletteAddHost
	tuiPaletteAddTunnel
	tuiPaletteEdit
	tuiPaletteRemove
	tuiPaletteSwitch
	tuiPaletteFilter
	tuiPaletteDoctor
)

type tuiPaletteItem struct {
	title  string
	hint   string
	action tuiPaletteAction
}

type tuiModel struct {
	path         string
	cfg          config.Config
	names        []string
	tunnelNames  []string
	cursor       int
	tunnelCursor int
	mode         tuiMode
	width        int
	height       int
	screen       tuiScreen
	status       string
	err          error
	connectName  string
	sftpName     string
	tunnelName   string
	outputTitle  string
	showDetails  bool
	hideDetails  bool

	filter        textinput.Model
	inputs        []textinput.Model
	focus         int
	editOld       string
	editTunnelOld string
	keyPick       []identityFileChoice
	keyCursor     int
	hostPick      []string
	hostCursor    int
	groupPick     []string
	groupCursor   int
	groupReturn   tuiScreen
	groupField    int
	palette       []tuiPaletteItem
	paletteCursor int
	paletteQuery  string
	help          help.Model
	keys          tuiKeyMap
}

type tuiKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Connect   key.Binding
	SFTP      key.Binding
	Preview   key.Binding
	Add       key.Binding
	Edit      key.Binding
	Remove    key.Binding
	Switch    key.Binding
	Filter    key.Binding
	Details   key.Binding
	Palette   key.Binding
	ReuseKey  key.Binding
	PickHost  key.Binding
	PickGroup key.Binding
	Save      key.Binding
	Cancel    key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func newTUIKeyMap() tuiKeyMap {
	return tuiKeyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "down")),
		Connect:   key.NewBinding(key.WithKeys("enter", "c"), key.WithHelp("enter/c", "connect")),
		SFTP:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sftp")),
		Preview:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "print")),
		Add:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Remove:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "remove")),
		Switch:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "hosts/tunnels")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Details:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
		Palette:   key.NewBinding(key.WithKeys(":", "ctrl+p"), key.WithHelp(":", "commands")),
		ReuseKey:  key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "reuse key")),
		PickHost:  key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("ctrl+h", "pick host")),
		PickGroup: key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "pick group")),
		Save:      key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k tuiKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Connect, k.SFTP, k.Preview, k.Add, k.Edit, k.Remove, k.Switch, k.Filter, k.Details, k.Palette, k.Help, k.Quit}
}

func (k tuiKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Connect, k.SFTP, k.Preview},
		{k.Add, k.Edit, k.Remove, k.Switch, k.Filter, k.Details, k.Palette},
		{k.ReuseKey, k.PickHost, k.PickGroup, k.Save, k.Cancel, k.Help, k.Quit},
	}
}

func (a App) runUI(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: shbx ui")
	}

	nextMode := tuiModeHosts
	nextStatus := ""
	for {
		model := newTUIModel()
		model.mode = nextMode
		if model.mode == tuiModeTunnels {
			model.filter.Placeholder = "filter tunnels"
			model.ensureTunnelCursor()
		}
		if nextStatus != "" {
			model.status = nextStatus
			model.err = nil
		}

		program := tea.NewProgram(model, tea.WithAltScreen())
		finalModel, err := program.Run()
		if err != nil {
			return err
		}

		tui, ok := finalModel.(tuiModel)
		if !ok {
			return nil
		}
		if tui.connectName != "" {
			return a.runConnect([]string{tui.connectName})
		}
		if tui.sftpName != "" {
			return a.runSFTP([]string{tui.sftpName})
		}
		if tui.tunnelName == "" {
			return nil
		}
		if err := a.runTunnelStart([]string{tui.tunnelName}); err != nil {
			return err
		}
		nextMode = tuiModeTunnels
		nextStatus = fmt.Sprintf("Returned after starting tunnel %q.", tui.tunnelName)
	}
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
	model := tuiModel{
		path:   path,
		cfg:    cfg,
		screen: tuiScreenMain,
		filter: filter,
		help:   h,
		keys:   keys,
	}
	if model.cfg.Hosts == nil {
		model.cfg.Hosts = map[string]config.Host{}
	}
	if model.cfg.Tunnels == nil {
		model.cfg.Tunnels = map[string]config.Tunnel{}
	}
	if model.cfg.Groups == nil {
		model.cfg.Groups = map[string]config.Group{}
	}
	model.reloadNames()
	model.reloadTunnelNames()
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
		case tuiScreenTunnelForm:
			return m.updateTunnelForm(msg)
		case tuiScreenRemove:
			return m.updateRemove(msg)
		case tuiScreenPreview:
			return m.updatePreview(msg)
		case tuiScreenPalette:
			return m.updatePalette(msg)
		case tuiScreenKeySelect:
			return m.updateKeySelect(msg)
		case tuiScreenHostSelect:
			return m.updateHostSelect(msg)
		case tuiScreenGroupSelect:
			return m.updateGroupSelect(msg)
		}
	}

	if m.screen == tuiScreenMain && m.filter.Focused() {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.ensureActiveCursor()
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
		case msg.Type == tea.KeyCtrlC:
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.ensureActiveCursor()
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
	case key.Matches(msg, m.keys.Switch):
		m.toggleMode()
	case key.Matches(msg, m.keys.Filter):
		return m.focusFilter()
	case key.Matches(msg, m.keys.Details):
		if m.detailsVisible() {
			m.showDetails = false
			m.hideDetails = true
			m.status = "Hiding details."
		} else {
			m.showDetails = true
			m.hideDetails = false
			m.status = "Showing details."
		}
		m.err = nil
	case key.Matches(msg, m.keys.Palette):
		return m.openPalette()
	case key.Matches(msg, m.keys.Up):
		if m.mode == tuiModeTunnels && m.tunnelCursor > 0 {
			m.tunnelCursor--
		} else if m.mode == tuiModeHosts && m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.mode == tuiModeTunnels && m.tunnelCursor < len(m.filteredTunnelItems())-1 {
			m.tunnelCursor++
		} else if m.mode == tuiModeHosts && m.cursor < len(m.filteredItems())-1 {
			m.cursor++
		}
	case key.Matches(msg, m.keys.Connect):
		if m.mode == tuiModeTunnels {
			item, ok := m.selectedTunnelItem()
			if !ok {
				m.setError(errors.New("no tunnel selected"))
				return m, nil
			}
			if _, running, err := tunnelstate.Get(item.name); err != nil {
				m.setError(err)
				return m, nil
			} else if !running {
				if host, ok := m.cfg.Hosts[item.tunnel.Host]; ok && host.Password == "" {
					m.tunnelName = item.name
					return m, tea.Quit
				}
			}
			if err := m.toggleTunnel(item.name, item.tunnel); err != nil {
				m.setError(err)
			}
			return m, nil
		}
		item, ok := m.selectedItem()
		if !ok {
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		m.connectName = item.name
		return m, tea.Quit
	case key.Matches(msg, m.keys.SFTP):
		return m.runSelectedSFTP()
	case key.Matches(msg, m.keys.Preview):
		if m.mode == tuiModeTunnels {
			item, ok := m.selectedTunnelItem()
			if !ok {
				m.setError(errors.New("no tunnel selected"))
				return m, nil
			}
			host, ok := m.cfg.Hosts[item.tunnel.Host]
			if !ok {
				m.setError(fmt.Errorf("host %q for tunnel %q not found", item.tunnel.Host, item.name))
				return m, nil
			}
			sshArgs, err := buildTunnelStartSSHArgs(item.name, host, item.tunnel)
			if err != nil {
				m.setError(err)
				return m, nil
			}
			m.status = tunnelCommandString(host, sshArgs)
			m.err = nil
			m.outputTitle = "Tunnel command"
			m.screen = tuiScreenPreview
			return m, nil
		}
		item, ok := m.selectedItem()
		if !ok {
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		m.status = connectCommandString(item.host, buildSSHArgs(item.host))
		m.err = nil
		m.outputTitle = "SSH command"
		m.screen = tuiScreenPreview
	case key.Matches(msg, m.keys.Add):
		if m.mode == tuiModeTunnels {
			return m.openTunnelForm("", config.Tunnel{Type: "local"})
		}
		return m.openForm("", config.Host{})
	case key.Matches(msg, m.keys.Edit):
		if m.mode == tuiModeTunnels {
			item, ok := m.selectedTunnelItem()
			if !ok {
				m.setError(errors.New("no tunnel selected"))
				return m, nil
			}
			return m.openTunnelForm(item.name, item.tunnel)
		}
		item, ok := m.selectedItem()
		if !ok {
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		return m.openForm(item.name, item.host)
	case key.Matches(msg, m.keys.Remove):
		if m.mode == tuiModeTunnels {
			if _, ok := m.selectedTunnelItem(); !ok {
				m.setError(errors.New("no tunnel selected"))
				return m, nil
			}
		} else if _, ok := m.selectedItem(); !ok {
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
	case key.Matches(msg, m.keys.PickGroup):
		if !m.openGroupSelect(tuiScreenForm, tuiFieldGroup) {
			m.setError(errors.New("no saved groups to choose"))
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

func (m tuiModel) updateTunnelForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.screen = tuiScreenMain
		m.inputs = nil
		m.hostPick = nil
		m.status = "Cancelled."
		m.err = nil
		return m, nil
	case key.Matches(msg, m.keys.PickHost):
		if !m.openHostSelect() {
			m.setError(errors.New("no saved hosts to choose"))
			return m, nil
		}
		m.status = ""
		m.err = nil
		return m, nil
	case key.Matches(msg, m.keys.PickGroup):
		if !m.openGroupSelect(tuiScreenTunnelForm, tuiTunnelFieldGroup) {
			m.setError(errors.New("no saved groups to choose"))
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
		if err := m.saveTunnelForm(); err != nil {
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

func (m tuiModel) updateHostSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Cancel):
		m.screen = tuiScreenTunnelForm
		m.status = "Host selection cancelled."
		m.err = nil
		m.hostPick = nil
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.hostCursor > 0 {
			m.hostCursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.hostCursor < len(m.hostPick)-1 {
			m.hostCursor++
		}
	case key.Matches(msg, m.keys.Connect), msg.String() == "enter":
		if len(m.hostPick) == 0 || m.hostCursor < 0 || m.hostCursor >= len(m.hostPick) {
			m.screen = tuiScreenTunnelForm
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		m.inputs[m.focus].Blur()
		m.inputs[tuiTunnelFieldHost].SetValue(m.hostPick[m.hostCursor])
		m.focus = tuiTunnelFieldHost
		m.inputs[m.focus].Focus()
		m.screen = tuiScreenTunnelForm
		m.status = "Selected SSH host."
		m.err = nil
		m.hostPick = nil
	}
	return m, nil
}

func (m tuiModel) updateGroupSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Cancel):
		m.screen = m.groupReturn
		m.status = "Group selection cancelled."
		m.err = nil
		m.groupPick = nil
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.groupCursor > 0 {
			m.groupCursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.groupCursor < len(m.groupPick)-1 {
			m.groupCursor++
		}
	case key.Matches(msg, m.keys.Connect), msg.String() == "enter":
		if len(m.groupPick) == 0 || m.groupCursor < 0 || m.groupCursor >= len(m.groupPick) {
			m.screen = m.groupReturn
			m.setError(errors.New("no group selected"))
			return m, nil
		}
		if m.groupField >= 0 && m.groupField < len(m.inputs) {
			m.inputs[m.focus].Blur()
			m.inputs[m.groupField].SetValue(m.groupPick[m.groupCursor])
			m.focus = m.groupField
			m.inputs[m.focus].Focus()
		}
		m.screen = m.groupReturn
		m.status = "Selected group."
		m.err = nil
		m.groupPick = nil
	}
	return m, nil
}

func (m tuiModel) updateRemove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		if m.mode == tuiModeTunnels {
			item, ok := m.selectedTunnelItem()
			if !ok {
				m.screen = tuiScreenMain
				m.setError(errors.New("no tunnel selected"))
				return m, nil
			}
			if host, ok := m.cfg.Hosts[item.tunnel.Host]; ok {
				_, _, _ = stopTunnelByName(item.name, host)
			}
			delete(m.cfg.Tunnels, item.name)
			if err := config.Save(m.path, m.cfg); err != nil {
				m.screen = tuiScreenMain
				m.setError(err)
				return m, nil
			}
			m.reloadTunnelNames()
			m.ensureTunnelCursor()
			m.screen = tuiScreenMain
			m.status = fmt.Sprintf("Removed tunnel %q", item.name)
			m.err = nil
			return m, nil
		}
		item, ok := m.selectedItem()
		if !ok {
			m.screen = tuiScreenMain
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		dependentTunnels := dependentTunnelNames(m.cfg, item.name)
		if len(dependentTunnels) > 0 {
			m.screen = tuiScreenMain
			m.setError(fmt.Errorf("host %q is used by tunnels: %s", item.name, strings.Join(dependentTunnels, ", ")))
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
	case "n", "N", "esc":
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
		m.outputTitle = ""
	}
	return m, nil
}

func (m tuiModel) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m, tea.Quit
	case key.Matches(msg, m.keys.Cancel):
		m.screen = tuiScreenMain
		m.palette = nil
		m.paletteQuery = ""
		m.status = "Cancelled."
		m.err = nil
		return m, nil
	case msg.Type == tea.KeyUp:
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
	case msg.Type == tea.KeyDown:
		if m.paletteCursor < len(m.filteredPaletteItems())-1 {
			m.paletteCursor++
		}
	case msg.Type == tea.KeyEnter:
		items := m.filteredPaletteItems()
		if len(items) == 0 || m.paletteCursor < 0 || m.paletteCursor >= len(items) {
			m.setError(errors.New("no command selected"))
			return m, nil
		}
		return m.runPaletteAction(items[m.paletteCursor].action)
	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyCtrlH:
		if m.paletteQuery != "" {
			runes := []rune(m.paletteQuery)
			m.paletteQuery = string(runes[:len(runes)-1])
			m.paletteCursor = 0
			m.err = nil
		}
	case msg.Type == tea.KeyRunes:
		m.paletteQuery += string(msg.Runes)
		m.paletteCursor = 0
		m.err = nil
	}
	m.ensurePaletteCursor()
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
	case tuiScreenTunnelForm:
		body = m.tunnelFormView()
	case tuiScreenRemove:
		body = m.removeView()
	case tuiScreenPreview:
		body = m.previewView()
	case tuiScreenPalette:
		body = m.paletteView()
	case tuiScreenKeySelect:
		body = m.keySelectView()
	case tuiScreenHostSelect:
		body = m.hostSelectView()
	case tuiScreenGroupSelect:
		body = m.groupSelectView()
	default:
		body = m.mainView()
	}

	footer := statusStyle.Render(m.status)
	if m.status == "" {
		footer = mutedStyle.Render("Ready.")
	}
	if m.screen == tuiScreenPreview {
		footer = ""
	}
	helpView := ""
	if m.showGlobalHelp() {
		helpView = mutedStyle.Render(m.help.View(m.keys))
	}
	if m.screen == tuiScreenPreview {
		helpView = ""
	}

	parts := make([]string, 0, 6)
	parts = append(parts, body, "")
	if footer != "" {
		parts = append(parts, footer)
	}
	if helpView != "" {
		parts = append(parts, helpView)
	}
	return trimToHeight(lipgloss.JoinVertical(lipgloss.Left, parts...), m.height)
}

func (m tuiModel) showGlobalHelp() bool {
	if m.screen == tuiScreenPreview {
		return false
	}
	if m.screen == tuiScreenMain && m.height > 0 && m.height < 18 {
		return false
	}
	return true
}

func (m tuiModel) mainBodyHeight() int {
	if m.height <= 0 {
		return 0
	}

	used := 1 // blank line between body and footer
	used++    // status footer
	if m.showGlobalHelp() {
		used += 1
	}

	return maxInt(4, m.height-used)
}

func (m tuiModel) mainView() string {
	if m.width != 0 && m.width < tuiNarrowWidth {
		return m.narrowMainView()
	}
	return m.wideMainView()
}

func (m tuiModel) wideMainView() string {
	contentWidth := m.width - 2
	if m.width == 0 {
		contentWidth = 110
	}
	contentWidth = maxInt(80, contentWidth)

	bodyHeight := m.mainBodyHeight()
	shellWidth := maxInt(appShellMinWidth, contentWidth-appShellChrome)
	sidebarWidth := wideSidebarWidth(shellWidth)
	detailsWidth := shellWidth - sidebarWidth - 3
	showDetails := m.detailsVisibleAtWidth(detailsWidth)
	header := m.appHeaderView(shellWidth, bodyHeight == 0 || bodyHeight >= compactLogoMinBodyHeight)
	contentHeight := 18
	if bodyHeight > 0 {
		contentHeight = maxInt(1, bodyHeight-appShellFrameLines-lipgloss.Height(header))
	}

	if !showDetails {
		sidebar := m.sidebarView(shellWidth, contentHeight)
		content := lipgloss.JoinVertical(lipgloss.Left, header, sidebar)
		return appShellStyle.Width(shellWidth).Render(content)
	}

	detailsWidth = maxInt(detailsMinWidth, detailsWidth)
	sidebar := m.sidebarView(sidebarWidth, contentHeight)
	divider := verticalDivider(contentHeight)
	details := m.detailsPaneView(detailsWidth, contentHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", divider, " ", details)
	content := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return appShellStyle.Width(shellWidth).Render(content)
}

func (m tuiModel) narrowMainView() string {
	contentWidth := maxInt(40, m.width-2)
	shellWidth := maxInt(36, contentWidth-appShellChrome)
	bodyHeight := m.mainBodyHeight()
	header := m.appHeaderView(shellWidth, bodyHeight == 0 || bodyHeight >= compactLogoMinBodyHeight)
	contentHeight := 12
	if bodyHeight > 0 {
		contentHeight = maxInt(1, bodyHeight-appShellFrameLines-lipgloss.Height(header))
	}

	hostsActive := m.mode == tuiModeHosts
	showDetails := m.detailsVisibleAtWidth(shellWidth)

	listHeight := contentHeight
	detailsBlock := ""
	if showDetails {
		detailsHeight := maxInt(4, contentHeight/3)
		if detailsHeight+2 < contentHeight {
			listHeight = contentHeight - detailsHeight - 1
			detailsBlock = m.detailsPaneView(shellWidth, detailsHeight)
		}
	}

	var listBlock string
	if hostsActive {
		listBlock = sidebarSectionView(m.sectionTitle("HOSTS", len(m.filteredItems())), m.hostsPaneView(shellWidth, true, maxInt(1, listHeight-1)), shellWidth, true, listHeight)
	} else {
		listBlock = sidebarSectionView(m.sectionTitle("TUNNELS", len(m.filteredTunnelItems())), m.tunnelsPaneView(shellWidth, true, maxInt(1, listHeight-1)), shellWidth, true, listHeight)
	}

	parts := []string{header, listBlock}
	if detailsBlock != "" {
		parts = append(parts, subtleDivider(shellWidth), detailsBlock)
	}
	return appShellStyle.Width(shellWidth).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m tuiModel) appHeaderView(width int, showLogo bool) string {
	counts := mutedStyle.Render(fmt.Sprintf("%d hosts  ·  %d tunnels", len(m.names), len(m.tunnelNames)))
	icon := titleStyle.Render(appIconChar)
	name := titleStyle.Render(appName)
	dot := dividerStyle.Render(" · ")

	if width < compactLogoMinWidth {
		// Very narrow: icon + counts only.
		left := icon + "  " + name
		padding := width - lipgloss.Width(left) - lipgloss.Width(counts)
		if padding < 1 {
			return fitRow(appIconChar+" "+appName, width) + "\n" + subtleDivider(width)
		}
		return left + strings.Repeat(" ", padding) + counts + "\n" + subtleDivider(width)
	}

	leftBlock := icon + "  " + name
	if width >= headerVersionMinWidth {
		leftBlock += "  " + mutedStyle.Render("v"+Version)
	}
	leftBlock += dot + counts

	if showLogo && width >= headerTaglineMinWidth {
		tagline := mutedStyle.Render(appTagline)
		padding := width - lipgloss.Width(leftBlock) - lipgloss.Width(tagline)
		if padding >= 2 {
			return leftBlock + strings.Repeat(" ", padding) + tagline + "\n" + subtleDivider(width)
		}
	}

	padding := width - lipgloss.Width(leftBlock)
	if padding < 0 {
		padding = 0
	}
	return leftBlock + strings.Repeat(" ", padding) + "\n" + subtleDivider(width)
}

func (m tuiModel) detailsVisible() bool {
	return m.detailsVisibleAtWidth(m.currentDetailsWidth())
}

func (m tuiModel) detailsVisibleAtWidth(shellWidth int) bool {
	if m.showDetails {
		return true
	}
	if m.hideDetails {
		return false
	}
	if shellWidth > 0 && shellWidth < detailsMinWidth {
		return false
	}
	bodyHeight := m.mainBodyHeight()
	return bodyHeight == 0 || bodyHeight >= detailsMinBodyHeight
}

func (m tuiModel) currentDetailsWidth() int {
	if m.width == 0 {
		return appShellDefaultWidth - wideSidebarWidth(appShellDefaultWidth) - 3
	}
	contentWidth := maxInt(40, m.width-2)
	if m.width < tuiNarrowWidth {
		return maxInt(36, contentWidth-appShellChrome)
	}
	contentWidth = maxInt(80, contentWidth)
	shellWidth := maxInt(appShellMinWidth, contentWidth-appShellChrome)
	return shellWidth - wideSidebarWidth(shellWidth) - 3
}

func wideSidebarWidth(shellWidth int) int {
	return maxInt(28, minInt(42, shellWidth/3))
}

func (m tuiModel) sidebarView(width, height int) string {
	if height <= 0 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			sidebarSectionView(m.sectionTitle("HOSTS", len(m.filteredItems())), m.hostsPaneView(width, m.mode == tuiModeHosts), width, m.mode == tuiModeHosts, 0),
			subtleDivider(width),
			sidebarSectionView(m.sectionTitle("TUNNELS", len(m.filteredTunnelItems())), m.tunnelsPaneView(width, m.mode == tuiModeTunnels), width, m.mode == tuiModeTunnels, 0),
		)
	}

	gap := 1
	available := maxInt(2, height-gap)
	hostHeight := available / 2
	tunnelHeight := available - hostHeight
	if m.mode == tuiModeHosts && available >= 8 {
		hostHeight = available * 3 / 5
		tunnelHeight = available - hostHeight
	} else if m.mode == tuiModeTunnels && available >= 8 {
		tunnelHeight = available * 3 / 5
		hostHeight = available - tunnelHeight
	}

	hosts := sidebarSectionView(m.sectionTitle("HOSTS", len(m.filteredItems())), m.hostsPaneView(width, m.mode == tuiModeHosts, maxInt(1, hostHeight-1)), width, m.mode == tuiModeHosts, hostHeight)
	tunnels := sidebarSectionView(m.sectionTitle("TUNNELS", len(m.filteredTunnelItems())), m.tunnelsPaneView(width, m.mode == tuiModeTunnels, maxInt(1, tunnelHeight-1)), width, m.mode == tuiModeTunnels, tunnelHeight)
	return padBlockHeight(lipgloss.JoinVertical(lipgloss.Left, hosts, subtleDivider(width), tunnels), height)
}

func (m tuiModel) detailsPaneView(width, height int) string {
	body := m.detailsView(width)
	if height <= 0 {
		return lipgloss.JoinVertical(lipgloss.Left, detailsHeaderStyle.Render("DETAILS"), body)
	}
	bodyHeight := maxInt(1, height-1)
	body = clampLines(body, bodyHeight, 0)
	return sidebarSectionView("DETAILS", body, width, false, height)
}

func (m tuiModel) sectionTitle(title string, count int) string {
	return fmt.Sprintf("%s %d", title, count)
}

func sidebarSectionView(title, body string, width int, focused bool, height int) string {
	headerStyle := sectionHeaderStyle
	if focused {
		headerStyle = activeSectionHeaderStyle
	}
	header := headerStyle.Render(fitRow(title, width))
	if height == 1 {
		return header
	}
	if height > 1 {
		body = clampLines(body, height-1, 0)
		return padBlockHeight(lipgloss.JoinVertical(lipgloss.Left, header, body), height)
	}
	if body == "" {
		return header
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func subtleDivider(width int) string {
	return dividerStyle.Render(strings.Repeat("─", maxInt(1, width)))
}

func verticalDivider(height int) string {
	if height <= 0 {
		height = 1
	}
	lines := make([]string, height)
	for i := range lines {
		lines[i] = dividerStyle.Render("│")
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) filterRowView() string {
	if !m.filter.Focused() && m.filter.Value() == "" {
		return ""
	}
	return m.filter.View()
}

func (m tuiModel) panelFilterView(mode tuiMode) string {
	if m.mode != mode {
		return ""
	}
	return m.filterRowView()
}

func (m tuiModel) hostsPaneView(width int, focused bool, maxLines ...int) string {
	items := m.filteredItems()
	prefix := m.panelFilterView(tuiModeHosts)
	limit := optionalLineLimit(maxLines)
	if len(items) == 0 {
		message := "No saved hosts. Press a to add one."
		if m.filter.Value() != "" && m.mode == tuiModeHosts {
			message = "No matching hosts."
		}
		if prefix != "" {
			return clampLines(lipgloss.JoinVertical(lipgloss.Left, prefix, "", mutedStyle.Render(message)), limit, 0)
		}
		return clampLines(mutedStyle.Render(message), limit, 0)
	}

	lines := make([]string, 0, len(items)+4)
	selectedLine := -1
	if prefix != "" {
		lines = append(lines, prefix, "")
	}
	prevSection := "\x00"
	for i, item := range items {
		section := sectionLabel(item.group)
		if section != prevSection {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, subSectionStyle.Render(section))
			prevSection = section
		}
		if i == m.cursor {
			selectedLine = len(lines)
			if focused {
				line := hostRow(item, width-2, selectedTargetStyle)
				lines = append(lines, selectedStyle.Render(fitRow("> "+line, width)))
			} else {
				line := hostRow(item, width-2, mutedStyle)
				lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
			}
		} else {
			line := hostRow(item, width-2, mutedStyle)
			lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
		}
	}
	if !focused {
		selectedLine = 0
	}
	return clampLines(strings.Join(lines, "\n"), limit, selectedLine)
}

func (m tuiModel) tunnelsPaneView(width int, focused bool, maxLines ...int) string {
	items := m.filteredTunnelItems()
	prefix := m.panelFilterView(tuiModeTunnels)
	limit := optionalLineLimit(maxLines)
	if len(items) == 0 {
		message := "No saved tunnels. Press a to add one."
		if m.filter.Value() != "" && m.mode == tuiModeTunnels {
			message = "No matching tunnels."
		}
		if prefix != "" {
			return clampLines(lipgloss.JoinVertical(lipgloss.Left, prefix, "", mutedStyle.Render(message)), limit, 0)
		}
		return clampLines(mutedStyle.Render(message), limit, 0)
	}

	lines := make([]string, 0, len(items)+4)
	selectedLine := -1
	if prefix != "" {
		lines = append(lines, prefix, "")
	}
	prevSection := "\x00"
	for i, item := range items {
		section := sectionLabel(item.group)
		if section != prevSection {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, subSectionStyle.Render(section))
			prevSection = section
		}
		if i == m.tunnelCursor {
			selectedLine = len(lines)
			if focused {
				line := tunnelRow(item, width-2, selectedTargetStyle)
				lines = append(lines, selectedStyle.Render(fitRow("> "+line, width)))
			} else {
				line := tunnelRow(item, width-2, mutedStyle)
				lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
			}
		} else {
			line := tunnelRow(item, width-2, mutedStyle)
			lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
		}
	}
	if !focused {
		selectedLine = 0
	}
	return clampLines(strings.Join(lines, "\n"), limit, selectedLine)
}

func sectionLabel(group string) string {
	if strings.TrimSpace(group) == "" {
		return "ungrouped"
	}
	return group
}

func (m tuiModel) detailsView(width int) string {
	if m.mode == tuiModeTunnels {
		return m.tunnelDetailsView(width)
	}

	item, ok := m.selectedItem()
	if !ok {
		return mutedStyle.Render("Select or add a host.")
	}

	rows := []string{
		titleStyle.Render(truncate(item.name, width)),
		"",
		labelValue("Host", item.host.Host),
		labelValue("User", optionalValue(item.host.User)),
		labelValue("Port", strconv.Itoa(effectivePort(item.host))),
		labelValue("Identity file", optionalValue(item.host.IdentityFile)),
		labelValue("Group", optionalValue(item.host.Group)),
		labelValue("Password", passwordStatus(item.host)),
		"",
		mutedStyle.Render(wrapText(connectCommandString(item.host, buildSSHArgs(item.host)), width)),
		mutedStyle.Render(wrapText(sftpCommandString(item.host, buildSFTPArgs(item.host, "")), width)),
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) tunnelDetailsView(width int) string {
	item, ok := m.selectedTunnelItem()
	if !ok {
		return mutedStyle.Render("Select or add a tunnel.")
	}

	command := "missing SSH host"
	if host, ok := m.cfg.Hosts[item.tunnel.Host]; ok {
		if sshArgs, err := buildTunnelStartSSHArgs(item.name, host, item.tunnel); err == nil {
			command = tunnelCommandString(host, sshArgs)
		}
	}
	status := "stopped"
	if entry, running, _ := tunnelstate.Get(item.name); running {
		status = fmt.Sprintf("running pid %d", entry.PID)
	}
	rows := []string{
		titleStyle.Render(truncate(item.name, width)),
		"",
		labelValue("Status", status),
		labelValue("SSH host", item.tunnel.Host),
		labelValue("Type", item.tunnel.Type),
		labelValue("Group", optionalValue(item.tunnel.Group)),
		labelValue("Forward", formatTunnelForward(item.tunnel)),
		labelValue("Bind", optionalValue(item.tunnel.BindAddress)),
		"",
		mutedStyle.Render(wrapText(command, width)),
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) formView() string {
	title := "Add host"
	if m.editOld != "" {
		title = "Edit host"
	}
	lines := []string{titleStyle.Render(title), ""}
	labels := []string{"Name", "Host", "User", "Password", "Port", "Identity file", "Group"}
	hasReusableKeys := len(identityFileChoices(m.cfg.Hosts, m.editOld)) > 0
	hasGroups := len(groupNames(m.cfg)) > 0
	for i, input := range m.inputs {
		label := labels[i]
		if i == tuiFieldIdentityFile && hasReusableKeys {
			label += " " + mutedStyle.Render("(ctrl+k)")
		}
		if i == tuiFieldGroup && hasGroups {
			label += " " + mutedStyle.Render("(ctrl+g)")
		}
		if i == m.focus {
			label = "> " + label
		} else {
			label = "  " + label
		}
		lines = append(lines, labelStyle.Render(label), input.View())
	}
	helpText := "ctrl+k: reuse key  ctrl+g: choose group  tab/down: next  enter/ctrl+s: save  esc: cancel"
	lines = append(lines, "", mutedStyle.Render(helpText))
	return strings.Join(lines, "\n")
}

func (m tuiModel) tunnelFormView() string {
	title := "Add tunnel"
	if m.editTunnelOld != "" {
		title = "Edit tunnel"
	}
	lines := []string{titleStyle.Render(title), ""}
	labels := []string{"Name", "SSH host", "Type", "Bind address", "Local port", "Remote host", "Remote port", "Group"}
	hasHosts := len(m.cfg.Hosts) > 0
	hasGroups := len(groupNames(m.cfg)) > 0
	for i, input := range m.inputs {
		label := labels[i]
		if i == tuiTunnelFieldHost && hasHosts {
			label += " " + mutedStyle.Render("(ctrl+h)")
		}
		if i == tuiTunnelFieldGroup && hasGroups {
			label += " " + mutedStyle.Render("(ctrl+g)")
		}
		if i == m.focus {
			label = "> " + label
		} else {
			label = "  " + label
		}
		lines = append(lines, labelStyle.Render(label), input.View())
	}
	helpText := "type: local/remote/dynamic  ctrl+h: choose host  ctrl+g: choose group  tab/down: next  enter/ctrl+s: save  esc: cancel"
	lines = append(lines, "", mutedStyle.Render(helpText))
	return strings.Join(lines, "\n")
}

func (m tuiModel) keySelectView() string {
	width := maxInt(56, minInt(84, m.width-4))
	if m.width == 0 {
		width = 68
	}

	lines := []string{titleStyle.Render("Choose identity file"), ""}
	if len(m.keyPick) == 0 {
		lines = append(lines, mutedStyle.Render("No saved identity files."))
	} else {
		for i, choice := range m.keyPick {
			line := fitRow(identityFileChoiceLabel(choice), width-2)
			if i == m.keyCursor {
				lines = append(lines, selectedStyle.Render(fitRow("> "+line, width)))
			} else {
				lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
			}
		}
	}
	lines = append(lines, "", mutedStyle.Render("up/down: select  enter: use  esc: back"))
	return strings.Join(lines, "\n")
}

func (m tuiModel) hostSelectView() string {
	width := maxInt(56, minInt(84, m.width-4))
	if m.width == 0 {
		width = 68
	}

	lines := []string{titleStyle.Render("Choose SSH host"), ""}
	if len(m.hostPick) == 0 {
		lines = append(lines, mutedStyle.Render("No saved hosts."))
	} else {
		for i, name := range m.hostPick {
			line := fitRow(fmt.Sprintf("%s (%s)", name, formatListTarget(m.cfg.Hosts[name])), width-2)
			if i == m.hostCursor {
				lines = append(lines, selectedStyle.Render(fitRow("> "+line, width)))
			} else {
				lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
			}
		}
	}
	lines = append(lines, "", mutedStyle.Render("up/down: select  enter: use  esc: back"))
	return strings.Join(lines, "\n")
}

func (m tuiModel) groupSelectView() string {
	width := maxInt(56, minInt(84, m.width-4))
	if m.width == 0 {
		width = 68
	}

	lines := []string{titleStyle.Render("Choose group"), ""}
	if len(m.groupPick) == 0 {
		lines = append(lines, mutedStyle.Render("No saved groups."))
	} else {
		for i, name := range m.groupPick {
			line := fitRow(name, width-2)
			if i == m.groupCursor {
				lines = append(lines, selectedStyle.Render(fitRow("> "+line, width)))
			} else {
				lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
			}
		}
	}
	lines = append(lines, "", mutedStyle.Render("up/down: select  enter: use  esc: back"))
	return strings.Join(lines, "\n")
}

func (m tuiModel) paletteView() string {
	width := maxInt(56, minInt(84, m.width-4))
	if m.width == 0 {
		width = 72
	}

	lines := []string{titleStyle.Render("Command palette"), ""}
	if m.paletteQuery != "" {
		lines = append(lines, mutedStyle.Render("> "+m.paletteQuery), "")
	}

	items := m.filteredPaletteItems()
	if len(items) == 0 {
		lines = append(lines, mutedStyle.Render("No matching actions."))
	} else {
		for i, item := range items {
			if i == m.paletteCursor {
				line := paletteItemLabel(item, width-2, selectedTargetStyle)
				lines = append(lines, selectedStyle.Render(fitRow("> "+line, width)))
			} else {
				line := paletteItemLabel(item, width-2, mutedStyle)
				lines = append(lines, normalRowStyle.Render(fitRow("  "+line, width)))
			}
		}
	}
	lines = append(lines, "", mutedStyle.Render("up/down: select  enter: run  esc: back"))
	return strings.Join(lines, "\n")
}

func (m tuiModel) removeView() string {
	if m.mode == tuiModeTunnels {
		item, ok := m.selectedTunnelItem()
		name := ""
		if ok {
			name = item.name
		}
		text := fmt.Sprintf("Remove tunnel %q?\n\nPress y/enter to remove, n/esc to cancel.", name)
		return titleStyle.Render("Confirm remove") + "\n\n" + text
	}

	item, ok := m.selectedItem()
	name := ""
	if ok {
		name = item.name
	}
	text := fmt.Sprintf("Remove host %q?\n\nPress y/enter to remove, n/esc to cancel.", name)
	if ok {
		dependentTunnels := dependentTunnelNames(m.cfg, item.name)
		if len(dependentTunnels) > 0 {
			text = fmt.Sprintf("Host %q is used by tunnels: %s.\n\nRemove or edit those tunnels first.", name, strings.Join(dependentTunnels, ", "))
		}
	}
	return titleStyle.Render("Confirm remove") + "\n\n" + text
}

func (m tuiModel) previewView() string {
	title := m.outputTitle
	if title == "" {
		title = "SSH command"
		if m.mode == tuiModeTunnels {
			title = "Tunnel command"
		}
	}
	width := maxInt(32, m.width-8)
	if m.width == 0 {
		width = 100
	}
	return titleStyle.Render(title) + "\n\n" + formatCommandBlock(m.status, width) + "\n\n" + mutedStyle.Render("esc/p back  q quit")
}

func (m *tuiModel) reloadNames() {
	m.names = m.names[:0]
	for name := range m.cfg.Hosts {
		m.names = append(m.names, name)
	}
	sort.Strings(m.names)
	m.ensureCursor()
}

func (m *tuiModel) reloadTunnelNames() {
	m.tunnelNames = m.tunnelNames[:0]
	for name := range m.cfg.Tunnels {
		m.tunnelNames = append(m.tunnelNames, name)
	}
	sort.Strings(m.tunnelNames)
	m.ensureTunnelCursor()
}

func (m *tuiModel) toggleMode() {
	m.filter.Blur()
	m.filter.SetValue("")
	if m.mode == tuiModeHosts {
		m.mode = tuiModeTunnels
		m.filter.Placeholder = "filter tunnels"
		m.status = "Showing tunnels."
	} else {
		m.mode = tuiModeHosts
		m.filter.Placeholder = "filter hosts"
		m.status = "Showing hosts."
	}
	m.err = nil
	m.ensureActiveCursor()
}

func (m *tuiModel) ensureActiveCursor() {
	if m.mode == tuiModeTunnels {
		m.ensureTunnelCursor()
		return
	}
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

func (m *tuiModel) ensureTunnelCursor() {
	items := m.filteredTunnelItems()
	if len(items) == 0 {
		m.tunnelCursor = 0
		return
	}
	if m.tunnelCursor < 0 {
		m.tunnelCursor = 0
	}
	if m.tunnelCursor >= len(items) {
		m.tunnelCursor = len(items) - 1
	}
}

func (m *tuiModel) ensurePaletteCursor() {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		m.paletteCursor = 0
		return
	}
	if m.paletteCursor < 0 {
		m.paletteCursor = 0
	}
	if m.paletteCursor >= len(items) {
		m.paletteCursor = len(items) - 1
	}
}

func (m tuiModel) filteredItems() []tuiHostItem {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	matches := func(name string, host config.Host) bool {
		if query == "" {
			return true
		}
		haystack := strings.ToLower(strings.Join([]string{name, host.Host, host.User, host.IdentityFile, host.Group, formatListTarget(host)}, " "))
		return strings.Contains(haystack, query)
	}

	items := make([]tuiHostItem, 0, len(m.names))
	for _, name := range m.names {
		host := m.cfg.Hosts[name]
		if !matches(name, host) {
			continue
		}
		items = append(items, tuiHostItem{name: name, host: host, group: host.Group})
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftGroup := sectionLabel(items[i].group)
		rightGroup := sectionLabel(items[j].group)
		if leftGroup != rightGroup {
			if leftGroup == "ungrouped" {
				return false
			}
			if rightGroup == "ungrouped" {
				return true
			}
			return leftGroup < rightGroup
		}
		return items[i].name < items[j].name
	})
	return items
}

func (m tuiModel) filteredTunnelItems() []tuiTunnelItem {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	matches := func(name string, tunnel config.Tunnel) bool {
		if query == "" {
			return true
		}
		haystack := strings.ToLower(strings.Join([]string{name, tunnel.Host, tunnel.Type, tunnel.BindAddress, tunnel.RemoteHost, tunnel.Group, formatTunnelForward(tunnel)}, " "))
		return strings.Contains(haystack, query)
	}

	items := make([]tuiTunnelItem, 0, len(m.tunnelNames))
	for _, name := range m.tunnelNames {
		tunnel := m.cfg.Tunnels[name]
		if !matches(name, tunnel) {
			continue
		}
		items = append(items, tuiTunnelItem{name: name, tunnel: tunnel, group: tunnel.Group})
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftGroup := sectionLabel(items[i].group)
		rightGroup := sectionLabel(items[j].group)
		if leftGroup != rightGroup {
			if leftGroup == "ungrouped" {
				return false
			}
			if rightGroup == "ungrouped" {
				return true
			}
			return leftGroup < rightGroup
		}
		return items[i].name < items[j].name
	})
	return items
}

func (m tuiModel) filteredPaletteItems() []tuiPaletteItem {
	query := strings.TrimSpace(m.paletteQuery)
	if query == "" {
		return append([]tuiPaletteItem(nil), m.palette...)
	}

	items := make([]tuiPaletteItem, 0, len(m.palette))
	for _, item := range m.palette {
		if fuzzyPaletteMatch(query, item) {
			items = append(items, item)
		}
	}
	return items
}

func fuzzyPaletteMatch(query string, item tuiPaletteItem) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.TrimSpace(item.title + " " + item.hint))
	if strings.Contains(haystack, query) {
		return true
	}

	next := 0
	queryRunes := []rune(query)
	for _, r := range haystack {
		if r == queryRunes[next] {
			next++
			if next == len(queryRunes) {
				return true
			}
		}
	}
	return false
}

func (m tuiModel) selectedItem() (tuiHostItem, bool) {
	items := m.filteredItems()
	if len(items) == 0 || m.cursor < 0 || m.cursor >= len(items) {
		return tuiHostItem{}, false
	}
	return items[m.cursor], true
}

func (m tuiModel) selectedTunnelItem() (tuiTunnelItem, bool) {
	items := m.filteredTunnelItems()
	if len(items) == 0 || m.tunnelCursor < 0 || m.tunnelCursor >= len(items) {
		return tuiTunnelItem{}, false
	}
	return items[m.tunnelCursor], true
}

func (m tuiModel) focusFilter() (tea.Model, tea.Cmd) {
	cmd := m.filter.Focus()
	if m.mode == tuiModeTunnels {
		m.status = "Filtering tunnels."
	} else {
		m.status = "Filtering hosts."
	}
	m.err = nil
	return m, cmd
}

func (m tuiModel) runSelectedConnect() (tea.Model, tea.Cmd) {
	if m.mode == tuiModeTunnels {
		item, ok := m.selectedTunnelItem()
		if !ok {
			m.screen = tuiScreenMain
			m.setError(errors.New("no tunnel selected"))
			return m, nil
		}
		if _, running, err := tunnelstate.Get(item.name); err != nil {
			m.screen = tuiScreenMain
			m.setError(err)
			return m, nil
		} else if !running {
			if host, ok := m.cfg.Hosts[item.tunnel.Host]; ok && host.Password == "" {
				m.tunnelName = item.name
				return m, tea.Quit
			}
		}
		if err := m.toggleTunnel(item.name, item.tunnel); err != nil {
			m.screen = tuiScreenMain
			m.setError(err)
		}
		m.screen = tuiScreenMain
		return m, nil
	}

	item, ok := m.selectedItem()
	if !ok {
		m.screen = tuiScreenMain
		m.setError(errors.New("no host selected"))
		return m, nil
	}
	m.connectName = item.name
	return m, tea.Quit
}

func (m tuiModel) runSelectedSFTP() (tea.Model, tea.Cmd) {
	name, _, ok, err := m.selectedSFTPHost()
	if err != nil {
		m.screen = tuiScreenMain
		m.setError(err)
		return m, nil
	}
	if !ok {
		m.screen = tuiScreenMain
		m.setError(errors.New("no host selected"))
		return m, nil
	}
	m.sftpName = name
	return m, tea.Quit
}

func (m tuiModel) runSelectedSFTPPreview() (tea.Model, tea.Cmd) {
	_, host, ok, err := m.selectedSFTPHost()
	if err != nil {
		m.screen = tuiScreenMain
		m.setError(err)
		return m, nil
	}
	if !ok {
		m.screen = tuiScreenMain
		m.setError(errors.New("no host selected"))
		return m, nil
	}
	m.status = sftpCommandString(host, buildSFTPArgs(host, ""))
	m.outputTitle = "SFTP command"
	m.err = nil
	m.screen = tuiScreenPreview
	return m, nil
}

func (m tuiModel) selectedSFTPHost() (string, config.Host, bool, error) {
	if m.mode == tuiModeTunnels {
		item, ok := m.selectedTunnelItem()
		if !ok {
			return "", config.Host{}, false, nil
		}
		host, ok := m.cfg.Hosts[item.tunnel.Host]
		if !ok {
			return "", config.Host{}, false, fmt.Errorf("host %q for tunnel %q not found", item.tunnel.Host, item.name)
		}
		return item.tunnel.Host, host, true, nil
	}

	item, ok := m.selectedItem()
	if !ok {
		return "", config.Host{}, false, nil
	}
	return item.name, item.host, true, nil
}

func (m tuiModel) runSelectedPreview() (tea.Model, tea.Cmd) {
	if m.mode == tuiModeTunnels {
		item, ok := m.selectedTunnelItem()
		if !ok {
			m.screen = tuiScreenMain
			m.setError(errors.New("no tunnel selected"))
			return m, nil
		}
		host, ok := m.cfg.Hosts[item.tunnel.Host]
		if !ok {
			m.screen = tuiScreenMain
			m.setError(fmt.Errorf("host %q for tunnel %q not found", item.tunnel.Host, item.name))
			return m, nil
		}
		sshArgs, err := buildTunnelStartSSHArgs(item.name, host, item.tunnel)
		if err != nil {
			m.screen = tuiScreenMain
			m.setError(err)
			return m, nil
		}
		m.status = tunnelCommandString(host, sshArgs)
		m.outputTitle = "Tunnel command"
		m.err = nil
		m.screen = tuiScreenPreview
		return m, nil
	}

	item, ok := m.selectedItem()
	if !ok {
		m.screen = tuiScreenMain
		m.setError(errors.New("no host selected"))
		return m, nil
	}
	m.status = connectCommandString(item.host, buildSSHArgs(item.host))
	m.outputTitle = "SSH command"
	m.err = nil
	m.screen = tuiScreenPreview
	return m, nil
}

func (m tuiModel) openPalette() (tea.Model, tea.Cmd) {
	m.palette = m.paletteItems()
	m.paletteCursor = 0
	m.paletteQuery = ""
	m.screen = tuiScreenPalette
	m.status = ""
	m.err = nil
	return m, nil
}

func (m tuiModel) paletteItems() []tuiPaletteItem {
	items := make([]tuiPaletteItem, 0, 10)
	if m.mode == tuiModeTunnels {
		if item, ok := m.selectedTunnelItem(); ok {
			items = append(items,
				tuiPaletteItem{title: "Start/stop tunnel", hint: item.name, action: tuiPaletteConnect},
				tuiPaletteItem{title: "Open host SFTP", hint: item.tunnel.Host, action: tuiPaletteSFTP},
				tuiPaletteItem{title: "Print tunnel command", hint: item.name, action: tuiPalettePreview},
				tuiPaletteItem{title: "Print host SFTP command", hint: item.tunnel.Host, action: tuiPaletteSFTPPreview},
				tuiPaletteItem{title: "Edit tunnel", hint: item.name, action: tuiPaletteEdit},
				tuiPaletteItem{title: "Remove tunnel", hint: item.name, action: tuiPaletteRemove},
			)
		}
		items = append(items,
			tuiPaletteItem{title: "Add tunnel", hint: "new tunnel", action: tuiPaletteAddTunnel},
			tuiPaletteItem{title: "Add host", hint: "new host", action: tuiPaletteAddHost},
			tuiPaletteItem{title: "Switch to hosts", hint: "tab", action: tuiPaletteSwitch},
			tuiPaletteItem{title: "Filter tunnels", hint: "/", action: tuiPaletteFilter},
			tuiPaletteItem{title: "Run doctor", hint: "diagnostics", action: tuiPaletteDoctor},
		)
		return items
	}

	if item, ok := m.selectedItem(); ok {
		items = append(items,
			tuiPaletteItem{title: "Connect host", hint: item.name, action: tuiPaletteConnect},
			tuiPaletteItem{title: "Open SFTP", hint: item.name, action: tuiPaletteSFTP},
			tuiPaletteItem{title: "Print SSH command", hint: item.name, action: tuiPalettePreview},
			tuiPaletteItem{title: "Print SFTP command", hint: item.name, action: tuiPaletteSFTPPreview},
			tuiPaletteItem{title: "Edit host", hint: item.name, action: tuiPaletteEdit},
			tuiPaletteItem{title: "Remove host", hint: item.name, action: tuiPaletteRemove},
		)
	}
	items = append(items,
		tuiPaletteItem{title: "Add host", hint: "new host", action: tuiPaletteAddHost},
		tuiPaletteItem{title: "Add tunnel", hint: "new tunnel", action: tuiPaletteAddTunnel},
		tuiPaletteItem{title: "Switch to tunnels", hint: "tab", action: tuiPaletteSwitch},
		tuiPaletteItem{title: "Filter hosts", hint: "/", action: tuiPaletteFilter},
		tuiPaletteItem{title: "Run doctor", hint: "diagnostics", action: tuiPaletteDoctor},
	)
	return items
}

func (m tuiModel) runPaletteAction(action tuiPaletteAction) (tea.Model, tea.Cmd) {
	m.palette = nil
	switch action {
	case tuiPaletteConnect:
		return m.runSelectedConnect()
	case tuiPalettePreview:
		return m.runSelectedPreview()
	case tuiPaletteSFTP:
		return m.runSelectedSFTP()
	case tuiPaletteSFTPPreview:
		return m.runSelectedSFTPPreview()
	case tuiPaletteAddHost:
		return m.openForm("", config.Host{})
	case tuiPaletteAddTunnel:
		return m.openTunnelForm("", config.Tunnel{Type: "local"})
	case tuiPaletteEdit:
		if m.mode == tuiModeTunnels {
			item, ok := m.selectedTunnelItem()
			if !ok {
				m.screen = tuiScreenMain
				m.setError(errors.New("no tunnel selected"))
				return m, nil
			}
			return m.openTunnelForm(item.name, item.tunnel)
		}
		item, ok := m.selectedItem()
		if !ok {
			m.screen = tuiScreenMain
			m.setError(errors.New("no host selected"))
			return m, nil
		}
		return m.openForm(item.name, item.host)
	case tuiPaletteRemove:
		m.screen = tuiScreenRemove
		m.status = ""
		m.err = nil
		return m, nil
	case tuiPaletteSwitch:
		m.screen = tuiScreenMain
		m.toggleMode()
		return m, nil
	case tuiPaletteFilter:
		m.screen = tuiScreenMain
		return m.focusFilter()
	case tuiPaletteDoctor:
		m.status = buildDoctorReport()
		m.outputTitle = "Doctor"
		m.err = nil
		m.screen = tuiScreenPreview
		return m, nil
	default:
		m.screen = tuiScreenMain
		return m, nil
	}
}

func (m tuiModel) openForm(name string, host config.Host) (tea.Model, tea.Cmd) {
	inputs := make([]textinput.Model, tuiFieldCount)
	placeholders := []string{"prod", "192.0.2.10", "deploy", "optional", "22", "~/.ssh/id_ed25519", "work"}
	values := []string{name, host.Host, host.User, host.Password, "", host.IdentityFile, host.Group}
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
	m.editTunnelOld = ""
	m.keyPick = nil
	m.keyCursor = 0
	m.groupPick = nil
	m.groupCursor = 0
	m.screen = tuiScreenForm
	m.status = ""
	m.err = nil
	return m, cmd
}

func (m tuiModel) openTunnelForm(name string, tunnel config.Tunnel) (tea.Model, tea.Cmd) {
	inputs := make([]textinput.Model, tuiTunnelFieldCount)
	placeholders := []string{"db", "prod", "local", "127.0.0.1", "5432", "127.0.0.1", "5432", "work"}
	values := []string{name, tunnel.Host, tunnel.Type, tunnel.BindAddress, "", tunnel.RemoteHost, "", tunnel.Group}
	if tunnel.Type == "" {
		values[tuiTunnelFieldType] = "local"
	}
	if tunnel.LocalPort != 0 {
		values[tuiTunnelFieldLocalPort] = strconv.Itoa(tunnel.LocalPort)
	}
	if tunnel.RemotePort != 0 {
		values[tuiTunnelFieldRemotePort] = strconv.Itoa(tunnel.RemotePort)
	}

	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Placeholder = placeholders[i]
		inputs[i].Width = maxInt(18, minInt(52, m.width-24))
		inputs[i].SetValue(values[i])
	}
	cmd := inputs[0].Focus()

	m.inputs = inputs
	m.focus = 0
	m.editOld = ""
	m.editTunnelOld = name
	m.keyPick = nil
	m.hostPick = nil
	m.hostCursor = 0
	m.groupPick = nil
	m.groupCursor = 0
	m.screen = tuiScreenTunnelForm
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

func (m *tuiModel) openHostSelect() bool {
	names := make([]string, 0, len(m.cfg.Hosts))
	for name := range m.cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return false
	}

	current := strings.TrimSpace(m.inputs[tuiTunnelFieldHost].Value())
	cursor := 0
	for i, name := range names {
		if name == current {
			cursor = i
			break
		}
	}

	m.hostPick = names
	m.hostCursor = cursor
	m.screen = tuiScreenHostSelect
	return true
}

func (m *tuiModel) openGroupSelect(returnScreen tuiScreen, field int) bool {
	names := groupNames(m.cfg)
	if len(names) == 0 {
		return false
	}

	current := ""
	if field >= 0 && field < len(m.inputs) {
		current = strings.TrimSpace(m.inputs[field].Value())
	}
	cursor := 0
	for i, name := range names {
		if name == current {
			cursor = i
			break
		}
	}

	m.groupPick = names
	m.groupCursor = cursor
	m.groupReturn = returnScreen
	m.groupField = field
	m.screen = tuiScreenGroupSelect
	return true
}

func (m *tuiModel) saveForm() error {
	name := strings.TrimSpace(m.inputs[tuiFieldName].Value())
	host := config.Host{
		Host:         strings.TrimSpace(m.inputs[tuiFieldHost].Value()),
		User:         strings.TrimSpace(m.inputs[tuiFieldUser].Value()),
		Password:     m.inputs[tuiFieldPassword].Value(),
		IdentityFile: strings.TrimSpace(m.inputs[tuiFieldIdentityFile].Value()),
		Group:        strings.TrimSpace(m.inputs[tuiFieldGroup].Value()),
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
		renameTunnelHostReferences(&m.cfg, m.editOld, name)
	}

	m.cfg.Hosts[name] = host
	ensureGroup(&m.cfg, host.Group)
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
	m.hostPick = nil
	m.groupPick = nil
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

func (m *tuiModel) saveTunnelForm() error {
	name := strings.TrimSpace(m.inputs[tuiTunnelFieldName].Value())
	tunnel := config.Tunnel{
		Host:        strings.TrimSpace(m.inputs[tuiTunnelFieldHost].Value()),
		Type:        strings.ToLower(strings.TrimSpace(m.inputs[tuiTunnelFieldType].Value())),
		BindAddress: strings.TrimSpace(m.inputs[tuiTunnelFieldBind].Value()),
		RemoteHost:  strings.TrimSpace(m.inputs[tuiTunnelFieldRemoteHost].Value()),
		Group:       strings.TrimSpace(m.inputs[tuiTunnelFieldGroup].Value()),
	}
	if tunnel.Type == "" {
		tunnel.Type = "local"
	}
	if name == "" {
		return errors.New("tunnel name cannot be empty")
	}
	localPortText := strings.TrimSpace(m.inputs[tuiTunnelFieldLocalPort].Value())
	if localPortText != "" {
		port, err := strconv.Atoi(localPortText)
		if err != nil {
			return fmt.Errorf("invalid local port %q", localPortText)
		}
		tunnel.LocalPort = port
	}
	remotePortText := strings.TrimSpace(m.inputs[tuiTunnelFieldRemotePort].Value())
	if remotePortText != "" {
		port, err := strconv.Atoi(remotePortText)
		if err != nil {
			return fmt.Errorf("invalid remote port %q", remotePortText)
		}
		tunnel.RemotePort = port
	}
	if tunnel.Type == "dynamic" {
		tunnel.RemoteHost = ""
		tunnel.RemotePort = 0
	}
	if err := validateTunnel(tunnel, m.cfg.Hosts); err != nil {
		return err
	}
	if m.editTunnelOld == "" {
		if _, exists := m.cfg.Tunnels[name]; exists {
			return fmt.Errorf("tunnel %q already exists", name)
		}
	} else if name != m.editTunnelOld {
		if _, exists := m.cfg.Tunnels[name]; exists {
			return fmt.Errorf("tunnel %q already exists", name)
		}
		delete(m.cfg.Tunnels, m.editTunnelOld)
	}

	m.cfg.Tunnels[name] = tunnel
	ensureGroup(&m.cfg, tunnel.Group)
	if err := config.Save(m.path, m.cfg); err != nil {
		return err
	}
	m.reloadTunnelNames()
	for i, existing := range m.filteredTunnelItems() {
		if existing.name == name {
			m.tunnelCursor = i
			break
		}
	}
	m.screen = tuiScreenMain
	m.inputs = nil
	m.hostPick = nil
	m.groupPick = nil
	action := "Added"
	if m.editTunnelOld != "" {
		action = "Updated"
	}
	m.status = fmt.Sprintf("%s tunnel %q", action, name)
	m.err = nil
	return nil
}

func (m *tuiModel) toggleTunnel(name string, tunnel config.Tunnel) error {
	if entry, running, err := tunnelstate.Get(name); err != nil {
		return err
	} else if running {
		host, ok := m.cfg.Hosts[tunnel.Host]
		if !ok {
			return fmt.Errorf("host %q for tunnel %q not found", tunnel.Host, name)
		}
		stopped, _, err := stopTunnelByName(name, host)
		if err != nil {
			return err
		}
		if stopped {
			m.status = fmt.Sprintf("Stopped tunnel %q with pid %d", name, entry.PID)
		} else {
			m.status = fmt.Sprintf("Tunnel %q is not running", name)
		}
		m.err = nil
		return nil
	}

	host, ok := m.cfg.Hosts[tunnel.Host]
	if !ok {
		return fmt.Errorf("host %q for tunnel %q not found", tunnel.Host, name)
	}
	if err := validateTunnel(tunnel, m.cfg.Hosts); err != nil {
		return err
	}
	pid, err := startTunnelProcess(name, host, tunnel, false)
	if err != nil {
		return err
	}
	m.status = fmt.Sprintf("Started tunnel %q in background with pid %d", name, pid)
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

func paletteItemLabel(item tuiPaletteItem, width int, hintStyle lipgloss.Style) string {
	if strings.TrimSpace(item.hint) == "" {
		return truncate(item.title, width)
	}
	if width <= 0 {
		return ""
	}
	plain := item.title + "  " + item.hint
	if lipgloss.Width(plain) <= width {
		return item.title + "  " + hintStyle.Render(item.hint)
	}
	hintWidth := minInt(24, width/2)
	titleWidth := width - hintWidth - 2
	if titleWidth >= 10 {
		return truncate(item.title, titleWidth) + "  " + hintStyle.Render(truncate(item.hint, hintWidth))
	}
	return truncate(plain, width)
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

func tunnelRow(item tuiTunnelItem, width int, targetStyle lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	name := item.name
	target := tunnelStatusLabel(item.name) + "  " + formatTunnelForward(item.tunnel)
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

func tunnelStatusLabel(name string) string {
	if _, running, _ := tunnelstate.Get(name); running {
		return tunnelRunningStatusStyle.Render("● running")
	}
	return tunnelStoppedStatusStyle.Render("○ stopped")
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
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	return prefixByWidth(value, width-3) + "..."
}

func prefixByWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	lastGood := 0
	for i := range value {
		if i == 0 {
			continue
		}
		if lipgloss.Width(value[:i]) > width {
			return value[:lastGood]
		}
		lastGood = i
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return value[:lastGood]
}

func wrapText(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}

	words := strings.Fields(value)
	if len(words) == 0 {
		return wrapLongToken(value, width)
	}

	var lines []string
	current := ""
	for _, word := range words {
		for lipgloss.Width(word) > width {
			piece, rest := splitByWidth(word, width)
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			lines = append(lines, piece)
			word = rest
		}
		if current == "" {
			current = word
			continue
		}
		if lipgloss.Width(current)+1+lipgloss.Width(word) <= width {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		current = word
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}

func formatCommandBlock(command string, width int) string {
	sourceLines := strings.Split(command, "\n")
	if len(sourceLines) == 1 && lipgloss.Width(command) <= width {
		return command
	}

	lines := make([]string, 0, len(sourceLines))
	for _, sourceLine := range sourceLines {
		wrapped := strings.Split(wrapText(sourceLine, width), "\n")
		for i, line := range wrapped {
			if i > 0 {
				line = "  " + line
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func trimToHeight(value string, height int) string {
	if height <= 0 {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= height {
		return value
	}
	return strings.Join(lines[:height], "\n")
}

func optionalLineLimit(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return values[0]
}

func clampLines(value string, maxLines, focusLine int) string {
	if maxLines <= 0 {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= maxLines {
		return value
	}
	if focusLine < 0 {
		focusLine = 0
	}
	if focusLine >= len(lines) {
		focusLine = len(lines) - 1
	}

	start := focusLine - maxLines/2
	if start < 0 {
		start = 0
	}
	if start+maxLines > len(lines) {
		start = len(lines) - maxLines
	}

	out := append([]string(nil), lines[start:start+maxLines]...)
	if maxLines >= 3 {
		if start > 0 {
			out[0] = mutedStyle.Render("...")
		}
		if start+maxLines < len(lines) {
			out[len(out)-1] = mutedStyle.Render("...")
		}
	}
	return strings.Join(out, "\n")
}

func padBlockHeight(value string, height int) string {
	if height <= 0 {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func wrapLongToken(value string, width int) string {
	var lines []string
	for lipgloss.Width(value) > width {
		piece, rest := splitByWidth(value, width)
		lines = append(lines, piece)
		value = rest
	}
	if value != "" {
		lines = append(lines, value)
	}
	return strings.Join(lines, "\n")
}

func splitByWidth(value string, width int) (string, string) {
	if width <= 0 {
		return "", value
	}
	lastGood := 0
	for i := range value {
		if i == 0 {
			continue
		}
		if lipgloss.Width(value[:i]) > width {
			if lastGood == 0 {
				return value[:i], value[i:]
			}
			return value[:lastGood], value[lastGood:]
		}
		lastGood = i
	}
	if lipgloss.Width(value) > width {
		if lastGood == 0 {
			return value, ""
		}
		return value[:lastGood], value[lastGood:]
	}
	return value, ""
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
	appIconChar = "▣"
	appName     = "sshuttlebox"
	appTagline  = "SSH hosts & tunnels, in your terminal"

	detailsMinBodyHeight     = 14
	detailsMinWidth          = 58
	compactLogoMinWidth      = 44
	compactLogoMinBodyHeight = 14
	headerTaglineMinWidth    = 86
	headerVersionMinWidth    = 50
	appShellMinWidth         = 76
	appShellDefaultWidth     = 110
	appShellChrome           = 0
	appShellFrameLines       = 2
	tuiNarrowWidth           = 80
)

var (
	appShellStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("236"))
	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("71"))
	activeSectionHeaderStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("40"))
	detailsHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("71"))
	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("236"))
	subSectionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("71")).
			Bold(true)
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("40"))
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("194")).
			Background(lipgloss.Color("22"))
	selectedTargetStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("150")).
				Background(lipgloss.Color("22"))
	normalRowStyle = lipgloss.NewStyle()
	mutedStyle     = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("71"))
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("40"))
	tunnelRunningStatusStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("40"))
	tunnelStoppedStatusStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("241"))
)
