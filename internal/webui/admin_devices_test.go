package webui

import (
	"reflect"
	"testing"
)

func TestDeviceAuthSummary(t *testing.T) {
	tests := []struct {
		name     string
		vaults   []vaultOption
		clientID string
		wantCnt  int
		wantNms  []string
	}{
		{
			name:     "nil vaults",
			vaults:   nil,
			clientID: "c1",
			wantCnt:  0,
			wantNms:  nil,
		},
		{
			name:     "empty vaults",
			vaults:   []vaultOption{},
			clientID: "c1",
			wantCnt:  0,
			wantNms:  nil,
		},
		{
			name: "single vault authorized for this client",
			vaults: []vaultOption{
				{Name: "notes", AuthorizedForClient: map[string]bool{"c1": true}},
			},
			clientID: "c1",
			wantCnt:  1,
			wantNms:  []string{"notes"},
		},
		{
			name: "single vault not authorized for this client",
			vaults: []vaultOption{
				{Name: "notes", AuthorizedForClient: map[string]bool{"c2": true}},
			},
			clientID: "c1",
			wantCnt:  0,
			wantNms:  nil,
		},
		{
			name: "single vault with empty AuthorizedForClient",
			vaults: []vaultOption{
				{Name: "notes", AuthorizedForClient: map[string]bool{}},
			},
			clientID: "c1",
			wantCnt:  0,
			wantNms:  nil,
		},
		{
			name: "multiple vaults mixed authorization",
			vaults: []vaultOption{
				{Name: "alpha", AuthorizedForClient: map[string]bool{"c1": true}},
				{Name: "beta", AuthorizedForClient: map[string]bool{"c2": true}},
				{Name: "gamma", AuthorizedForClient: map[string]bool{"c1": true, "c2": true}},
			},
			clientID: "c1",
			wantCnt:  2,
			wantNms:  []string{"alpha", "gamma"},
		},
		{
			name: "preserves vault order in names",
			vaults: []vaultOption{
				{Name: "third", AuthorizedForClient: map[string]bool{"c1": true}},
				{Name: "first", AuthorizedForClient: map[string]bool{"c1": true}},
				{Name: "second", AuthorizedForClient: map[string]bool{"c1": true}},
			},
			clientID: "c1",
			wantCnt:  3,
			wantNms:  []string{"third", "first", "second"},
		},
		{
			name: "all vaults authorized for this client",
			vaults: []vaultOption{
				{Name: "x", AuthorizedForClient: map[string]bool{"c1": true}},
				{Name: "y", AuthorizedForClient: map[string]bool{"c1": true}},
			},
			clientID: "c1",
			wantCnt:  2,
			wantNms:  []string{"x", "y"},
		},
		{
			name: "none authorized for this client",
			vaults: []vaultOption{
				{Name: "x", AuthorizedForClient: map[string]bool{"c2": true, "c3": true}},
				{Name: "y", AuthorizedForClient: map[string]bool{}},
			},
			clientID: "c1",
			wantCnt:  0,
			wantNms:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCnt, gotNms := deviceAuthSummary(tt.vaults, tt.clientID)
			if gotCnt != tt.wantCnt {
				t.Errorf("count = %d, want %d", gotCnt, tt.wantCnt)
			}
			if !reflect.DeepEqual(gotNms, tt.wantNms) {
				t.Errorf("names = %#v, want %#v", gotNms, tt.wantNms)
			}
		})
	}
}
