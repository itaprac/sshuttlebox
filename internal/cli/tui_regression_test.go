package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
)

func TestTUIFormsAcceptQInTextFields(t *testing.T) {
	for _, tunnel := range []bool{false, true} {
		m := newTUIModelWithState("", config.Default(), nil)
		if tunnel {
			m = openTUITunnelForm(t, m, "", config.Tunnel{Type: "local"})
		} else {
			m = openTUIForm(t, m, "", config.Host{})
		}
		for field := range m.inputs {
			if tunnel && field == tuiTunnelFieldType {
				continue
			}
			m.inputs[m.focus].Blur()
			m.focus = field
			m.inputs[field].Focus()
			m.inputs[field].SetValue("")
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
			m = updated.(tuiModel)
			if got := m.inputs[field].Value(); got != "q" {
				t.Fatalf("tunnel=%t field=%d value=%q", tunnel, field, got)
			}
		}
	}
}

func TestTUILoadErrorBlocksWritesUntilSuccessfulReload(t *testing.T) {
	withTempHome(t)
	path, _, err := config.Init()
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("{broken JSON")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	m := newTUIModel()
	if m.loadErr == nil {
		t.Fatal("missing persistent load error")
	}
	for _, key := range []string{"a", "e", "r", ":"} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(tuiModel)
		if m.screen != tuiScreenMain || m.loadErr == nil {
			t.Fatalf("key %q bypassed load error", key)
		}
	}
	updated, _ := m.openForm("", config.Host{})
	m = updated.(tuiModel)
	if m.screen == tuiScreenForm {
		t.Fatal("direct form opened after load error")
	}
	if m.saveForm() == nil || m.saveTunnelForm() == nil {
		t.Fatal("save bypassed load guard")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("damaged config was replaced")
	}
	if !strings.Contains(m.View(), path) || !strings.Contains(m.View(), "reload") {
		t.Fatal("recovery instructions missing")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"hosts":{},"tunnels":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	updated, _ = m.Update(m.reloadConfigCmd()())
	m = updated.(tuiModel)
	if m.loadErr != nil {
		t.Fatal(m.loadErr)
	}
	updated, _ = m.openForm("", config.Host{})
	if updated.(tuiModel).screen != tuiScreenForm {
		t.Fatal("reload did not unblock forms")
	}
}

func TestTUIFormSavePreservesConcurrentAdditionAndRejectsConflictingEdit(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "old.example"})
	m := newTUIModel()
	m = openTUIForm(t, m, "prod", m.cfg.Hosts["prod"])
	addHost(t, "other", config.Host{Host: "other.example"})
	m.inputs[tuiFieldHost].SetValue("new.example")
	if err := m.saveForm(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.cfg.Hosts["other"]; !ok {
		t.Fatal("merged config missing concurrent addition")
	}
	m = openTUIForm(t, m, "prod", m.cfg.Hosts["prod"])
	current, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	changed := current.Hosts["prod"]
	changed.Host = "external.example"
	current.Hosts["prod"] = changed
	if err := config.Save(m.path, current); err != nil {
		t.Fatal(err)
	}
	m.inputs[tuiFieldHost].SetValue("local.example")
	before := config.Clone(m.cfg)
	if err := m.saveForm(); err == nil {
		t.Fatal("conflicting update succeeded")
	}
	if !reflect.DeepEqual(before, m.cfg) {
		t.Fatal("failed save changed model config")
	}
	assertHost(t, "prod", config.Host{Host: "external.example"})
}

func TestTUIFailedSaveKeepsModelAndForm(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example"})
	m := newTUIModel()
	m = openTUIForm(t, m, "prod", m.cfg.Hosts["prod"])
	before := config.Clone(m.cfg)
	m.inputs[tuiFieldName].SetValue("renamed")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	m.path = filepath.Join(blocker, "config.json")
	if err := m.saveForm(); err == nil {
		t.Fatal("expected save error")
	}
	if !reflect.DeepEqual(before, m.cfg) || m.screen != tuiScreenForm {
		t.Fatal("save failure changed model or closed form")
	}
}

