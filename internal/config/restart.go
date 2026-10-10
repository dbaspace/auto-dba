package config

import (
	"fmt"
	"log"
	"os"
	"syscall"
)

// SelfRestart 用当前可执行文件与 os.Args 替换本进程（用于 http.port 等需重新 Listen 的变更）。
func SelfRestart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	log.Printf("nacos watch: self-restart → %s %v", exe, os.Args)
	return syscall.Exec(exe, os.Args, os.Environ())
}
