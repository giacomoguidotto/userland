package adapters

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedSSHConfigSelectsAgentWithoutShellSetup(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../cfg/home/ssh/config")
	if err != nil {
		t.Fatal(err)
	}
	const macSocket = "Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
	const linuxSocket = ".1password/agent.sock"
	for _, tc := range []struct {
		name    string
		sockets []string
		host    string
		agent   string
		prefix  string
	}{
		{name: "macOS desktop with stale inherited agent", sockets: []string{macSocket}, host: "github.com", agent: macSocket},
		{name: "Linux desktop with stale inherited agent", sockets: []string{linuxSocket}, host: "github.com", agent: linuxSocket},
		{name: "macOS socket takes precedence", sockets: []string{macSocket, linuxSocket}, host: "github.com", agent: macSocket},
		{name: "remote session keeps forwarded agent", host: "github.com", agent: "SSH_AUTH_SOCK"},
		{name: "other hosts keep their agent", sockets: []string{macSocket, linuxSocket}, host: "trellis-remote-dev"},
		{name: "earlier explicit setting wins", sockets: []string{macSocket}, host: "github.com", agent: "none", prefix: "Host github.com\n  IdentityAgent none\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Keep Unix socket paths below macOS's length limit.
			home, err := os.MkdirTemp("/tmp", "ul-ssh-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(home) })
			for _, name := range tc.sockets {
				path := filepath.Join(home, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				socket, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { socket.Close() })
			}
			// OpenSSH resolves ~ and %d through passwd, not the process HOME.
			// Substitute only that home expansion to isolate the shipped rules.
			config := strings.NewReplacer("%d", home, "~/", home+"/").Replace(string(source))
			path := filepath.Join(home, "config")
			if err := os.WriteFile(path, []byte(tc.prefix+config), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(ssh, "-G", "-F", path, tc.host)
			cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=/tmp/userland-inherited-agent.sock")
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("OpenSSH rejected managed config: %v", err)
			}
			got := ""
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "identityagent ") {
					got = strings.TrimPrefix(line, "identityagent ")
				}
			}
			want := tc.agent
			if want == macSocket || want == linuxSocket {
				want = filepath.Join(home, want)
			}
			if got != want {
				t.Fatalf("effective IdentityAgent = %q, want %q", got, want)
			}
		})
	}
}
