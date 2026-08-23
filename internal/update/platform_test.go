package update

import "testing"

func TestSupportedPlatforms(t *testing.T) {
	plats := SupportedPlatforms()
	if len(plats) != 5 {
		t.Fatalf("SupportedPlatforms length = %d, want 5", len(plats))
	}
	want := map[string]bool{
		"linux/amd64":   true,
		"linux/arm64":   true,
		"windows/amd64": true,
		"darwin/amd64":  true,
		"darwin/arm64":  true,
	}
	for _, p := range plats {
		key := p.GOOS + "/" + p.GOARCH
		if !want[key] {
			t.Errorf("unexpected platform %s", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing platforms: %v", want)
	}
}

func TestIsSupportedPlatform(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         bool
	}{
		{"linux", "amd64", true},
		{"linux", "arm64", true},
		{"windows", "amd64", true},
		{"darwin", "amd64", true},
		{"darwin", "arm64", true},
		{"linux", "386", false},
		{"windows", "arm64", false},
		{"freebsd", "amd64", false},
		{"darwin", "386", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := IsSupportedPlatform(tc.goos, tc.goarch); got != tc.want {
			t.Errorf("IsSupportedPlatform(%q,%q)=%v want %v", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestAssetName_Exact(t *testing.T) {
	cases := []struct {
		version string
		goos    string
		goarch  string
		want    string
	}{
		{"1.2.3", "linux", "amd64", "oss-server_1.2.3_linux_amd64.tar.gz"},
		{"v1.2.3", "linux", "amd64", "oss-server_1.2.3_linux_amd64.tar.gz"},
		{"1.2.3", "linux", "arm64", "oss-server_1.2.3_linux_arm64.tar.gz"},
		{"1.2.3", "darwin", "amd64", "oss-server_1.2.3_darwin_amd64.tar.gz"},
		{"1.2.3", "darwin", "arm64", "oss-server_1.2.3_darwin_arm64.tar.gz"},
		{"1.2.3", "windows", "amd64", "oss-server_1.2.3_windows_amd64.zip"},
		{"1.0.0-alpha.1", "linux", "amd64", "oss-server_1.0.0-alpha.1_linux_amd64.tar.gz"},
		{"v2.0.0-beta+build.1", "darwin", "arm64", "oss-server_2.0.0-beta+build.1_darwin_arm64.tar.gz"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got, err := AssetName(tc.version, tc.goos, tc.goarch)
			if err != nil {
				t.Fatalf("AssetName error: %v", err)
			}
			if got != tc.want {
				t.Errorf("AssetName(%q,%q,%q)=%q want %q", tc.version, tc.goos, tc.goarch, got, tc.want)
			}
		})
	}
}

func TestAssetName_Unsupported(t *testing.T) {
	unsupported := []struct{ goos, goarch string }{
		{"linux", "386"},
		{"windows", "arm64"},
		{"freebsd", "amd64"},
		{"windows", "386"},
	}
	for _, tc := range unsupported {
		_, err := AssetName("1.2.3", tc.goos, tc.goarch)
		if err == nil {
			t.Errorf("AssetName should fail for %s/%s", tc.goos, tc.goarch)
		} else if !IsUnsupportedPlatformError(err) {
			t.Errorf("expected unsupported platform error, got %v", err)
		}
	}
}

func TestAssetName_InvalidVersion(t *testing.T) {
	invalid := []string{"", "dev", "1.2", "01.2.3", "v", "not-semver"}
	for _, v := range invalid {
		_, err := AssetName(v, "linux", "amd64")
		if err == nil {
			t.Errorf("AssetName should fail for invalid version %q", v)
		}
	}
}

func TestExpectedAssetNames(t *testing.T) {
	m, err := ExpectedAssetNames("1.2.3")
	if err != nil {
		t.Fatalf("ExpectedAssetNames error: %v", err)
	}
	if len(m) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(m))
	}
	if got := m["linux/amd64"]; got != "oss-server_1.2.3_linux_amd64.tar.gz" {
		t.Errorf("linux/amd64 = %q", got)
	}
	if got := m["windows/amd64"]; got != "oss-server_1.2.3_windows_amd64.zip" {
		t.Errorf("windows/amd64 = %q", got)
	}
}
