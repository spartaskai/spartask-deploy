//go:build !windows

package main

// disableQuickEdit concerns the Windows console only.
func disableQuickEdit() func() { return func() {} }
