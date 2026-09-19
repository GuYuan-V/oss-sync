package serverplugin

import "testing"

func TestCoreRequestEventCoversPluginDomains(t *testing.T) {
	tests := []struct {
		method string
		path   string
		after  string
	}{
		{method: "POST", path: "/api/auth/register", after: "user.created"},
		{method: "POST", path: "/api/vaults", after: "vault.created"},
		{method: "POST", path: "/api/vaults/id/sync/upload", after: "file.saved"},
		{method: "POST", path: "/api/shares", after: "share.created"},
		{method: "PATCH", path: "/api/devices/id", after: "device.updated"},
		{method: "DELETE", path: "/api/vaults/id/collaborations/1", after: "collaboration.deleted"},
	}
	for _, test := range tests {
		if got := coreRequestEvent(test.method, test.path).After; got != test.after {
			t.Errorf("coreRequestEvent(%s, %s).After = %q, want %q", test.method, test.path, got, test.after)
		}
	}
}