func TestTUIActiveTunnelCannotBeRenamedOrReconfigured(t *testing.T) {
	for _, rename := range []bool{true, false} {
		t.Run(fmt.Sprint(rename), func(t *testing.T) {
			withTempHome(t)
			addHost(t, "prod", config.Host{Host: "prod.example"})
			m := newTUIModel()
			m = openTUITunnelForm(t, m, "", config.Tunnel{Type: "local"})
			setTUITunnelFormValues(&m, "db", "prod", "local", "", "5432", "localhost", "5432")
			if err := m.saveTunnelForm(); err != nil {
				t.Fatal(err)
			}
			if err := tunnelstate.Save(tunnelstate.State{Tunnels: map[string]tunnelstate.Entry{"db": tunnelstate.NewEntry(os.Getpid(), "ssh -N prod", "")}}); err != nil {
				t.Fatal(err)
			}
			m = openTUITunnelForm(t, m, "db", m.cfg.Tunnels["db"])
			if rename {
				m.inputs[tuiTunnelFieldName].SetValue("new")
			} else {
				m.inputs[tuiTunnelFieldLocalPort].SetValue("5433")
			}
			before := config.Clone(m.cfg)
			if err := m.saveTunnelForm(); err == nil {
				t.Fatal("active tunnel edit succeeded")
			}
			if !reflect.DeepEqual(before, m.cfg) {
				t.Fatal("active tunnel edit changed model")
			}
			loaded, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Tunnels["db"] != before.Tunnels["db"] {
				t.Fatal("active tunnel edit changed disk")
			}
		})
	}
}

func TestTUIViewUsesSnapshotWithoutReadingStateOrStartingSSH(t *testing.T) {
	home := withTempHome(t)
	marker := filepath.Join(home, "ssh-called")
	fake := filepath.Join(home, "ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf called >> '"+marker+"'\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHBX_SSH_BIN", fake)
	cfg := config.Default()
	cfg.Hosts["prod"] = config.Host{Host: "prod.example"}
	for i := 0; i < 100; i++ {
		cfg.Tunnels[fmt.Sprintf("db%03d", i)] = config.Tunnel{Host: "prod", Type: "dynamic", LocalPort: 1080 + i}
	}
	m := newTUIModelWithState("", cfg, nil)
	m.mode = tuiModeTunnels
	m.width = 120
	m.height = 20
	m.statuses = map[string]tuiTunnelStatus{"db000": {state: "running"}}
	before := []string{}
	filepath.Walk(home, func(path string, info os.FileInfo, err error) error { before = append(before, path); return err })
	for i := 0; i < 3; i++ {
		if got := m.View(); !strings.Contains(got, "running") {
			t.Fatal("view ignored cached status")
		}
	}
	after := []string{}
	filepath.Walk(home, func(path string, info os.FileInfo, err error) error { after = append(after, path); return err })
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("View created files or invoked SSH: before=%v after=%v", before, after)
	}
	// A corrupt status file must not change a previously rendered snapshot.
	statePath, err := tunnelstate.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.View(), "running") {
		t.Fatal("view read corrupt disk state")
	}
	updated, _ := m.Update(m.refreshStatusCmd()())
	m = updated.(tuiModel)
	if m.tunnelStatus("db000") != "unknown" || m.statuses["db000"].err == nil {
		t.Fatal("refresh error shown as stopped")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("View invoked SSH")
	}
}

func TestTUIShortFormsKeepFocusAndValidationVisible(t *testing.T) {
	for _, tunnel := range []bool{false, true} {
		m := newTUIModelWithState("", config.Default(), nil)
		m.width = 80
		m.height = 15
		if tunnel {
			m = openTUITunnelForm(t, m, "", config.Tunnel{Type: "local"})
		} else {
			m = openTUIForm(t, m, "", config.Host{})
		}
		m.focus = len(m.inputs) - 1
		m.inputs[m.focus].SetValue("visible-group")
		m.setError(errors.New("validation failed"))
		view := m.View()
		for _, want := range []string{"> Group", "visible-group", "validation failed"} {
			if !strings.Contains(view, want) {
				t.Fatalf("tunnel=%t missing %q: %s", tunnel, want, view)
			}
		}
		if lipgloss.Height(view) > 15 {
			t.Fatalf("form height=%d", lipgloss.Height(view))
		}
	}
}

