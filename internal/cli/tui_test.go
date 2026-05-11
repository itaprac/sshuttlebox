package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/itaprac/sshuttlebox/internal/config"
)

func TestTUIModelLoadsHostsSorted(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example"})
	addHost(t, "dev", config.Host{Host: "dev.example"})
	addHost(t, "staging", config.Host{Host: "staging.example"})

	model := newTUIModel()
	if !reflect.DeepEqual(model.names, []string{"dev", "prod", "staging"}) {
		t.Fatalf("names = %#v", model.names)
	}
}

func TestTUIFormAddsEditsAndRemovesHost(t *testing.T) {
	home := withTempHome(t)
	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}

	model := newTUIModelWithState(path, config.Default(), nil)
	model = openTUIForm(t, model, "", config.Host{})
	setTUIFormValues(&model, "prod", "192.0.2.10", "deploy", "secret", "2222", filepath.Join(home, "missing_key"))
	if err := model.saveForm(); err != nil {
		t.Fatalf("save add form: %v", err)
	}
	assertHost(t, "prod", config.Host{
		Host:         "192.0.2.10",
		User:         "deploy",
		Password:     "secret",
		Port:         2222,
		IdentityFile: filepath.Join(home, "missing_key"),
	})
	if !strings.Contains(model.status, "Warning: identity file does not exist") {
		t.Fatalf("expected identity-file warning in status, got %q", model.status)
	}

	model = openTUIForm(t, model, "prod", model.cfg.Hosts["prod"])
	setTUIFormValues(&model, "staging", "staging.example", "ops", "", "2200", "")
	if err := model.saveForm(); err != nil {
		t.Fatalf("save edit form: %v", err)
	}
	assertHostMissing(t, "prod")
	assertHost(t, "staging", config.Host{Host: "staging.example", User: "ops", Port: 2200})

	model.screen = tuiScreenRemove
	model.cursor = 0
	updated, _ := model.updateRemove(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	assertHostMissing(t, "staging")
	if !strings.Contains(model.status, "Removed host \"staging\"") {
		t.Fatalf("unexpected remove status %q", model.status)
	}
}

