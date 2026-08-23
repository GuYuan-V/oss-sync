package update

// newTestCandidate is test-only; it must not exist in shipped candidate.go.
// Provides deterministic IDs/digest so legacy 6-arg call sites keep working
// while production NewCandidate requires real identity.
func newTestCandidate(tag, goos, goarch, assetURL, releaseURL string, size int64) (*Candidate, error) {
	const placeholderDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return NewCandidate(tag, goos, goarch, assetURL, releaseURL, size, 1, 1, placeholderDigest)
}
