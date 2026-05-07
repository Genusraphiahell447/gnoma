package bash

import "testing"

func TestIsInteractiveCmd(t *testing.T) {
	tests := []struct {
		cmd     string
		wantHit bool
	}{
		// prefix-interactive: always interactive with any args
		{"sudo apt install foo", true},
		{"sudo", true},
		{"sudo\tls", true},
		{"ssh user@host", true},
		{"ssh -p 2222 host", true},
		{"ssh", true},
		{"passwd", true},
		{"passwd root", true},
		{"vim file.txt", true},
		{"vim", true},
		{"vi /etc/hosts", true},
		{"vi", true},
		{"nano config.yml", true},
		{"nano", true},
		{"less output.log", true},
		{"more pager.txt", true},
		{"htop", true},
		{"top", true},
		{"top -b", true},
		{"mysql -u root", true},
		{"psql -U postgres", true},
		{"ftp example.com", true},
		{"sftp user@host", true},
		{"git push origin main", true},
		{"git push", true},

		// exact-interactive: interactive only as bare command (REPL)
		{"python3", true},
		{"python", true},
		{"python2", true},
		{"node", true},
		{"irb", true},
		{"iex", true},
		{"ghci", true},
		{"julia", true},

		// exact-interactive: args → NOT interactive (running a script)
		{"python3 script.py", false},
		{"python3 -c 'print(1)'", false},
		{"python2 run.py", false},
		{"node server.js", false},
		{"irb file.rb", false},
		{"julia script.jl", false},

		// non-interactive commands
		{"ls -la", false},
		{"grep foo bar.txt", false},
		{"cat /etc/hosts", false},
		{"git status", false},
		{"git pull", false},
		{"git diff", false},
		{"echo hello", false},
		{"pwd", false},
		{"find . -name '*.go'", false},

		// should not match on prefix-sharing names
		{"topological_sort", false},
		{"sshkeys", false},
		{"vitals", false},
		{"moral", false},
		{"git pushover", false},
	}

	for _, tt := range tests {
		reason := isInteractiveCmd(tt.cmd)
		got := reason != ""
		if got != tt.wantHit {
			t.Errorf("isInteractiveCmd(%q) hit=%v (reason=%q), want hit=%v",
				tt.cmd, got, reason, tt.wantHit)
		}
	}
}
