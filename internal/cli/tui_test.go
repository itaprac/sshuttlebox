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

func TestUIHelpAndCompletionMentionCommand(t *testing.T) {
	var out strings.Builder
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out.String(), "ui               Open the interactive terminal UI") {
		t.Fatalf("help missing ui command in %q", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"__complete", "commands", "--", "u"}); err != nil {
		t.Fatalf("complete commands: %v", err)
	}
	if out.String() != "ui\n" {
		t.Fatalf("completion output = %q, want ui", out.String())
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
