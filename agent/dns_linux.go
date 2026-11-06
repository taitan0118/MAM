//go:build linux

package main

import "errors"

// Linux (systemd-resolved, NetworkManager... mỗi bản một kiểu): chưa đổi DNS hệ thống, chỉ chặn bằng /etc/hosts.
const dnsSupported = false

func dnsBackupExists() bool       { return false }
func dnsApply() ([]string, error) { return nil, errors.New("chưa hỗ trợ trên Linux") }
func dnsRestore()                 {}
