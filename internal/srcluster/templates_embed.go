package srcluster

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed templates/start.sh
var startScriptTemplate string

//go:embed templates/node-agent.service
var unitTemplate string

func renderStartScript(listen, user, pass, binPath string) string {
	s := startScriptTemplate
	s = strings.ReplaceAll(s, "{{LISTEN}}", listen)
	s = strings.ReplaceAll(s, "{{USER}}", user)
	s = strings.ReplaceAll(s, "{{PASS}}", pass)
	s = strings.ReplaceAll(s, "{{BIN}}", binPath)
	return s
}

func renderUnit(remoteDir, listen, user, pass string) string {
	s := unitTemplate
	s = strings.ReplaceAll(s, "{{REMOTE_DIR}}", remoteDir)
	s = strings.ReplaceAll(s, "{{LISTEN}}", listen)
	s = strings.ReplaceAll(s, "{{USER}}", user)
	s = strings.ReplaceAll(s, "{{PASS}}", pass)
	return s
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func mustListen(port int) string {
	if port <= 0 {
		port = 8091
	}
	return fmt.Sprintf("0.0.0.0:%d", port)
}
