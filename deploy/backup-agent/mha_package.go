package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	mhaPkgJobsMu sync.Mutex
	mhaPkgJobs   = map[string]*mhaPkgJob{}
)

type mhaPkgJob struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Log       string    `json:"log,omitempty"`
	UpdatedAt time.Time `json:"-"`
}

func putMHAPkgJob(id string, job *mhaPkgJob) {
	if job != nil {
		job.UpdatedAt = time.Now()
	}
	mhaPkgJobsMu.Lock()
	defer mhaPkgJobsMu.Unlock()
	mhaPkgJobs[id] = job
	now := time.Now()
	for jid, j := range mhaPkgJobs {
		if j == nil {
			delete(mhaPkgJobs, jid)
			continue
		}
		if strings.EqualFold(j.Status, "running") {
			continue
		}
		if j.UpdatedAt.IsZero() || now.Sub(j.UpdatedAt) > 2*time.Hour {
			delete(mhaPkgJobs, jid)
		}
	}
	for len(mhaPkgJobs) > 256 {
		var oldestID string
		var oldest time.Time
		for jid, j := range mhaPkgJobs {
			if j == nil || strings.EqualFold(j.Status, "running") {
				continue
			}
			if oldestID == "" || j.UpdatedAt.Before(oldest) {
				oldestID, oldest = jid, j.UpdatedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(mhaPkgJobs, oldestID)
	}
}

func handleMHAPackageInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DownloadURL string   `json:"download_url"`
		SHA256      string   `json:"sha256"`
		PackFormat  string   `json:"pack_format"` // rpm | deb
		Role        string   `json:"role"`        // manager | node
		Targets     []string `json:"targets"`     // empty/"local" or host for ssh
		SSHUser     string   `json:"ssh_user"`
		SSHPort     int      `json:"ssh_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	packFmt := strings.ToLower(strings.TrimSpace(req.PackFormat))
	if packFmt != "rpm" && packFmt != "deb" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "pack_format 须为 rpm 或 deb"})
		return
	}
	if strings.TrimSpace(req.DownloadURL) == "" || strings.TrimSpace(req.SHA256) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "download_url 与 sha256 必填"})
		return
	}
	jobID := fmt.Sprintf("%d", time.Now().UnixNano())
	if len(jobID) > 16 {
		jobID = jobID[len(jobID)-16:]
	}
	job := &mhaPkgJob{ID: jobID, Status: "running", Message: "installing"}
	putMHAPkgJob(jobID, job)

	go runMHAPackageInstall(job, req.DownloadURL, req.SHA256, packFmt, req.Role, req.Targets, req.SSHUser, req.SSHPort)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID})
}

func handleMHAPackageJob(w http.ResponseWriter, r *http.Request) {
	handlePackageJob(w, r)
}

func handlePackageJob(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	id := path
	for _, pfx := range []string{
		"/v1/mha/package/jobs/",
		"/v1/pgaf/package/jobs/",
		"/v1/patroni/package/jobs/",
	} {
		if strings.HasPrefix(path, pfx) {
			id = strings.TrimPrefix(path, pfx)
			break
		}
	}
	id = strings.Trim(id, "/")
	mhaPkgJobsMu.Lock()
	job := mhaPkgJobs[id]
	mhaPkgJobsMu.Unlock()
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
}

func runMHAPackageInstall(job *mhaPkgJob, url, wantSHA, packFmt, role string, targets []string, sshUser string, sshPort int) {
	appendLog := func(s string) {
		mhaPkgJobsMu.Lock()
		job.Log += s + "\n"
		mhaPkgJobsMu.Unlock()
	}
	fail := func(msg string) {
		mhaPkgJobsMu.Lock()
		job.Status = "failed"
		job.Message = msg
		job.UpdatedAt = time.Now()
		mhaPkgJobsMu.Unlock()
	}
	if *dryRun {
		appendLog("dry-run install " + packFmt + " role=" + role)
		mhaPkgJobsMu.Lock()
		job.Status = "success"
		job.Message = "dry-run ok"
		job.UpdatedAt = time.Now()
		mhaPkgJobsMu.Unlock()
		return
	}
	dir := filepath.Join(*workdir, "mha-pkg")
	_ = os.MkdirAll(dir, 0o750)
	local := filepath.Join(dir, "pkg."+packFmt)
	appendLog("download " + url)
	if err := downloadFile(url, local); err != nil {
		fail("download: " + err.Error())
		return
	}
	sum, err := fileSHA256(local)
	if err != nil {
		fail("checksum: " + err.Error())
		return
	}
	if !strings.EqualFold(sum, strings.TrimSpace(wantSHA)) {
		fail(fmt.Sprintf("sha256 mismatch got=%s want=%s", sum, wantSHA))
		return
	}
	appendLog("checksum ok")

	if len(targets) == 0 {
		targets = []string{"local"}
	}
	sshUser = strings.TrimSpace(sshUser)
	if sshUser == "" {
		sshUser = "root"
	}
	if sshPort <= 0 {
		sshPort = 22
	}
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" || t == "local" {
			appendLog("install local role=" + role)
			if err := installLocalPkg(local, packFmt); err != nil {
				fail(err.Error())
				return
			}
			continue
		}
		appendLog("install remote " + t)
		if err := installRemotePkg(local, packFmt, t, sshUser, sshPort); err != nil {
			fail(err.Error())
			return
		}
	}
	mhaPkgJobsMu.Lock()
	job.Status = "success"
	job.Message = "install ok"
	job.UpdatedAt = time.Now()
	mhaPkgJobsMu.Unlock()
}

func downloadFile(url, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func installLocalPkg(path, packFmt string) error {
	var cmd *exec.Cmd
	switch packFmt {
	case "rpm":
		cmd = exec.Command("rpm", "-Uvh", path)
	case "deb":
		cmd = exec.Command("dpkg", "-i", path)
	default:
		return fmt.Errorf("unsupported format")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, trimMHAOut(string(out)))
	}
	return nil
}

func installRemotePkg(localPath, packFmt, host, user string, port int) error {
	remote := fmt.Sprintf("/tmp/mha-pkg.%s", packFmt)
	scp := exec.Command("scp", "-P", fmt.Sprintf("%d", port), "-o", "StrictHostKeyChecking=accept-new",
		localPath, fmt.Sprintf("%s@%s:%s", user, host, remote))
	if out, err := scp.CombinedOutput(); err != nil {
		return fmt.Errorf("scp: %v (%s)", err, trimMHAOut(string(out)))
	}
	var remoteCmd string
	switch packFmt {
	case "rpm":
		remoteCmd = "rpm -Uvh " + remote
	case "deb":
		remoteCmd = "dpkg -i " + remote
	}
	ssh := exec.Command("ssh", "-p", fmt.Sprintf("%d", port), "-o", "StrictHostKeyChecking=accept-new",
		fmt.Sprintf("%s@%s", user, host), remoteCmd)
	if out, err := ssh.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh install: %v (%s)", err, trimMHAOut(string(out)))
	}
	return nil
}
