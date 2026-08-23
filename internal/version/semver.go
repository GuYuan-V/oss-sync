// 版本比较
package version

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SemVer 表示严格解析后的语义化版本。
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Build      string
	Raw        string
}

// String 返回规范化的版本字符串（不含 v 前缀）。
func (v SemVer) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// IsValid 判断字符串是否为严格 SemVer（允许可选的 v/V 前缀）。
func IsValid(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// Parse 严格解析版本字符串，允许可选的 v/V 前缀。
func Parse(s string) (SemVer, error) {
	raw := s
	s = strings.TrimSpace(s)
	if s == "" {
		return SemVer{}, errors.New("version is empty")
	}
	if s[0] == 'v' || s[0] == 'V' {
		s = s[1:]
		if s == "" {
			return SemVer{}, errors.New("version is empty after v prefix")
		}
	}

	// 分离 build metadata
	var build string
	if idx := strings.Index(s, "+"); idx >= 0 {
		build = s[idx+1:]
		s = s[:idx]
		if build == "" {
			return SemVer{}, errors.New("build metadata is empty")
		}
		if err := validateIdentifiers(build, "build"); err != nil {
			return SemVer{}, err
		}
	}

	// 分离 prerelease
	var prerelease string
	if idx := strings.Index(s, "-"); idx >= 0 {
		prerelease = s[idx+1:]
		s = s[:idx]
		if prerelease == "" {
			return SemVer{}, errors.New("prerelease is empty")
		}
		if err := validateIdentifiers(prerelease, "prerelease"); err != nil {
			return SemVer{}, err
		}
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return SemVer{}, fmt.Errorf("core version must have 3 dot-separated parts, got %d", len(parts))
	}
	nums := make([]int, 3)
	for i, p := range parts {
		if p == "" {
			return SemVer{}, errors.New("core version part is empty")
		}
		if !isNumeric(p) {
			return SemVer{}, fmt.Errorf("core version part %q is not numeric", p)
		}
		if hasLeadingZero(p) {
			return SemVer{}, fmt.Errorf("core version part %q has leading zero", p)
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return SemVer{}, fmt.Errorf("invalid numeric part %q", p)
		}
		nums[i] = n
	}

	return SemVer{
		Major:      nums[0],
		Minor:      nums[1],
		Patch:      nums[2],
		Prerelease: prerelease,
		Build:      build,
		Raw:        raw,
	}, nil
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func hasLeadingZero(s string) bool {
	return len(s) > 1 && s[0] == '0'
}

func validateIdentifiers(s, kind string) error {
	if s == "" {
		return fmt.Errorf("%s identifier is empty", kind)
	}
	parts := strings.Split(s, ".")
	for _, p := range parts {
		if p == "" {
			return fmt.Errorf("%s identifier part is empty", kind)
		}
		for _, c := range p {
			if !isIdentChar(c) {
				return fmt.Errorf("%s identifier %q contains invalid character %q", kind, p, string(c))
			}
		}
		if kind == "prerelease" && isNumeric(p) && hasLeadingZero(p) {
			return fmt.Errorf("%s numeric identifier %q has leading zero", kind, p)
		}
	}
	return nil
}

func isIdentChar(c rune) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		c == '-'
}

// Compare 比较两个版本字符串，返回 -1/0/1，任一非法时返回错误。
// 比较忽略 build metadata，prerelease 按 SemVer 规范排序。
func Compare(a, b string) (int, error) {
	av, err := Parse(a)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", a, err)
	}
	bv, err := Parse(b)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", b, err)
	}
	return compareParsed(av, bv), nil
}

// MustCompare 供测试或确定合法输入时使用，非法输入返回 0。
func MustCompare(a, b string) int {
	c, _ := Compare(a, b)
	return c
}

func compareParsed(a, b SemVer) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	// prerelease precedence
	aPreEmpty := a.Prerelease == ""
	bPreEmpty := b.Prerelease == ""
	if aPreEmpty && bPreEmpty {
		return 0
	}
	if aPreEmpty {
		return 1 // release > prerelease
	}
	if bPreEmpty {
		return -1
	}
	return comparePrerelease(a.Prerelease, b.Prerelease)
}

func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		if i >= len(ap) {
			return -1 // a shorter => lower precedence
		}
		if i >= len(bp) {
			return 1
		}
		x, y := ap[i], bp[i]
		if x == y {
			continue
		}
		xNum := isNumeric(x)
		yNum := isNumeric(y)
		if xNum && yNum {
			xn, _ := strconv.Atoi(x)
			yn, _ := strconv.Atoi(y)
			if xn < yn {
				return -1
			}
			return 1
		}
		if xNum != yNum {
			if xNum {
				return -1 // numeric < alphanumeric
			}
			return 1
		}
		if x < y {
			return -1
		}
		return 1
	}
	return 0
}

// Normalize 去除空白与可选的 v/V 前缀后返回规范化版本字符串。
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		return strings.TrimSpace(s[1:])
	}
	return s
}

