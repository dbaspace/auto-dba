package localcommand

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestStartWithPtyEcho(t *testing.T) {
	cmd := exec.Command("/bin/echo", "ok")
	f, err := startWithPty(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	if !bytes.Contains(buf[:n], []byte("ok")) {
		t.Fatalf("output %q", buf[:n])
	}
	_ = cmd.Wait()
}
