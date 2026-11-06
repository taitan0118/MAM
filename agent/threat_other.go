//go:build !windows

package main

// Chỉ Windows có Defender; các HĐH khác chưa quét.
func scanThreats() []Threat { return nil }
