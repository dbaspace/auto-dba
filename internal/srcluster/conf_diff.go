package srcluster

import (
	"sort"
	"strings"
)

// ConfKeyChange 参数级差异（忽略注释、空行与顺序）。
type ConfKeyChange struct {
	Key string `json:"key"`
	Op  string `json:"op"` // add | del | change
	Old string `json:"old,omitempty"`
	New string `json:"new,omitempty"`
}

// ConfLine 逐行差异的一行；OldNo/NewNo 从 1 开始，0 表示该侧无此行。
type ConfLine struct {
	Op    string `json:"op"` // equal | add | del
	OldNo int    `json:"old_no,omitempty"`
	NewNo int    `json:"new_no,omitempty"`
	Text  string `json:"text"`
}

// parseConfKV 解析 key = value；重复键以最后一次为准。
func parseConfKV(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		i := strings.Index(t, "=")
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(t[:i])
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(t[i+1:])
	}
	return out
}

// ConfKeyDiff 返回 old → new 的参数增删改，按 key 排序。
func ConfKeyDiff(oldContent, newContent string) []ConfKeyChange {
	o, n := parseConfKV(oldContent), parseConfKV(newContent)
	out := []ConfKeyChange{}
	for k, nv := range n {
		ov, ok := o[k]
		switch {
		case !ok:
			out = append(out, ConfKeyChange{Key: k, Op: "add", New: nv})
		case ov != nv:
			out = append(out, ConfKeyChange{Key: k, Op: "change", Old: ov, New: nv})
		}
	}
	for k, ov := range o {
		if _, ok := n[k]; !ok {
			out = append(out, ConfKeyChange{Key: k, Op: "del", Old: ov})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// confLCSMaxCells 超过该规模不做 LCS，退化为整段删除 + 整段新增，避免大文件吃内存。
const confLCSMaxCells = 4_000_000

func splitConfLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// ConfLineDiff 基于 LCS 的逐行差异（行尾空白不计入比较）。
func ConfLineDiff(oldContent, newContent string) []ConfLine {
	a, b := splitConfLines(oldContent), splitConfLines(newContent)
	eq := func(i, j int) bool {
		return strings.TrimRight(a[i], " \t") == strings.TrimRight(b[j], " \t")
	}
	out := make([]ConfLine, 0, len(a)+len(b))

	pre := 0
	for pre < len(a) && pre < len(b) && eq(pre, pre) {
		out = append(out, ConfLine{Op: "equal", OldNo: pre + 1, NewNo: pre + 1, Text: b[pre]})
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && eq(len(a)-1-suf, len(b)-1-suf) {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	out = append(out, lcsMiddle(ma, mb, pre, eq)...)
	for k := 0; k < suf; k++ {
		i, j := len(a)-suf+k, len(b)-suf+k
		out = append(out, ConfLine{Op: "equal", OldNo: i + 1, NewNo: j + 1, Text: b[j]})
	}
	return out
}

func lcsMiddle(a, b []string, off int, eq func(i, j int) bool) []ConfLine {
	n, m := len(a), len(b)
	out := make([]ConfLine, 0, n+m)
	if n*m > confLCSMaxCells || n == 0 || m == 0 {
		for i := range a {
			out = append(out, ConfLine{Op: "del", OldNo: off + i + 1, Text: a[i]})
		}
		for j := range b {
			out = append(out, ConfLine{Op: "add", NewNo: off + j + 1, Text: b[j]})
		}
		return out
	}
	// dp[i][j] = a[i:] 与 b[j:] 的 LCS 长度
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if eq(off+i, off+j) {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case eq(off+i, off+j):
			out = append(out, ConfLine{Op: "equal", OldNo: off + i + 1, NewNo: off + j + 1, Text: b[j]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, ConfLine{Op: "del", OldNo: off + i + 1, Text: a[i]})
			i++
		default:
			out = append(out, ConfLine{Op: "add", NewNo: off + j + 1, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, ConfLine{Op: "del", OldNo: off + i + 1, Text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, ConfLine{Op: "add", NewNo: off + j + 1, Text: b[j]})
	}
	return out
}

// nodeLocalConfKeys 每台机器各不相同的键：批量下发时只改源节点，不跟随到同角色其他节点。
var nodeLocalConfKeys = map[string]bool{
	"frontend_address":  true,
	"priority_networks": true,
}

// SplitNodeLocalChanges 把参数改动分成公共改动与本机改动。
func SplitNodeLocalChanges(changes []ConfKeyChange) (common, local []ConfKeyChange) {
	for _, c := range changes {
		if nodeLocalConfKeys[c.Key] {
			local = append(local, c)
		} else {
			common = append(common, c)
		}
	}
	return
}

// confLineKey 返回非注释 key = value 行的键；其他行返回空。
func confLineKey(line string) string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return ""
	}
	i := strings.Index(t, "=")
	if i <= 0 {
		return ""
	}
	return strings.TrimSpace(t[:i])
}

// replaceConfValue 保留原行 "key =" 及等号后的空白风格，只替换值。
func replaceConfValue(line, value string) string {
	i := strings.Index(line, "=")
	j := i + 1
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	return line[:j] + value
}

// ConfKeyPatch 把参数改动应用到 conf 正文：修改替换该键最后一次出现的行（无则追加），
// 新增同理，删除去掉该键全部非注释行；其余行（含注释、顺序）原样保留。
func ConfKeyPatch(content string, changes []ConfKeyChange) string {
	if len(changes) == 0 {
		return content
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	pos := map[string][]int{}
	for i, l := range lines {
		if k := confLineKey(l); k != "" {
			pos[k] = append(pos[k], i)
		}
	}
	drop := map[int]bool{}
	var appended []string
	for _, c := range changes {
		at := pos[c.Key]
		switch c.Op {
		case "add", "change":
			if len(at) > 0 {
				last := at[len(at)-1]
				lines[last] = replaceConfValue(lines[last], c.New)
			} else {
				appended = append(appended, c.Key+" = "+c.New)
			}
		case "del":
			for _, p := range at {
				drop[p] = true
			}
		}
	}
	out := make([]string, 0, len(lines)+len(appended))
	for i, l := range lines {
		if !drop[i] {
			out = append(out, l)
		}
	}
	body := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if len(appended) > 0 {
		body += "\n" + strings.Join(appended, "\n")
	}
	return body + "\n"
}

// ConfLineStats 统计新增 / 删除行数。
func ConfLineStats(lines []ConfLine) (added, deleted int) {
	for _, l := range lines {
		switch l.Op {
		case "add":
			added++
		case "del":
			deleted++
		}
	}
	return
}
