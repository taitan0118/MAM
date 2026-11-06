//go:build !windows

package main

func maintClean() Result { return fail("Chức năng này chỉ có trên Windows") }
func maintRAM() Result   { return fail("Chức năng này chỉ có trên Windows") }
