package update

import "testing"

func TestOperationState_StableNames(t *testing.T) {
	cases := []struct {
		state OperationState
		want  string
	}{
		{StateIdle, "idle"},
		{StatePrepare, "prepare"},
		{StateFetchRelease, "fetch_release"},
		{StateSelectAsset, "select_asset"},
		{StateDownload, "download"},
		{StateVerify, "verify"},
		{StateBackup, "backup"},
		{StateSwap, "swap"},
		{StateDone, "done"},
		{StateFailed, "failed"},
		{StateUpToDate, "up_to_date"},
		{StateChecking, "checking"},
		{StateInProgress, "in_progress"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if string(tc.state) != tc.want {
				t.Errorf("state %q != %q", tc.state, tc.want)
			}
			if !tc.state.IsValid() {
				t.Errorf("state %q should be valid", tc.state)
			}
		})
	}
	if got := OperationState("unknown"); got.IsValid() {
		t.Error("unknown state should be invalid")
	}
}

func TestOperationState_IsTerminal(t *testing.T) {
	terminal := []OperationState{StateDone, StateFailed, StateUpToDate}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	nonTerminal := []OperationState{StateIdle, StatePrepare, StateDownload, StateChecking, StateInProgress}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

func TestUpdateResult_PhaseTyped(t *testing.T) {
	r := UpdateResult{Phase: StateDownload, State: StateDownload}
	if r.Phase != StateDownload || r.State != StateDownload {
		t.Errorf("Phase/State not typed correctly: %+v", r)
	}
}