func TestTUIFormValidation(t *testing.T) {
	withTempHome(t)
	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg := config.Default()
	cfg.Hosts["prod"] = config.Host{Host: "prod.example"}
	cfg.Hosts["dev"] = config.Host{Host: "dev.example"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	model := newTUIModelWithState(path, cfg, nil)

	model = openTUIForm(t, model, "", config.Host{})
	setTUIFormValues(&model, "", "example.com", "", "", "", "")
	assertTUIFormError(t, model.saveForm(), "host name cannot be empty")

	setTUIFormValues(&model, "new", "", "", "", "", "")
	assertTUIFormError(t, model.saveForm(), "SSH host cannot be empty")

	setTUIFormValues(&model, "new", "example.com", "", "", "70000", "")
	assertTUIFormError(t, model.saveForm(), "invalid port 70000")

	setTUIFormValues(&model, "prod", "example.com", "", "", "", "")
	assertTUIFormError(t, model.saveForm(), "host \"prod\" already exists")

	model = openTUIForm(t, model, "prod", cfg.Hosts["prod"])
	setTUIFormValues(&model, "dev", "prod.example", "", "", "", "")
	assertTUIFormError(t, model.saveForm(), "host \"dev\" already exists")
}

func TestTUIFormCanChooseExistingIdentityFile(t *testing.T) {
	withTempHome(t)
	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg := config.Default()
	cfg.Hosts["bastion"] = config.Host{Host: "bastion.example", IdentityFile: "~/.ssh/bastion_key"}
	cfg.Hosts["ops"] = config.Host{Host: "ops.example", IdentityFile: "~/.ssh/ops_key"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	model := newTUIModelWithState(path, cfg, nil)
	model = openTUIForm(t, model, "", config.Host{})

	updated, _ := model.updateForm(tea.KeyMsg{Type: tea.KeyCtrlK})
	model = updated.(tuiModel)
	if model.screen != tuiScreenKeySelect {
		t.Fatalf("screen = %v, want key select", model.screen)
	}
	if len(model.keyPick) != 2 {
		t.Fatalf("key choices = %d, want 2", len(model.keyPick))
	}

	updated, _ = model.updateKeySelect(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if got := model.inputs[tuiFieldIdentityFile].Value(); got != "~/.ssh/bastion_key" {
		t.Fatalf("identity file after first selection = %q, want bastion key", got)
	}
	if model.screen != tuiScreenForm {
		t.Fatalf("screen after selection = %v, want form", model.screen)
	}

	updated, _ = model.updateForm(tea.KeyMsg{Type: tea.KeyCtrlK})
	model = updated.(tuiModel)
	updated, _ = model.updateKeySelect(keyMsg(tea.KeyDown))
	model = updated.(tuiModel)
	updated, _ = model.updateKeySelect(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if got := model.inputs[tuiFieldIdentityFile].Value(); got != "~/.ssh/ops_key" {
		t.Fatalf("identity file after second selection = %q, want ops key", got)
	}
}

func TestTUITunnelFormCanChooseExistingHost(t *testing.T) {
	withTempHome(t)
	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg := config.Default()
	cfg.Hosts["bastion"] = config.Host{Host: "bastion.example", User: "deploy"}
	cfg.Hosts["prod"] = config.Host{Host: "prod.example", User: "deploy"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	model := newTUIModelWithState(path, cfg, nil)
	model = openTUITunnelForm(t, model, "", config.Tunnel{})

	updated, _ := model.updateTunnelForm(tea.KeyMsg{Type: tea.KeyCtrlH})
	model = updated.(tuiModel)
	if model.screen != tuiScreenHostSelect {
		t.Fatalf("screen = %v, want host select", model.screen)
	}
	if len(model.hostPick) != 2 {
		t.Fatalf("host choices = %d, want 2", len(model.hostPick))
	}

	updated, _ = model.updateHostSelect(keyMsg(tea.KeyDown))
	model = updated.(tuiModel)
	updated, _ = model.updateHostSelect(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if got := model.inputs[tuiTunnelFieldHost].Value(); got != "prod" {
		t.Fatalf("SSH host after selection = %q, want prod", got)
	}
	if model.screen != tuiScreenTunnelForm {
		t.Fatalf("screen after selection = %v, want tunnel form", model.screen)
	}
}

func TestTUIFormsCanChooseExistingGroup(t *testing.T) {
	withTempHome(t)
	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg := config.Default()
	cfg.Groups["ops"] = config.Group{Name: "ops"}
	cfg.Groups["work"] = config.Group{Name: "work"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	model := newTUIModelWithState(path, cfg, nil)
	model = openTUIForm(t, model, "", config.Host{})
	updated, _ := model.updateForm(tea.KeyMsg{Type: tea.KeyCtrlG})
	model = updated.(tuiModel)
	if model.screen != tuiScreenGroupSelect {
		t.Fatalf("screen = %v, want group select", model.screen)
	}
	if len(model.groupPick) != 2 {
		t.Fatalf("group choices = %d, want 2", len(model.groupPick))
	}
	updated, _ = model.updateGroupSelect(keyMsg(tea.KeyDown))
	model = updated.(tuiModel)
	updated, _ = model.updateGroupSelect(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if got := model.inputs[tuiFieldGroup].Value(); got != "work" {
		t.Fatalf("host group after selection = %q, want work", got)
	}
	if model.screen != tuiScreenForm {
		t.Fatalf("screen after host group selection = %v, want host form", model.screen)
	}

	model = openTUITunnelForm(t, model, "", config.Tunnel{})
	updated, _ = model.updateTunnelForm(tea.KeyMsg{Type: tea.KeyCtrlG})
	model = updated.(tuiModel)
	updated, _ = model.updateGroupSelect(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if got := model.inputs[tuiTunnelFieldGroup].Value(); got != "ops" {
		t.Fatalf("tunnel group after selection = %q, want ops", got)
	}
	if model.screen != tuiScreenTunnelForm {
		t.Fatalf("screen after tunnel group selection = %v, want tunnel form", model.screen)
	}
}

func TestTUIFilterNarrowsHostsAndAllowsQInQuery(t *testing.T) {
	model := newTUIModelWithState("", config.Config{
		Version: 1,
		Hosts: map[string]config.Host{
			"prod": {Host: "prod.example"},
			"qa":   {Host: "qa.example"},
		},
	}, nil)

	updated, _ := model.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(tuiModel)
	if !model.filter.Focused() {
		t.Fatalf("filter should be focused after slash")
	}

	updated, cmd := model.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	model = updated.(tuiModel)
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatalf("typing q in filter should not quit")
		}
	}
	if !model.filter.Focused() {
		t.Fatalf("typing q in filter should not quit")
	}
	if got := model.filter.Value(); got != "q" {
		t.Fatalf("filter value = %q, want q", got)
	}
	items := model.filteredItems()
	if len(items) != 1 || items[0].name != "qa" {
		t.Fatalf("filtered items = %+v, want qa only", items)
	}
	hosts := model.hostsPaneView(60, true)
	if !strings.Contains(hosts, "/ q") {
		t.Fatalf("hosts pane should render active filter inside panel: %q", hosts)
	}
	tunnels := model.tunnelsPaneView(60, false)
	if strings.Contains(tunnels, "/ q") {
		t.Fatalf("inactive tunnels pane should not render host filter: %q", tunnels)
	}
}

func TestTUIFilterRendersInsideTunnelsPane(t *testing.T) {
	model := newTUIModelWithState("", config.Config{
		Version: 1,
		Hosts: map[string]config.Host{
			"prod": {Host: "prod.example"},
		},
		Tunnels: map[string]config.Tunnel{
			"db":    {Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432},
			"socks": {Host: "prod", Type: "dynamic", LocalPort: 1080},
		},
	}, nil)
	model.toggleMode()

	updated, _ := model.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(tuiModel)
	updated, _ = model.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model = updated.(tuiModel)

	hosts := model.hostsPaneView(60, false)
	if strings.Contains(hosts, "/ s") {
		t.Fatalf("inactive hosts pane should not render tunnel filter: %q", hosts)
	}
	tunnels := model.tunnelsPaneView(60, true)
	if !strings.Contains(tunnels, "/ s") {
		t.Fatalf("tunnels pane should render active filter inside panel: %q", tunnels)
	}
}

func TestTUIConnectActionQuitsWithSelectedHost(t *testing.T) {
	model := newTUIModelWithState("", config.Config{
		Version: 1,
		Hosts: map[string]config.Host{
			"dev":  {Host: "dev.example"},
			"prod": {Host: "prod.example"},
		},
	}, nil)
	model.cursor = 1

	updated, cmd := model.updateMain(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if model.connectName != "prod" {
		t.Fatalf("connectName = %q, want prod", model.connectName)
	}
	if cmd == nil {
		t.Fatalf("expected quit command")
	}
}

func TestTUITunnelModeAddsEditsRemovesAndStartsTunnel(t *testing.T) {
	withTempHome(t)
	path, _, err := config.Init()
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	cfg := config.Default()
	cfg.Hosts["prod"] = config.Host{Host: "prod.example", User: "deploy"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("SHBX_SSH_BIN", fakeSSHBinary(t))

	model := newTUIModelWithState(path, cfg, nil)
	model.toggleMode()
	if model.mode != tuiModeTunnels {
		t.Fatalf("mode = %v, want tunnels", model.mode)
	}

	model = openTUITunnelForm(t, model, "", config.Tunnel{Type: "local"})
	setTUITunnelFormValues(&model, "db", "prod", "local", "", "5432", "127.0.0.1", "5432")
	if err := model.saveTunnelForm(); err != nil {
		t.Fatalf("save add tunnel form: %v", err)
	}
	assertTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432})

	model = openTUITunnelForm(t, model, "db", model.cfg.Tunnels["db"])
	setTUITunnelFormValues(&model, "socks", "prod", "dynamic", "127.0.0.1", "1080", "ignored", "9999")
	if err := model.saveTunnelForm(); err != nil {
		t.Fatalf("save edit tunnel form: %v", err)
	}
	assertTunnelMissing(t, "db")
	assertTunnel(t, "socks", config.Tunnel{Host: "prod", Type: "dynamic", BindAddress: "127.0.0.1", LocalPort: 1080})

	updated, cmd := model.updateMain(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if model.tunnelName != "socks" {
		t.Fatalf("tunnelName = %q, want socks", model.tunnelName)
	}
	if cmd == nil {
		t.Fatalf("expected quit command")
	}

	model.tunnelName = ""
	model.screen = tuiScreenRemove
	updated, _ = model.updateRemove(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	assertTunnelMissing(t, "socks")
}

func TestTUISelectedRowStaysInsideListWidth(t *testing.T) {
	item := tuiHostItem{
		name: "starlink-be-dev",
		host: config.Host{
			Host:         "10.10.100.124",
			User:         "ssniegowski",
			IdentityFile: "/Users/itaprac/.ssh/proxy-scp_ssniegowski",
		},
	}
	width := 36
	row := hostRow(item, width-2, selectedTargetStyle)
	rendered := selectedStyle.Render(fitRow("> "+row, width))
	if got := lipgloss.Width(rendered); got > width {
		t.Fatalf("selected row width = %d, want <= %d; row %q", got, width, rendered)
	}
}

func TestTUIUnfocusedPaneDoesNotShowCursorMarker(t *testing.T) {
	model := newTUIModelWithState("", config.Config{
		Version: 1,
		Hosts: map[string]config.Host{
			"mini": {Host: "100.124.218.15", User: "srv"},
		},
		Tunnels: map[string]config.Tunnel{
			"db": {Host: "mini", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432},
		},
	}, nil)

	hosts := model.hostsPaneView(52, false)
	if strings.Contains(hosts, "> mini") {
		t.Fatalf("unfocused hosts pane still shows cursor marker: %q", hosts)
	}

	tunnels := model.tunnelsPaneView(52, false)
	if strings.Contains(tunnels, "> db") {
		t.Fatalf("unfocused tunnels pane still shows cursor marker: %q", tunnels)
	}
}

func TestTUIListsGroupHostsAndTunnels(t *testing.T) {
	model := newTUIModelWithState("", config.Config{
		Version: 1,
		Hosts: map[string]config.Host{
			"dev":  {Host: "dev.example"},
			"prod": {Host: "prod.example", Group: "work"},
		},
		Tunnels: map[string]config.Tunnel{
			"db":    {Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432, Group: "work"},
			"socks": {Host: "dev", Type: "dynamic", LocalPort: 1080},
		},
	}, nil)

	hosts := model.hostsPaneView(60, true)
	if !strings.Contains(hosts, "work") || !strings.Contains(hosts, "ungrouped") {
		t.Fatalf("hosts pane missing group sections: %q", hosts)
	}

	tunnels := model.tunnelsPaneView(60, true)
	if !strings.Contains(tunnels, "work") || !strings.Contains(tunnels, "ungrouped") {
		t.Fatalf("tunnels pane missing group sections: %q", tunnels)
	}
}

func TestWrapTextKeepsLongCommandVisible(t *testing.T) {
	command := "ssh -p 22 -M -S /very/long/control/path/that/would/otherwise/be/truncated.sock -f -N -T deploy@example.com"
	wrapped := wrapText(command, 32)
	if strings.Contains(wrapped, "...") {
		t.Fatalf("wrapped command should not be truncated: %q", wrapped)
	}
	if !strings.Contains(wrapped, "deploy@example.com") || !strings.Contains(wrapped, "truncated.sock") {
		t.Fatalf("wrapped command lost content: %q", wrapped)
	}
	for _, line := range strings.Split(wrapped, "\n") {
		if lipgloss.Width(line) > 32 {
			t.Fatalf("line width = %d, want <= 32 for %q in %q", lipgloss.Width(line), line, wrapped)
		}
	}
}

func TestTUIPreviewDoesNotRenderGlobalHelpOrDuplicateQuit(t *testing.T) {
	model := newTUIModelWithState("", config.Default(), nil)
	model.screen = tuiScreenPreview
	model.status = "ssh -p 22 -M -S /very/long/control/path.sock -f -N -T deploy@example.com"
	model.width = 48

	view := model.View()
	if strings.Contains(view, "enter/c connect") {
		t.Fatalf("preview should not render global help: %q", view)
	}
	if got := strings.Count(view, "quit"); got != 1 {
		t.Fatalf("preview should render one quit help, got %d occurrences in %q", got, view)
	}
	if !strings.Contains(view, "\n  ") {
		t.Fatalf("wrapped preview command should indent continuation lines: %q", view)
	}
}

func TestTUICommandPaletteRunsActions(t *testing.T) {
	withTempHome(t)
	model := newTUIModelWithState("", config.Config{
		Version: 1,
		Hosts: map[string]config.Host{
			"prod": {Host: "prod.example"},
		},
	}, nil)

	updated, _ := model.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	model = updated.(tuiModel)
	if model.screen != tuiScreenPalette {
		t.Fatalf("screen = %v, want command palette", model.screen)
	}
	view := model.View()
	for _, want := range []string{"Command palette", "Connect host", "Run doctor"} {
		if !strings.Contains(view, want) {
			t.Fatalf("palette view missing %q in %q", want, view)
		}
	}

	for i, item := range model.palette {
		if item.action == tuiPaletteDoctor {
			model.paletteCursor = i
			break
		}
	}
	updated, _ = model.updatePalette(keyMsg(tea.KeyEnter))
	model = updated.(tuiModel)
	if model.screen != tuiScreenPreview {
		t.Fatalf("screen = %v, want preview", model.screen)
	}
	if model.outputTitle != "Doctor" {
		t.Fatalf("outputTitle = %q, want Doctor", model.outputTitle)
	}
	if !strings.Contains(model.status, "sshuttlebox doctor") {
		t.Fatalf("doctor output missing from status %q", model.status)
	}
	view = model.View()
	if !strings.Contains(view, "sshuttlebox doctor") || !strings.Contains(view, "\n[WARN] config file missing") {
		t.Fatalf("doctor preview should preserve report lines: %q", view)
	}
}

func TestUIHelpAndCompletionMentionCommand(t *testing.T) {
	var out strings.Builder
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out.String(), "ui               Open the interactive terminal UI") {
		t.Fatalf("help missing ui command in %q", out.String())
	}
	if !strings.Contains(out.String(), "doctor           Check config, SSH, keys, and tunnels") {
		t.Fatalf("help missing doctor command in %q", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"__complete", "commands", "--", "u"}); err != nil {
		t.Fatalf("complete commands: %v", err)
	}
	if out.String() != "ui\n" {
		t.Fatalf("completion output = %q, want ui", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"__complete", "commands", "--", "d"}); err != nil {
		t.Fatalf("complete commands: %v", err)
	}
	if out.String() != "doctor\n" {
		t.Fatalf("completion output = %q, want doctor", out.String())
	}
}

func openTUIForm(t *testing.T, model tuiModel, name string, host config.Host) tuiModel {
	t.Helper()

	updated, _ := model.openForm(name, host)
	return updated.(tuiModel)
}

func setTUIFormValues(model *tuiModel, name, host, user, password, port, identityFile string) {
	values := []string{name, host, user, password, port, identityFile}
	for i, value := range values {
		model.inputs[i].SetValue(value)
	}
}

func openTUITunnelForm(t *testing.T, model tuiModel, name string, tunnel config.Tunnel) tuiModel {
	t.Helper()

	updated, _ := model.openTunnelForm(name, tunnel)
	return updated.(tuiModel)
}

func setTUITunnelFormValues(model *tuiModel, name, host, tunnelType, bind, localPort, remoteHost, remotePort string) {
	values := []string{name, host, tunnelType, bind, localPort, remoteHost, remotePort}
	for i, value := range values {
		model.inputs[i].SetValue(value)
	}
}

func assertTUIFormError(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func keyMsg(keyType tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: keyType}
}
