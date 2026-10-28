//go:build windows

package main

import "unsafe"

// unsafePointer keeps unsafe imports in one place for syscall argument passing.
func unsafePointer[T any](p *T) unsafe.Pointer { return unsafe.Pointer(p) }
