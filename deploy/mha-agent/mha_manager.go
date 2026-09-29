package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func mhaManagerUnitName(conf string) string {
	stem := strings.TrimSuffix(filepath.Base(conf), filepath.Ext(conf))
	if stem == "" {
		stem = "app"
	}
	stem = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, stem)
	return "mha-manager@" + stem + ".service"
}

func ensureMHAManagerUnit(conf string) (string, error) {
	unit := mhaManagerUnitName(conf)
	unitPath := filepath.Join("/etc/systemd/system", unit)
	bin, lookErr := exec.LookPath("masterha_manager")
	if lookErr != nil {
		bin = "masterha_manager"
	}
	body := fmt.Sprintf(`[Unit]
Description=MHA Manager for %s
After=network.target

[Service]
Type=simple
ExecStart=%s --conf=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, conf, bin, conf)
	if *dryRun {
		return unit, nil
	}
	if err := os.WriteFile(unitPath, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write unit: %w", err)
	}
	ctxOut, err := exec.Command("systemctl", "daemon-reload").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("daemon-reload: %w (%s)", err, strings.TrimSpace(string(ctxOut)))
	}
	return unit, nil
}

func handleMHAManagerStatus(w http.ResponseWriter, r *http.Request) {
	conf, err := resolveConf(r.URL.Query().Get("conf"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	unit := mhaManagerUnitName(conf)
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "active": false, "unit": unit, "conf": conf, "message": "dry-run",
		})
		return
	}
	out, err := exec.Command("systemctl", "is-active", unit).CombinedOutput()
	state := strings.TrimSpace(string(out))
	active := err == nil && state == "active"
	detailOut, _ := exec.Command("systemctl", "status", unit, "--no-pager", "-l").CombinedOutput()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "active": active, "state": state, "unit": unit, "conf": conf,
		"detail": trimOut(string(detailOut)),
	})
}

func handleMHAManagerCtrl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/v1/mha/manager/")
	action = strings.Trim(action, "/")
	var req struct {
		Conf string `json:"conf"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	conf, err := resolveConf(req.Conf)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	unit, err := ensureMHAManagerUnit(conf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	var sysAction string
	switch action {
	case "start":
		sysAction = "start"
	case "stop":
		sysAction = "stop"
	case "restart":
		sysAction = "restart"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "unknown action"})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run " + sysAction, "unit": unit, "conf": conf})
		return
	}
	out, err := exec.Command("systemctl", sysAction, unit).CombinedOutput()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "message": fmt.Sprintf("systemctl %s: %v", sysAction, err),
			"output": trimOut(string(out)), "unit": unit,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "message": sysAction + " ok", "unit": unit, "conf": conf,
		"output": trimOut(string(out)),
	})
}

func handleSwitchDead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Conf          string `json:"conf"`
		NewMasterHost string `json:"new_master_host"`
		NewMasterPort int    `json:"new_master_port"`
		TimeoutSec    int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	conf, err := resolveConf(req.Conf)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	args := []string{
		"--conf=" + conf,
		"--master_state=dead",
		"--interactive=0",
	}
	if strings.TrimSpace(req.NewMasterHost) != "" && req.NewMasterPort > 0 {
		args = append(args,
			"--new_master_host="+req.NewMasterHost,
			fmt.Sprintf("--new_master_port=%d", req.NewMasterPort),
		)
	}
	out, code, err := runMasterha(timeout, "masterha_master_switch", args...)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "output": trimOut(out), "exit_code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dead switch ok", "output": trimOut(out)})
}
