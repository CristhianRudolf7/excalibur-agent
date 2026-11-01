package engine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestPackageCommandPositions(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want bool
	}{
		{"echo available # apt-get install", false}, {`printf 'make -j; apt-get install'`, false},
		{"git filter-repo --help", false}, {"apt-get install jq", true}, {"make -j", true},
		{`sudo -n env MSG="quoted build text" make -j`, true},
		{"A=1 sudo -u root env B=2 nohup nice -n 10 make -j", true}, {"echo ok | cat; make -j", true},
	} {
		if got := packageOrBuildCommand(tc.cmd); got != tc.want {
			t.Errorf("%q: %v", tc.cmd, got)
		}
		args, _ := json.Marshal(map[string]string{"command": tc.cmd})
		h := []tasks.HistoryTool{{Name: "exec", Args: string(args), Output: "exit: running  session: sess-1  alive: true"}}
		if got := runningWorkReasons(h, nil, time.Now()); (len(got) > 0) != tc.want {
			t.Errorf("finish check %q: %v", tc.cmd, got)
		}
	}
}

func TestRunningWorkUsesLiveFacts(t *testing.T) {
	h := []tasks.HistoryTool{{Name: "exec", Args: `{"command":"make -j"}`, Output: "exit: running  session: sess-1  alive: true"}}
	for _, info := range []tools.SessionInfo{{ID: "sess-1", Alive: false}, {ID: "sess-1", Alive: true}, {ID: "sess-1", Alive: true, Build: true}} {
		got := runningWorkReasons(h, nil, time.Now(), []tools.SessionInfo{info})
		if (len(got) > 0) != (info.Alive && info.Build) {
			t.Fatalf("%+v: %v", info, got)
		}
	}
}
