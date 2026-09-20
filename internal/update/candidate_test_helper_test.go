package update

// newTestCandidate 仅供测试使用，不得出现在正式 candidate.go 中。
// 该辅助函数提供确定性 ID 与 digest，兼容旧 6 参数调用，而生产 NewCandidate 要求真实身份。
func newTestCandidate(tag, goos, goarch, assetURL, releaseURL string, size int64) (*Candidate, error) {
	const placeholderDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return NewCandidate(tag, goos, goarch, assetURL, releaseURL, size, 1, 1, placeholderDigest)
}