func TestTUITunnelTypePickerHidesUnusedFieldsAndLabelsRemoteTargets(t *testing.T) {
	m := newTUIModelWithState("", config.Default(), nil)
	m.width = 100
	m.height = 30
	m = openTUITunnelForm(t, m, "", config.Tunnel{Type: "local"})
	m.focus = tuiTunnelFieldType
	updated, _ := m.Update(keyMsg(tea.KeyRight))
	m = updated.(tuiModel)
	if !strings.Contains(m.View(), "Local target host") || !strings.Contains(m.View(), "Remote listen port") {
		t.Fatal("remote labels do not explain direction")
	}
	updated, _ = m.Update(keyMsg(tea.KeyRight))
	m = updated.(tuiModel)
	if strings.Contains(m.View(), "target host") || strings.Contains(m.View(), "Remote port") {
		t.Fatal("dynamic form shows unused fields")
	}
	m.focus = tuiTunnelFieldLocalPort
	m.focusNext()
	if m.focus != tuiTunnelFieldGroup {
		t.Fatal("focus enters hidden dynamic field")
	}
	m.focus = tuiTunnelFieldType
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bad")})
	m = updated.(tuiModel)
	if m.inputs[tuiTunnelFieldType].Value() != "dynamic" {
		t.Fatal("type picker accepts arbitrary text")
	}
}

func TestTUITunnelOperationsReturnCommandsBeforeIO(t *testing.T) {
	home := withTempHome(t)
	addHost(t, "prod", config.Host{Host: "prod.example", Password: "fake"})
	m := newTUIModel()
	m = openTUITunnelForm(t, m, "", config.Tunnel{Type: "dynamic"})
	setTUITunnelFormValues(&m, "socks", "prod", "dynamic", "", "1080", "", "")
	if err := m.saveTunnelForm(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "called")
	fake := filepath.Join(home, "slow-ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf called >> '"+marker+"'\nsleep 1\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHBX_SSH_BIN", fake)
	m.mode = tuiModeTunnels
	for _, remove := range []bool{false, true} {
		current := m
		start := time.Now()
		var updated tea.Model
		var cmd tea.Cmd
		if remove {
			current.screen = tuiScreenRemove
			updated, cmd = current.Update(keyMsg(tea.KeyEnter))
		} else {
			updated, cmd = current.Update(keyMsg(tea.KeyEnter))
		}
		if time.Since(start) > 200*time.Millisecond {
			t.Fatal("Update blocked on tunnel IO")
		}
		if cmd == nil || !updated.(tuiModel).busy {
			t.Fatal("operation did not enter pending state")
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("operation performed IO before command execution")
	}
}

func TestTUISessionReturnKeepsCursorFilterAndDetails(t *testing.T) {
	withTempHome(t)
	addHost(t, "alpha", config.Host{Host: "alpha.example"})
	addHost(t, "beta", config.Host{Host: "beta.example"})
	m := newTUIModel()
	m.cursor = 1
	m.filter.SetValue("a")
	m.hideDetails = true
	m.connectName = "beta"
	m = m.resumeAfterSession("SSH session", errors.New("connection failed"))
	if m.cursor != 1 || m.filter.Value() != "a" || !m.hideDetails || m.connectName != "" || m.screen != tuiScreenMain {
		t.Fatal("session return discarded UI state")
	}
	if m.err == nil {
		t.Fatal("session failure was hidden")
	}
}

func BenchmarkTUIViewSnapshot(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			cfg := config.Default()
			cfg.Hosts["prod"] = config.Host{Host: "prod.example"}
			for i := 0; i < count; i++ {
				cfg.Tunnels[fmt.Sprintf("db%04d", i)] = config.Tunnel{Host: "prod", Type: "dynamic", LocalPort: 1080 + i}
			}
			m := newTUIModelWithState("", cfg, nil)
			m.mode = tuiModeTunnels
			m.width = 120
			m.height = 24
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.View()
			}
		})
	}
}

