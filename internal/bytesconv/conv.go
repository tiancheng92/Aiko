// Package bytesconv provides zero-copy conversions between string and []byte.
// Both functions avoid memory allocation by reinterpreting the underlying
// pointer; callers must not mutate the returned slice or use the returned
// string after the original value is modified or garbage-collected.
package bytesconv

import "unsafe"

// StringToBytes converts s to a []byte without allocating.
// The returned slice shares memory with s — do not write to it.
func StringToBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// BytesToString converts b to a string without allocating.
// The returned string shares memory with b — do not modify b afterwards.
func BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}
