package term

import "testing"

// A wrong answer here changes who owns a phone's taps. Say yes to a desktop and platform
// defaults intended for Termux leak into an SSH client. Say no to a phone and mouse tracking
// can swallow the tap Termux needs to show Android's keyboard again.
//
// Every case sets all four variables, including the ones it wants empty: the function reads
// the process environment, and a test that inherits half of it passes or fails by whose
// machine it ran on.
func TestIsTermux(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"the app's own signal", map[string]string{"TERMUX_VERSION": "0.118.0"}, true},
		{"the bootstrap's path, for a launcher that exports no version", map[string]string{"PREFIX": "/data/data/com.termux/files/usr"}, true},
		{"both, which is the ordinary session", map[string]string{"TERMUX_VERSION": "0.118.0", "PREFIX": "/data/data/com.termux/files/usr"}, true},
		{"a desktop, where nothing says anything", nil, false},
		// A PREFIX is an ordinary variable that ordinary software sets. Only the one naming
		// Termux's own package answers the question, so the test is what it contains and not
		// whether it is there at all.
		{"somebody else's prefix", map[string]string{"PREFIX": "/usr/local"}, false},
		// Termux's sshd passes the version it was started with to every login it accepts, so
		// these two rows are a laptop keeping the drag it has always had.
		{"ssh into the phone", map[string]string{"TERMUX_VERSION": "0.118.0", "SSH_CONNECTION": "10.0.0.2 51234 10.0.0.7 8022"}, false},
		{"ssh into the phone, the other spelling", map[string]string{"TERMUX_VERSION": "0.118.0", "SSH_TTY": "/dev/pts/1"}, false},
		{"ssh into the phone, and only the bootstrap to go on", map[string]string{"PREFIX": "/data/data/com.termux/files/usr", "SSH_CONNECTION": "10.0.0.2 51234 10.0.0.7 8022"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"TERMUX_VERSION", "PREFIX", "SSH_CONNECTION", "SSH_TTY"} {
				t.Setenv(k, c.env[k])
			}
			if got := IsTermux(); got != c.want {
				t.Errorf("IsTermux() = %v, want %v, with %v", got, c.want, c.env)
			}
		})
	}
}
