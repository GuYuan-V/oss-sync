package version

import "testing"

func TestIsDevelopmentVersion(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"dev", true},
		{"DEV", true},
		{"Dev", true},
		{"", true},
		{" ", true},
		{"0.1.0", false},
		{"v1.2.3", false},
		{"1.0.0-alpha", false},
		{"1.2.3+build", false},
		{" 1.2.3 ", false},
		{"not-semver", true},
		{"1.2", true},
		{"01.2.3", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := IsDevelopmentVersion(tc.input); got != tc.want {
				t.Errorf("IsDevelopmentVersion(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsDevelopment_Current(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "dev"
	if !IsDevelopment() {
		t.Error("dev should be development")
	}
	Version = "1.2.3"
	if IsDevelopment() {
		t.Error("1.2.3 should not be development")
	}
	Version = "0.1.0"
	if IsDevelopment() {
		t.Error("0.1.0 should not be development")
	}
}

func TestVersionDefaults(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty, default is dev")
	}
	// Must not be falsely eligible
	if !IsDevelopmentVersion("dev") {
		t.Error("dev must be development")
	}
	info := Get()
	if info.Version != Version {
		t.Errorf("Get().Version = %q, want %q", info.Version, Version)
	}
}
