//go:build !server && (!linux || gtk3)

package bootstrap

import "unsafe"

func installNativeGraphPinch(_ unsafe.Pointer) {}
