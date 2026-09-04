package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/itaprac/sshuttlebox/internal/config"
	"github.com/itaprac/sshuttlebox/internal/tunnelstate"
)

type tuiTunnelStatus struct {
	state string
	err   error
}
type tuiStatusTickMsg struct{}
type tuiStatusMsg struct {
	generation uint64
	statuses   map[string]tuiTunnelStatus
}
type tuiReloadMsg struct {
	cfg config.Config
	err error
}
type tuiOperationMsg struct {
	cursor          int
	tunnelCursor    int
	interactiveName string
	cfg             *config.Config
	status          string
	err             error
	formSaved       bool
}

func tuiStatusTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tuiStatusTickMsg{} })
}

func (m tuiModel) refreshStatusCmd() tea.Cmd {
	names := append([]string(nil), m.tunnelNames...)
	generation := m.generation
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := tunnelstate.Snapshot(ctx, names)
		statuses := make(map[string]tuiTunnelStatus, len(names))
		for _, name := range names {
			status := snapshot[name]
			problem := status.Err
			if err != nil {
				problem = err
			}
			state := "stopped"
			if status.Running {
				state = "running"
			}
			if problem != nil {
				state = "unknown"
			}
			statuses[name] = tuiTunnelStatus{state: state, err: problem}
		}
		return tuiStatusMsg{generation: generation, statuses: statuses}
	}
}

func (m tuiModel) tunnelStatus(name string) string {
	if status, ok := m.statuses[name]; ok {
		return status.state
	}
	return "checking"
}

func (m tuiModel) reloadConfigCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.LoadPath(m.path)
		return tuiReloadMsg{cfg: cfg, err: err}
	}
}

func (m tuiModel) resumeAfterSession(action string, sessionErr error) tuiModel {
	m.connectName, m.sftpName, m.tunnelName = "", "", ""
	m.screen = tuiScreenMain
	m.refreshing = false
	m.generation++
	cfg, err := config.LoadPath(m.path)
	if err != nil {
		m.loadErr = err
		m.setError(err)
		return m
	}
	m.cfg = cfg
	m.loadErr = nil
	m.reloadNames()
	m.reloadTunnelNames()
	m.status = action + " ended."
	m.err = sessionErr
	return m
}

func (m tuiModel) beginSave(tunnel bool) (tea.Model, tea.Cmd) {
	if m.loadErr != nil {
		m.setError(m.loadErr)
		return m, nil
	}
	if m.busy {
		return m, nil
	}
	worker := m
	worker.cfg = config.Clone(m.cfg)
	worker.inputs = append([]textinput.Model(nil), m.inputs...)
	// Input models contain mutable state. Freeze the form while its save runs.
	m.busy = true
	m.generation++
	m.status = "Saving..."
	m.err = nil
	return m, func() tea.Msg {
		var err error
		if tunnel {
			err = worker.saveTunnelForm()
		} else {
			err = worker.saveForm()
		}
		result := tuiOperationMsg{status: worker.status, err: err, formSaved: err == nil, cursor: worker.cursor, tunnelCursor: worker.tunnelCursor}
		if err == nil {
			result.cfg = &worker.cfg
		}
		return result
	}
}

func (m tuiModel) beginTunnelToggle(item tuiTunnelItem) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	host, ok := m.cfg.Hosts[item.tunnel.Host]
	if !ok {
		m.setError(fmt.Errorf("host %q not found", item.tunnel.Host))
		return m, nil
	}
	m.screen = tuiScreenMain
	m.busy = true
	m.generation++
	m.statuses = copyTUIStatuses(m.statuses)
	state := "starting"
	if m.tunnelStatus(item.name) == "running" {
		state = "stopping"
	}
	m.statuses[item.name] = tuiTunnelStatus{state: state}
	m.status = state + " tunnel " + item.name + "..."
	m.err = nil
	return m, func() tea.Msg {
		_, running, err := tunnelstate.Get(item.name)
		if err != nil {
			return tuiOperationMsg{err: err}
		}
		if running {
			_, _, err := stopTunnelByName(item.name, host)
			return tuiOperationMsg{status: fmt.Sprintf("Stopped tunnel %q", item.name), err: err}
		}
		if host.Password == "" {
			return tuiOperationMsg{interactiveName: item.name}
		}
		pid, err := startTunnelProcess(item.name, host, item.tunnel, false)
		return tuiOperationMsg{status: fmt.Sprintf("Started tunnel %q in background with pid %d", item.name, pid), err: err}
	}
}

func (m tuiModel) beginTunnelRemove(item tuiTunnelItem) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	next := config.Clone(m.cfg)
	m.busy = true
	m.generation++
	m.screen = tuiScreenMain
	m.status = "Removing tunnel " + item.name + "..."
	m.err = nil
	return m, func() tea.Msg {
		var saved config.Config
		err := tunnelstate.WithStopped(item.name, true, func() error {
			delete(next.Tunnels, item.name)
			var err error
			saved, err = config.SaveMerged(m.path, next)
			return err
		})
		if err != nil {
			return tuiOperationMsg{err: err}
		}
		return tuiOperationMsg{cfg: &saved, status: fmt.Sprintf("Removed tunnel %q", item.name)}
	}
}

func copyTUIStatuses(statuses map[string]tuiTunnelStatus) map[string]tuiTunnelStatus {
	result := make(map[string]tuiTunnelStatus, len(statuses))
	for name, status := range statuses {
		result[name] = status
	}
	return result
}
