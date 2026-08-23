package version

import "testing"

func TestParse_Valid(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"V1.2.3", "1.2.3"},
		{"0.0.0", "0.0.0"},
		{"1.0.0-alpha", "1.0.0-alpha"},
		{"1.0.0-alpha.1", "1.0.0-alpha.1"},
		{"1.0.0-0.3.7", "1.0.0-0.3.7"},
		{"1.0.0-x.7.z.92", "1.0.0-x.7.z.92"},
		{"1.0.0+20130313144700", "1.0.0+20130313144700"},
		{"1.0.0-beta+exp.sha.5114f85", "1.0.0-beta+exp.sha.5114f85"},
		{"1.0.0-alpha+001", "1.0.0-alpha+001"},
		{"1.0.0+001", "1.0.0+001"},
		{" v1.2.3 ", "1.2.3"},
		{"1.2.3-rc.1+build.1", "1.2.3-rc.1+build.1"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.input, err)
			}
			if got.String() != tc.want {
				t.Errorf("Parse(%q) = %q, want %q", tc.input, got.String(), tc.want)
			}
			if !IsValid(tc.input) {
				t.Errorf("IsValid(%q) should be true", tc.input)
			}
		})
	}
}

func TestParse_Invalid(t *testing.T) {
	cases := []string{
		"",
		" ",
		"v",
		"dev",
		"1.2",
		"1.2.3.4",
		"01.2.3",
		"1.02.3",
		"1.2.03",
		"1.2.3-",
		"1.2.3+",
		"1.2.3-alpha..1",
		"1.2.3-01",
		"1.2.3-alpha.01",
		"1.2.3-a*b",
		"1.2.3-",
		"a.b.c",
		"1.2.3-",
		"1.0.0-alpha_beta", // underscore not allowed
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Errorf("Parse(%q) expected error, got nil", input)
			}
			if IsValid(input) {
				t.Errorf("IsValid(%q) should be false", input)
			}
			if _, err := Compare(input, "1.0.0"); err == nil {
				t.Errorf("Compare with invalid %q should error", input)
			}
		})
	}
}

func TestCompare_Precendence(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "2.0.0", -1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-beta.11", "1.0.0-rc.1", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-alpha", 0},
		{"v1.2.3", "1.2.3", 0},
		{"1.0.0-alpha.1", "1.0.0-alpha.1", 0},
		{"1.0.0+build1", "1.0.0+build2", 0}, // build ignored
		{"1.0.0-alpha+build", "1.0.0-alpha", 0},
		{"1.0.0-1", "1.0.0-alpha", -1}, // numeric prerelease < alphanumeric
		{"1.0.0-alpha.1", "1.0.0-alpha.1+build", 0},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			got, err := Compare(tc.a, tc.b)
			if err != nil {
				t.Fatalf("Compare(%q,%q) error: %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Errorf("Compare(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
			// symmetry
			rev, _ := Compare(tc.b, tc.a)
			if rev != -tc.want {
				t.Errorf("Compare symmetry %q vs %q = %d, want %d", tc.b, tc.a, rev, -tc.want)
			}
		})
	}
}

func TestCompare_Malformed_Rejects(t *testing.T) {
	if _, err := Compare("dev", "1.0.0"); err == nil {
		t.Error("Compare with dev should error")
	}
	if _, err := Compare("1.0.0", "dev"); err == nil {
		t.Error("Compare with dev should error")
	}
	if _, err := Compare("", "1.0.0"); err == nil {
		t.Error("Compare with empty should error")
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.2.3", "1.2.3"},
		{"V1.2.3", "1.2.3"},
		{"  v1.2.3  ", "1.2.3"},
		{"1.2.3", "1.2.3"},
	}
	for _, tc := range cases {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
