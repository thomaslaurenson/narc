//go:build !production

package proxy

// ResetForTesting resets the package-level proxyCreated guard so that tests
// can create multiple Proxy instances across independent test cases.
// Must not be called from production code.
func ResetForTesting() {
	proxyCreated.Store(false)
}
