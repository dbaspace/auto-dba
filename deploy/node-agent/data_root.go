package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// BE 数据目录三条规则：
//  1. 目录不存在：lsblk -f 找未使用磁盘，有则审批执行时格式化挂载，无则失败；
//  2. 目录存在但位于系统盘：失败；
//  3. 目录存在且位于非系统盘：直接使用。

// blockDev lsblk 扁平化后的一个设备出现（LVM/RAID 在多个父设备下会出现多次）。
type blockDev struct {
	Name        string
	KName       string
	Parent      string // 父设备 kname；顶层为空
	Type        string
	FSType      string
	Mountpoint  string
	Size        string
	RO          bool
	HasChildren bool
}

type mountEntry struct {
	Mountpoint string
	FSType     string
	Source     string
}

type dataRootResult struct {
	OK       bool
	Rule     int // 1/2/3；0 表示无法判定
	Message  string
	FreeDisk *blockDev // 规则 1 且有空闲盘时的候选
	Disk     string    // 规则 2/3：目录所在顶层磁盘
	Mount    *mountEntry
}

var systemMountpoints = map[string]bool{"/": true, "/boot": true, "/boot/efi": true, "[SWAP]": true}

type lsblkJSONDev struct {
	Name       string         `json:"name"`
	KName      string         `json:"kname"`
	Type       string         `json:"type"`
	FSType     any            `json:"fstype"`
	Mountpoint any            `json:"mountpoint"`
	Size       any            `json:"size"`
	RO         any            `json:"ro"`
	Children   []lsblkJSONDev `json:"children"`
}

func anyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(x)
	}
}

func anyBool(v any) bool {
	s := strings.ToLower(anyStr(v))
	return s == "1" || s == "true"
}

