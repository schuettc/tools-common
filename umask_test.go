//go:build unix

package tools

import "syscall"

func syscallUmask(m int) int { return syscall.Umask(m) }
