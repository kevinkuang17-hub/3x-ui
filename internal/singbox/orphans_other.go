//go:build !linux

package singbox

func killStray(string) int { return 0 }
