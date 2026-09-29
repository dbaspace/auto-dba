package sshx

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

// Upload 经 SFTP 上传本地文件到远端（先写 .new 再 rename，自动建目录，chmod）。
func (c *Client) Upload(localPath, remotePath string) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("SSH 未连接")
	}
	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return err
	}
	sftpCl, err := sftp.NewClient(c.conn)
	if err != nil {
		return fmt.Errorf("创建 SFTP 失败: %w", err)
	}
	defer sftpCl.Close()

	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return fmt.Errorf("远端路径为空")
	}
	dir := path.Dir(remotePath)
	if dir != "" && dir != "." && dir != "/" {
		if err := sftpCl.MkdirAll(dir); err != nil {
			return fmt.Errorf("创建远端目录 %s 失败: %w%s", dir, err, writePermHint(err))
		}
	}

	tmpPath := remotePath + ".new"
	_ = sftpCl.Remove(tmpPath)
	dst, err := sftpCl.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建远端临时文件失败: %w%s", err, writePermHint(err))
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = sftpCl.Remove(tmpPath)
		return fmt.Errorf("上传失败: %w%s", err, writePermHint(err))
	}
	if err := dst.Close(); err != nil {
		_ = sftpCl.Remove(tmpPath)
		return err
	}
	mode := fi.Mode().Perm()
	if mode == 0 {
		mode = 0o755
	}
	_ = sftpCl.Chmod(tmpPath, mode)

	// 替换目标：先删旧文件再 rename（运行中的二进制原地 Create 会 ETXTBSY / SSH_FX_FAILURE）
	_ = sftpCl.Remove(remotePath)
	if err := sftpCl.Rename(tmpPath, remotePath); err != nil {
		_ = sftpCl.Remove(remotePath)
		if err2 := sftpCl.Rename(tmpPath, remotePath); err2 != nil {
			_ = sftpCl.Remove(tmpPath)
			return fmt.Errorf("替换远端文件失败: %w%s", err2, writePermHint(err2))
		}
	}
	_ = sftpCl.Chmod(remotePath, mode)
	return nil
}

// WriteFile 经 SFTP 写入远端文件内容。
func (c *Client) WriteFile(remotePath string, data []byte, mode os.FileMode) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("SSH 未连接")
	}
	sftpCl, err := sftp.NewClient(c.conn)
	if err != nil {
		return fmt.Errorf("创建 SFTP 失败: %w", err)
	}
	defer sftpCl.Close()

	remotePath = strings.TrimSpace(remotePath)
	dir := path.Dir(remotePath)
	if dir != "" && dir != "." && dir != "/" {
		if err := sftpCl.MkdirAll(dir); err != nil {
			return fmt.Errorf("创建远端目录 %s 失败: %w%s", dir, err, writePermHint(err))
		}
	}
	tmpPath := remotePath + ".new"
	_ = sftpCl.Remove(tmpPath)
	dst, err := sftpCl.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建远端文件失败: %w%s", err, writePermHint(err))
	}
	if _, err := dst.Write(data); err != nil {
		_ = dst.Close()
		_ = sftpCl.Remove(tmpPath)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = sftpCl.Remove(tmpPath)
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	_ = sftpCl.Chmod(tmpPath, mode)
	_ = sftpCl.Remove(remotePath)
	if err := sftpCl.Rename(tmpPath, remotePath); err != nil {
		_ = sftpCl.Remove(tmpPath)
		return fmt.Errorf("替换远端文件失败: %w%s", err, writePermHint(err))
	}
	return sftpCl.Chmod(remotePath, mode)
}

// MkdirAll 创建远端目录。
func (c *Client) MkdirAll(remoteDir string) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("SSH 未连接")
	}
	sftpCl, err := sftp.NewClient(c.conn)
	if err != nil {
		return err
	}
	defer sftpCl.Close()
	return sftpCl.MkdirAll(remoteDir)
}

func writePermHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "permission denied") || strings.Contains(msg, "permission_denied") {
		return "；请改用 SSH 用户可写目录（如 /home/<user>/auto-dba/backup-agent），勿填无权限的 /opt"
	}
	if strings.Contains(msg, "failure") || strings.Contains(msg, "ssh_fx_failure") ||
		strings.Contains(msg, "text file busy") || strings.Contains(msg, "etxtbsy") ||
		strings.Contains(msg, "busy") {
		return "；可能目标二进制仍在运行，请先停止 Agent 再下发"
	}
	return ""
}
