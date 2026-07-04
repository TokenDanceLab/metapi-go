//go:build !debug

package app

// StartDebugServer is a no-op when the "debug" build tag is not set.
func StartDebugServer(port int) {}
