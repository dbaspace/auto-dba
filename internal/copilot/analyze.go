package copilot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const analyzePreamble = `
import builtins, sys, json
_allow = {"json", "math", "statistics", "datetime", "re", "collections", "builtins", "sys"}
_imp = builtins.__import__
def _guard(name, globals=None, locals=None, fromlist=(), level=0):
    root = name.split(".")[0]
    if root not in _allow:
        raise ImportError("forbidden: " + name)
    return _imp(name, globals, locals, fromlist, level)
builtins.__import__ = _guard
def _no_open(*a, **k):
    raise PermissionError("open forbidden")
builtins.open = _no_open
for _m in ("os", "subprocess", "socket", "shutil", "pathlib", "importlib"):
    sys.modules.pop(_m, None)
data = json.load(sys.stdin)
`

func vetScript(script string) error {
	script = strings.TrimSpace(script)
	if script == "" {
		return fmt.Errorf("分析脚本为空")
	}
	if len(script) > 8000 {
		return fmt.Errorf("分析脚本过长")
	}
	low := strings.ToLower(script)
	for _, bad := range []string{"subprocess", "socket", "open(", "import os", "importlib", "__import__", "os.", "shutil", "pathlib", "pty"} {
		if strings.Contains(low, bad) {
			return fmt.Errorf("脚本不允许 %s", bad)
		}
	}
	return nil
}

// runAnalyzeScript 在空目录里用 python3 -I 跑统计脚本。标准输入是上一步 JSON，变量 data 已装好。
func runAnalyzeScript(script, inputJSON string) (string, error) {
	if err := vetScript(script); err != nil {
		return "", err
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		return "", fmt.Errorf("未找到 python3")
	}
	dir, err := os.MkdirTemp("", "copilot-analyze-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "analyze.py")
	body := analyzePreamble + "\n" + script + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, "-I", path)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(inputJSON)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"PYTHONNOUSERSITE=1",
		"PYTHONDONTWRITEBYTECODE=1",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", trim(msg, 400))
	}
	if out == "" {
		return "", fmt.Errorf("脚本没有输出")
	}
	return trim(out, 4000), nil
}