func TestTUIImportedHostKeepsConfigSourceAndInheritedPort(t *testing.T) {
	withTempHome(t)
	source := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(source, []byte("Host alias\n HostName example.com\n Port 2222\n"), 0600); err != nil {
		t.Fatal(err)
	}
	addHost(t, "imported", config.Host{Host: "alias", SSHConfigFile: source})
	m := newTUIModel()
	m.width = 100
	if view := m.detailsView(100); !strings.Contains(view, "inherited") || !strings.Contains(view, source) {
		t.Fatal("details hide inherited SSH settings")
	}
	m = openTUIForm(t, m, "imported", m.cfg.Hosts["imported"])
	m.inputs[tuiFieldGroup].SetValue("work")
	if err := m.saveForm(); err != nil {
		t.Fatal(err)
	}
	if m.cfg.Hosts["imported"].SSHConfigFile != source || m.cfg.Hosts["imported"].Port != 0 {
		t.Fatal("edit dropped source or replaced inherited port")
	}
}

func TestTUIAsyncSaveResultSelectsSavedEntry(t *testing.T) {
	withTempHome(t)
	addHost(t, "alpha", config.Host{Host: "alpha.example"})
	m := newTUIModel()
	m = openTUIForm(t, m, "", config.Host{})
	m.inputs[tuiFieldName].SetValue("zeta")
	m.inputs[tuiFieldHost].SetValue("zeta.example")
	updated, cmd := m.Update(keyMsg(tea.KeyCtrlS))
	m = updated.(tuiModel)
	if !m.busy {
		t.Fatal("save must run outside Update")
	}
	updated, _ = m.Update(cmd())
	m = updated.(tuiModel)
	selected, ok := m.selectedItem()
	if !ok || selected.name != "zeta" || m.screen != tuiScreenMain || m.busy {
		t.Fatalf("save result did not select entry: %+v", selected)
	}
}

func BenchmarkTUIStoppedStatusRefresh(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			cfg := config.Default()
			cfg.Hosts["prod"] = config.Host{Host: "prod.example"}
			for i := 0; i < count; i++ {
				cfg.Tunnels[fmt.Sprintf("db%04d", i)] = config.Tunnel{Host: "prod", Type: "dynamic", LocalPort: 1080 + i}
			}
			m := newTUIModelWithState("", cfg, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.refreshStatusCmd()()
			}
		})
	}
}

func TestTUIIgnoresStaleStatusResults(t *testing.T) {
	m := newTUIModelWithState("", config.Default(), nil)
	m.generation = 5
	m.statuses = map[string]tuiTunnelStatus{"db": {state: "running"}}
	updated, _ := m.Update(tuiStatusMsg{generation: 4, statuses: map[string]tuiTunnelStatus{"db": {state: "stopped"}}})
	m = updated.(tuiModel)
	if m.tunnelStatus("db") != "running" {
		t.Fatal("stale refresh replaced current status")
	}
	m.busy = true
	updated, _ = m.Update(tuiStatusMsg{generation: 5, statuses: map[string]tuiTunnelStatus{"db": {state: "stopped"}}})
	if updated.(tuiModel).tunnelStatus("db") != "running" {
		t.Fatal("refresh replaced pending operation status")
	}
}

func TestTUIQuitWaitsForPendingMutationAndSkipsInteractiveFallback(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		m := newTUIModelWithState("", config.Default(), nil)
		m.busy = true
		updated, cmd := m.Update(keyMsg(tea.KeyCtrlC))
		m = updated.(tuiModel)
		if cmd != nil || !m.quitPending {
			t.Fatal("quit left a pending mutation running")
		}
		result := tuiOperationMsg{status: "finished"}
		if interactive {
			result.interactiveName = "db"
		}
		updated, cmd = m.Update(result)
		m = updated.(tuiModel)
		if cmd == nil {
			t.Fatal("completion did not finish requested quit")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("completion did not quit")
		}
		if m.tunnelName != "" {
			t.Fatal("quit request launched an interactive fallback")
		}
	}
}