// parseLsblkJSON 解析 lsblk -J -o NAME,KNAME,TYPE,FSTYPE,MOUNTPOINT,SIZE,RO。
func parseLsblkJSON(b []byte) ([]blockDev, error) {
	var doc struct {
		Blockdevices []lsblkJSONDev `json:"blockdevices"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	var out []blockDev
	var walk func(d lsblkJSONDev, parent string)
	walk = func(d lsblkJSONDev, parent string) {
		k := d.KName
		if k == "" {
			k = d.Name
		}
		out = append(out, blockDev{
			Name: d.Name, KName: k, Parent: parent, Type: d.Type,
			FSType: anyStr(d.FSType), Mountpoint: anyStr(d.Mountpoint),
			Size: anyStr(d.Size), RO: anyBool(d.RO), HasChildren: len(d.Children) > 0,
		})
		for _, c := range d.Children {
			walk(c, k)
		}
	}
	for _, d := range doc.Blockdevices {
		walk(d, "")
	}
	return out, nil
}

// parseLsblkPairs 解析 lsblk -P -o NAME,KNAME,PKNAME,TYPE,FSTYPE,MOUNTPOINT,SIZE,RO（旧版 lsblk 无 -J）。
func parseLsblkPairs(text string) []blockDev {
	var out []blockDev
	for _, line := range strings.Split(text, "\n") {
		kv := parsePairs(line)
		if len(kv) == 0 {
			continue
		}
		k := kv["KNAME"]
		if k == "" {
			k = kv["NAME"]
		}
		out = append(out, blockDev{
			Name: kv["NAME"], KName: k, Parent: kv["PKNAME"], Type: kv["TYPE"],
			FSType: kv["FSTYPE"], Mountpoint: kv["MOUNTPOINT"], Size: kv["SIZE"], RO: kv["RO"] == "1",
		})
	}
	has := map[string]bool{}
	for _, d := range out {
		if d.Parent != "" {
			has[d.Parent] = true
		}
	}
	for i := range out {
		out[i].HasChildren = has[out[i].KName]
	}
	return out
}

func parsePairs(line string) map[string]string {
	kv := map[string]string{}
	s := strings.TrimSpace(line)
	for s != "" {
		eq := strings.Index(s, "=\"")
		if eq <= 0 {
			break
		}
		key := strings.TrimSpace(s[:eq])
		rest := s[eq+2:]
		end := strings.Index(rest, "\"")
		if end < 0 {
			break
		}
		kv[key] = rest[:end]
		s = strings.TrimSpace(rest[end+1:])
	}
	return kv
}

// topDisks 设备出现所属的顶层磁盘 kname。
func topDisks(devs []blockDev, d blockDev) []string {
	parents := map[string][]string{}
	for _, x := range devs {
		if x.Parent != "" {
			parents[x.KName] = appendUniq(parents[x.KName], x.Parent)
		}
	}
	var out []string
	seen := map[string]bool{}
	var walk func(k string)
	walk = func(k string) {
		if seen[k] {
			return
		}
		seen[k] = true
		ps := parents[k]
		if len(ps) == 0 {
			out = appendUniq(out, k)
			return
		}
		for _, p := range ps {
			walk(p)
		}
	}
	walk(d.KName)
	sort.Strings(out)
	return out
}

func appendUniq(list []string, v string) []string {
	for _, s := range list {
		if s == v {
			return list
		}
	}
	return append(list, v)
}

// systemDisks 任一子孙挂载在 / /boot /boot/efi 或 SWAP 的顶层磁盘。
func systemDisks(devs []blockDev) map[string]bool {
	sys := map[string]bool{}
	for _, d := range devs {
		if systemMountpoints[d.Mountpoint] {
			for _, t := range topDisks(devs, d) {
				sys[t] = true
			}
		}
	}
	return sys
}

// freeDisks 等同 lsblk -f 判断：整盘、非只读、无文件系统、无挂载、无分区/子设备、非系统盘。
func freeDisks(devs []blockDev, sys map[string]bool) []blockDev {
	var out []blockDev
	seen := map[string]bool{}
	for _, d := range devs {
		if d.Type != "disk" || d.Parent != "" || d.RO || d.FSType != "" || d.Mountpoint != "" || d.HasChildren || sys[d.KName] {
			continue
		}
		if d.Size == "" || d.Size == "0" || d.Size == "0B" || seen[d.KName] {
			continue
		}
		seen[d.KName] = true
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KName < out[j].KName })
	return out
}

// parseMountinfo 解析 /proc/self/mountinfo。
func parseMountinfo(text string) []mountEntry {
	var out []mountEntry
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		sep := -1
		for i := 6; i < len(f); i++ {
			if f[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(f) {
			continue
		}
		out = append(out, mountEntry{
			Mountpoint: unescapeMount(f[4]),
			FSType:     f[sep+1],
			Source:     unescapeMount(f[sep+2]),
		})
	}
	return out
}

func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ownerMount 路径所属挂载（最长前缀；同一挂载点取最后一条，即最上层）。
func ownerMount(path string, mounts []mountEntry) *mountEntry {
	p := filepath.Clean(path)
	var best *mountEntry
	for i := range mounts {
		m := &mounts[i]
		mp := filepath.Clean(m.Mountpoint)
		if p != mp && !strings.HasPrefix(p, strings.TrimSuffix(mp, "/")+"/") {
			continue
		}
		if best == nil || len(mp) >= len(filepath.Clean(best.Mountpoint)) {
			best = m
		}
	}
	return best
}

// devMatchesMount lsblk 设备是否为该挂载的来源（按挂载点或设备路径）。
func devMatchesMount(d blockDev, m *mountEntry) bool {
	if d.Mountpoint != "" && filepath.Clean(d.Mountpoint) == filepath.Clean(m.Mountpoint) {
		return true
	}
	src := m.Source
	if !strings.HasPrefix(src, "/dev/") {
		return false
	}
	for _, p := range []string{"/dev/" + d.KName, "/dev/" + d.Name, "/dev/mapper/" + d.Name} {
		if d.Name != "" && src == p {
			return true
		}
	}
	return false
}

func diskLabel(d blockDev) string {
	if d.Size != "" {
		return fmt.Sprintf("/dev/%s（%s）", d.KName, d.Size)
	}
	return "/dev/" + d.KName
}

func freeDiskNames(free []blockDev) string {
	var s []string
	for _, d := range free {
		s = append(s, d.KName)
	}
	return strings.Join(s, ",")
}

// checkDataRoot 按三条规则判定；exists=false 表示目录不存在。
func checkDataRoot(dataRoot string, exists, isDir bool, mounts []mountEntry, devs []blockDev) dataRootResult {
	dr := filepath.Clean(strings.TrimSpace(dataRoot))
	if !filepath.IsAbs(dr) || dr == "/" {
		return dataRootResult{Message: fmt.Sprintf("数据目录 %q 须为绝对路径且不能是 /", dataRoot)}
	}
	sys := systemDisks(devs)
	free := freeDisks(devs, sys)

	if !exists {
		if len(free) == 0 {
			return dataRootResult{Rule: 1, Message: fmt.Sprintf("数据目录 %s 不存在，且没有未使用的磁盘（lsblk -f），请先加盘", dr)}
		}
		d := free[0]
		return dataRootResult{OK: true, Rule: 1, FreeDisk: &d,
			Message: fmt.Sprintf("数据目录 %s 不存在；检测到未使用磁盘 %s，审批执行时将格式化为 xfs 并挂载到 %s", dr, diskLabel(d), dr)}
	}
	if !isDir {
		return dataRootResult{Message: fmt.Sprintf("数据目录 %s 已存在但不是目录", dr)}
	}

	m := ownerMount(dr, mounts)
	if m == nil {
		return dataRootResult{Message: fmt.Sprintf("无法识别数据目录 %s 所属挂载", dr)}
	}
	var disks []string
	for _, d := range devs {
		if devMatchesMount(d, m) {
			for _, t := range topDisks(devs, d) {
				disks = appendUniq(disks, t)
			}
		}
	}
	sort.Strings(disks)
	onSystem := len(disks) == 0 && filepath.Clean(m.Mountpoint) == "/"
	for _, t := range disks {
		if sys[t] {
			onSystem = true
		}
	}
	diskStr := strings.Join(disks, "+")
	if onSystem {
		if diskStr == "" {
			diskStr = "系统盘"
		}
		msg := fmt.Sprintf("数据目录 %s 位于系统盘 %s（%s 挂载于 %s），BE 数据目录不能使用系统盘", dr, diskStr, m.Source, m.Mountpoint)
		if len(free) > 0 {
			msg += fmt.Sprintf("；检测到未使用磁盘 %s，可改用一个不存在的目录名由平台格式化挂载，或手工挂载到 %s 后重试", freeDiskNames(free), dr)
		}
		return dataRootResult{Rule: 2, Message: msg, Disk: diskStr, Mount: m}
	}
	if len(disks) == 0 {
		return dataRootResult{Message: fmt.Sprintf("无法识别数据目录 %s 所在磁盘（挂载 %s，类型 %s，来源 %s）", dr, m.Mountpoint, m.FSType, m.Source), Mount: m}
	}
	return dataRootResult{OK: true, Rule: 3, Disk: diskStr, Mount: m,
		Message: fmt.Sprintf("数据目录 %s 位于 %s（%s %s，挂载于 %s），可直接使用", dr, diskStr, m.Source, m.FSType, m.Mountpoint)}
}

// ---- 采集 ----

func readBlockDevs() ([]blockDev, error) {
	out, err := exec.Command("lsblk", "-J", "-o", "NAME,KNAME,TYPE,FSTYPE,MOUNTPOINT,SIZE,RO").Output()
	if err == nil {
		if devs, perr := parseLsblkJSON(out); perr == nil {
			return devs, nil
		}
	}
	out, err = exec.Command("lsblk", "-P", "-o", "NAME,KNAME,PKNAME,TYPE,FSTYPE,MOUNTPOINT,SIZE,RO").Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk 执行失败: %v", err)
	}
	return parseLsblkPairs(string(out)), nil
}

func readMounts() ([]mountEntry, error) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("读取 /proc/self/mountinfo 失败: %v", err)
	}
	return parseMountinfo(string(b)), nil
}

func evalDataRoot(dataRoot string) (dataRootResult, []blockDev, error) {
	devs, err := readBlockDevs()
	if err != nil {
		return dataRootResult{}, nil, err
	}
	mounts, err := readMounts()
	if err != nil {
		return dataRootResult{}, devs, err
	}
	dr := filepath.Clean(strings.TrimSpace(dataRoot))
	fi, statErr := os.Stat(dr)
	exists := statErr == nil
	res := checkDataRoot(dr, exists, exists && fi.IsDir(), mounts, devs)
	if res.OK && res.Rule == 3 {
		var st syscall.Statfs_t
		if syscall.Statfs(dr, &st) == nil {
			total := uint64(st.Blocks) * uint64(st.Bsize)
			avail := uint64(st.Bavail) * uint64(st.Bsize)
			res.Message += fmt.Sprintf("；总 %s 可用 %s", humanBytes(total), humanBytes(avail))
		}
	}
	return res, devs, nil
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

// dirNonEmpty 目录中是否有 lost+found 以外的内容。
func dirNonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() != "lost+found" {
			return true
		}
	}
	return false
}

// diskSummary extra_disk 提醒信息。
func diskSummary(devs []blockDev) string {
	sys := systemDisks(devs)
	free := freeDisks(devs, sys)
	var sysList, mounted []string
	for k := range sys {
		sysList = append(sysList, k)
	}
	sort.Strings(sysList)
	for _, d := range devs {
		if d.Mountpoint != "" && !systemMountpoints[d.Mountpoint] {
			mounted = appendUniq(mounted, d.Name+" "+d.Mountpoint)
		}
	}
	var freeList []string
	for _, d := range free {
		freeList = append(freeList, fmt.Sprintf("%s(%s)", d.KName, d.Size))
	}
	orNone := func(s []string) string {
		if len(s) == 0 {
			return "无"
		}
		return strings.Join(s, ", ")
	}
	return fmt.Sprintf("未使用磁盘: %s；系统盘: %s；已挂载: %s", orNone(freeList), orNone(sysList), orNone(mounted))
}

// autoDiskInit 按三条规则处理 BE 数据目录：规则 3 直接使用，规则 2 拒绝，规则 1 格式化空闲盘并挂载。
func autoDiskInit(dataRoot string, persistFstab, dry bool) (string, error) {
	dr := filepath.Clean(strings.TrimSpace(dataRoot))
	res, devs, err := evalDataRoot(dr)
	if err != nil {
		return "", err
	}
	if !res.OK {
		return "", fmt.Errorf("%s", res.Message)
	}
	if res.Rule == 3 {
		return res.Message, nil
	}
	if res.FreeDisk == nil {
		return "", fmt.Errorf("数据目录 %s 未就绪且无候选磁盘", dr)
	}
	d := *res.FreeDisk
	dev := "/dev/" + d.KName
	sys := systemDisks(devs)
	if sys[d.KName] || d.HasChildren || d.FSType != "" || d.Mountpoint != "" || d.RO || d.Type != "disk" {
		return "", fmt.Errorf("拒绝格式化 %s：不是未使用的非系统盘", dev)
	}
	if dry {
		return fmt.Sprintf("dry-run：将格式化 %s 为 xfs 并挂载到 %s", dev, dr), nil
	}
	if _, err := exec.LookPath("mkfs.xfs"); err != nil {
		return "", fmt.Errorf("mkfs.xfs 不存在，请先安装 xfsprogs")
	}
	if out, err := exec.Command("mkfs.xfs", "-f", dev).CombinedOutput(); err != nil {
		return "", fmt.Errorf("mkfs.xfs %s 失败: %s %v", dev, strings.TrimSpace(string(out)), err)
	}
	if err := os.MkdirAll(dr, 0o755); err != nil {
		return "", fmt.Errorf("创建 %s 失败: %v", dr, err)
	}
	if out, err := exec.Command("mount", dev, dr).CombinedOutput(); err != nil {
		return "", fmt.Errorf("mount %s %s 失败: %s %v", dev, dr, strings.TrimSpace(string(out)), err)
	}
	msg := fmt.Sprintf("已格式化 %s 为 xfs 并挂载到 %s", dev, dr)
	if persistFstab {
		uuidOut, err := exec.Command("blkid", "-s", "UUID", "-o", "value", dev).Output()
		uuid := strings.TrimSpace(string(uuidOut))
		switch {
		case err != nil || uuid == "":
			msg += "；未取到 UUID，未写入 /etc/fstab，请手工持久化"
		default:
			fstab, _ := os.ReadFile("/etc/fstab")
			if fstabHasMountpoint(string(fstab), dr) {
				msg += "；/etc/fstab 已有 " + dr + " 记录，未重复写入，请核对"
			} else if f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
				msg += "；写入 /etc/fstab 失败: " + err.Error()
			} else {
				_, werr := fmt.Fprintf(f, "UUID=%s %s xfs defaults,noatime 0 0\n", uuid, dr)
				_ = f.Close()
				if werr != nil {
					msg += "；写入 /etc/fstab 失败: " + werr.Error()
				} else {
					msg += "；已写入 /etc/fstab（UUID=" + uuid + "）"
				}
			}
		}
	}
	mounts, err := readMounts()
	if err != nil {
		return "", err
	}
	if m := ownerMount(dr, mounts); m == nil || filepath.Clean(m.Mountpoint) != dr {
		return "", fmt.Errorf("挂载后校验失败：%s 不是挂载点", dr)
	}
	return msg, nil
}

// fstabHasMountpoint /etc/fstab 是否已有该挂载点。
func fstabHasMountpoint(fstab, mp string) bool {
	mp = filepath.Clean(mp)
	for _, line := range strings.Split(fstab, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 && filepath.Clean(unescapeMount(f[1])) == mp {
			return true
		}
	}
	return false
}
