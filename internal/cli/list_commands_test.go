package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/itaprac/sshuttlebox/internal/config"
)

func TestListFiltersHostsByGroup(t *testing.T) {
	withTempHome(t)
	addHost(t, "dev", config.Host{Host: "dev.example", User: "me", Group: "personal"})
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy", Group: "work"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"list", "--group", "work"}); err != nil {
		t.Fatalf("list --group: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "prod") || !strings.Contains(got, "work") {
		t.Fatalf("grouped host missing from output %q", got)
	}
	if strings.Contains(got, "dev") || strings.Contains(got, "personal") {
		t.Fatalf("ungrouped host leaked into filtered output %q", got)
	}
}

func TestListJSONIncludesStableHostFields(t *testing.T) {
	withTempHome(t)
	addHost(t, "prod", config.Host{
		Host:         "prod.example",
		User:         "deploy",
		Password:     "secret",
		Port:         2222,
		IdentityFile: "/tmp/prod_key",
		Group:        "work",
	})
	addHost(t, "bastion", config.Host{Host: "bastion.example", User: "jump", Group: "work"})
	addHost(t, "dev", config.Host{Host: "dev.example", Group: "personal"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"list", "--json", "--group", "work"}); err != nil {
		t.Fatalf("list --json --group: %v", err)
	}

	want := `[{"name":"bastion","host":"bastion.example","user":"jump","passwordSet":false,"port":0,"identityFile":"","group":"work"},{"name":"prod","host":"prod.example","user":"deploy","passwordSet":true,"port":2222,"identityFile":"/tmp/prod_key","group":"work"}]` + "\n"
	if out.String() != want {
		t.Fatalf("json output = %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatalf("json output leaked saved password: %q", out.String())
	}
}

func TestTunnelListFiltersByGroup(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("XDG_STATE_HOME", home)
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy"})
	addTunnel(t, "cache", config.Tunnel{Host: "prod", Type: "dynamic", LocalPort: 1080, Group: "personal"})
	addTunnel(t, "db", config.Tunnel{Host: "prod", Type: "local", LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432, Group: "work"})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"tunnel", "list", "--group", "work"}); err != nil {
		t.Fatalf("tunnel list --group: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "db") || !strings.Contains(got, "work") || !strings.Contains(got, "stopped") {
		t.Fatalf("grouped tunnel missing from output %q", got)
	}
	if strings.Contains(got, "cache") || strings.Contains(got, "personal") {
		t.Fatalf("ungrouped tunnel leaked into filtered output %q", got)
	}
}

func TestTunnelListJSONIncludesStableTunnelFields(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("XDG_STATE_HOME", home)
	addHost(t, "prod", config.Host{Host: "prod.example", User: "deploy"})
	addTunnel(t, "cache", config.Tunnel{Host: "prod", Type: "dynamic", LocalPort: 1080, Group: "personal"})
	addTunnel(t, "db", config.Tunnel{
		Host:        "prod",
		Type:        "local",
		BindAddress: "127.0.0.1",
		LocalPort:   5432,
		RemoteHost:  "127.0.0.1",
		RemotePort:  5432,
		Group:       "work",
	})

	var out bytes.Buffer
	app := App{in: strings.NewReader(""), out: &out}
	if err := app.Run([]string{"tunnel", "list", "--json", "--group", "work"}); err != nil {
		t.Fatalf("tunnel list --json --group: %v", err)
	}

	want := `[{"name":"db","host":"prod","type":"local","bindAddress":"127.0.0.1","localPort":5432,"remoteHost":"127.0.0.1","remotePort":5432,"group":"work","status":"stopped","running":false}]` + "\n"
	if out.String() != want {
		t.Fatalf("json output = %q, want %q", out.String(), want)
	}
}
