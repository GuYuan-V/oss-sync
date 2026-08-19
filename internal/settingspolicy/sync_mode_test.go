package settingspolicy

import (
	"errors"
	"testing"
)

func TestParseSyncMode_whenValueVaries_acceptsOnlySupportedModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    SyncMode
		wantErr bool
	}{
		{name: "accepts user choice", value: "user_choice", want: SyncModeUserChoice},
		{name: "accepts short polling", value: "short_poll", want: SyncModeShortPoll},
		{name: "accepts long polling", value: "long_poll", want: SyncModeLongPoll},
		{name: "rejects unknown mode", value: "websocket", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			got, err := ParseSyncMode(tt.value)

			// Then
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidSyncMode) {
					t.Fatalf("ParseSyncMode(%q) error = %v, want ErrInvalidSyncMode", tt.value, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSyncMode(%q): %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("ParseSyncMode(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
