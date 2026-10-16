package tmux

import "testing"

func TestInService(t *testing.T) {
	for _, c := range []struct {
		cgroup string
		want   bool
	}{
		{"0::/user.slice/user-1000.slice/user@1000.service/app.slice/nit.service\n", true},
		{"0::/system.slice/nit.service\n", true},
		{"0::/user.slice/user-1000.slice/session-3.scope\n", false},
		{"0::/user.slice/user-1000.slice/user@1000.service/app.slice/tmux-spawn-fdd6cfa1.scope\n", false},
		{"0::/\n", false},
		// cgroup v1 вместе с v2: юнит в строке name=systemd
		{"12:cpu,cpuacct:/\n1:name=systemd:/user.slice/user-1000.slice/user@1000.service/app.slice/nit.service\n0::/\n", true},
		{"", false},
	} {
		if got := inService(c.cgroup); got != c.want {
			t.Errorf("inService(%q) = %v, want %v", c.cgroup, got, c.want)
		}
	}
}
