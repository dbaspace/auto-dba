package srcluster

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// JDKInstallPriority update-alternatives 优先级。
const JDKInstallPriority = 1200

var (
	srVersionRe  = regexp.MustCompile(`(\d+)\.(\d+)`)
	ossJDKNameRe = regexp.MustCompile(`(?i)^jdk-(\d+)\.(tar\.gz|tgz)$`)
)

// DefaultJDKRange 按 StarRocks 版本号给出默认 JDK 范围（max=0 表示不限上限）：
// 2.x → 8–11；3.0–3.4 → 11–17；3.5+ → 17+。
func DefaultJDKRange(version string) (min, max int) {
	m := srVersionRe.FindStringSubmatch(version)
	if m == nil {
		return 11, 0
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	switch {
	case major < 3:
		return 8, 11
	case major == 3 && minor < 5:
		return 11, 17
	default:
		return 17, 0
	}
}

func parseJDKField(name, v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 8 || n > 99 {
		return 0, fmt.Errorf("%s 须为 JDK 主版本号整数（如 11、17、21），当前为 %q", name, v)
	}
	return n, nil
}

// NormalizeReleaseJDK 校验并补全版本包 JDK 范围：两者都为空时按版本号取默认值；最低不大于最高。
func NormalizeReleaseJDK(version, jdkMin, jdkMax string) (string, string, error) {
	lo, err := parseJDKField("JDK 最低", jdkMin)
	if err != nil {
		return "", "", err
	}
	hi, err := parseJDKField("JDK 最高", jdkMax)
	if err != nil {
		return "", "", err
	}
	if lo == 0 && hi == 0 {
		lo, hi = DefaultJDKRange(version)
	} else if lo == 0 {
		lo, _ = DefaultJDKRange(version)
		if lo > hi {
			lo = hi
		}
	}
	if hi > 0 && lo > hi {
		return "", "", fmt.Errorf("JDK 最低 %d 不能大于最高 %d", lo, hi)
	}
	outMax := ""
	if hi > 0 {
		outMax = strconv.Itoa(hi)
	}
	return strconv.Itoa(lo), outMax, nil
}

// releaseJDKRange 版本包登记的 JDK 范围；字段为空或非法时回退默认值。
func releaseJDKRange(rel *store.SrRelease) (min, max int) {
	if rel == nil {
		return 0, 0
	}
	lo, hi, err := NormalizeReleaseJDK(rel.Version, rel.JDKMin, rel.JDKMax)
	if err != nil {
		return DefaultJDKRange(rel.Version)
	}
	min, _ = strconv.Atoi(lo)
	if hi != "" {
		max, _ = strconv.Atoi(hi)
	}
	return min, max
}

func jdkInRange(major, min, max int) bool {
	if major <= 0 {
		return false
	}
	if min > 0 && major < min {
		return false
	}
	return max <= 0 || major <= max
}

// JDKRangeText 11–17 / 17+。
func JDKRangeText(min, max int) string {
	switch {
	case min > 0 && max > 0:
		return fmt.Sprintf("%d–%d", min, max)
	case min > 0:
		return fmt.Sprintf("%d+", min)
	case max > 0:
		return fmt.Sprintf("≤%d", max)
	}
	return "不限"
}

// ossJDK OSS 上可用于安装的 JDK 包。
type ossJDK struct {
	Key       string
	Major     int
	SHA256Key string
}

// pickOSSJDK 在 OSS 前缀目录下（不含子目录）按 jdk-<major>.tar.gz 选范围内主版本最大的包。
func pickOSSJDK(objs []OSSObject, prefix string, min, max int) *ossJDK {
	keys := map[string]bool{}
	for _, o := range objs {
		keys[o.Key] = true
	}
	var best *ossJDK
	for _, o := range objs {
		key := strings.TrimSpace(o.Key)
		if !strings.HasPrefix(key, prefix) || strings.Contains(strings.TrimPrefix(key, prefix), "/") {
			continue
		}
		m := ossJDKNameRe.FindStringSubmatch(path.Base(key))
		if m == nil {
			continue
		}
		major, _ := strconv.Atoi(m[1])
		if !jdkInRange(major, min, max) || (best != nil && major <= best.Major) {
			continue
		}
		best = &ossJDK{Key: key, Major: major}
		if keys[key+".sha256"] {
			best.SHA256Key = key + ".sha256"
		}
	}
	return best
}

const (
	jdkOK      = "ok"
	jdkInstall = "install"
	jdkFail    = "fail"
)

// jdkDecision 单节点 JDK 判定：满足 / 升级时安装（本机已有或 OSS 下载）/ 阻断。
type jdkDecision struct {
	Status   string
	Message  string
	JavaHome string  // 切换到本机已有 JDK
	Pkg      *ossJDK // 从 OSS 下载安装
}

func currentJDKText(chk *JavaCheckResp) string {
	if chk == nil || chk.Major <= 0 {
		msg := ""
		if chk != nil {
			msg = strings.TrimSpace(chk.Message)
		}
		if msg == "" {
			return "未检测到可用 java"
		}
		return "未检测到可用 java（" + msg + "）"
	}
	return fmt.Sprintf("当前 JDK %s（%s）", chk.Version, chk.Path)
}

func systemJDKText(chk *JavaCheckResp) string {
	if chk == nil || chk.SystemMajor <= 0 {
		msg := "未检测到 /usr/bin/java"
		if chk != nil && strings.TrimSpace(chk.SystemMessage) != "" {
			msg += "（" + strings.TrimSpace(chk.SystemMessage) + "）"
		}
		return msg
	}
	p := chk.SystemPath
	if p == "" {
		p = "/usr/bin/java"
	}
	return fmt.Sprintf("JDK %s（/usr/bin/java → %s）", chk.SystemVersion, p)
}

// systemJDKOK 系统默认 java 是否满足；旧 Agent 不返回该信息时视为不满足（无法确认 systemd 实际用的 java）。
func systemJDKOK(chk *JavaCheckResp, min, max int) bool {
	return chk != nil && chk.SystemChecked && jdkInRange(chk.SystemMajor, min, max)
}

// oldAgentJDKMsg Agent 未返回系统 java 信息时的阻断文案。
const oldAgentJDKMsg = "Node Agent 版本过旧，无法校验 systemd 启动使用的 /usr/bin/java，请在「Node Agent」页重新下发后重试（未停服）"

// jdkReady StarRocks 实际生效的 JDK 与系统默认 java 都满足。
func jdkReady(chk *JavaCheckResp, min, max int) bool {
	return chk != nil && chk.Major > 0 && jdkInRange(chk.Major, min, max) && systemJDKOK(chk, min, max)
}

// decideJDK label 如 be@10.0.0.1；version 为目标 StarRocks 版本；pkg 为 OSS 中范围内的包（可为 nil）。
// systemd 启动不加载登录环境，启动脚本可能由 /usr/bin/java 推断 JAVA_HOME，所以 conf 里 JAVA_HOME 满足之外，
// 系统默认 java 也必须满足，否则切 update-alternatives（优先 /home/jdk）。
func decideJDK(label, version string, chk *JavaCheckResp, min, max int, pkg *ossJDK, prefix string) jdkDecision {
	want := fmt.Sprintf("%s 要求 JDK %s", version, JDKRangeText(min, max))
	if chk == nil || !chk.SystemChecked {
		return jdkDecision{Status: jdkFail, Message: fmt.Sprintf("%s %s；%s", label, currentJDKText(chk), oldAgentJDKMsg)}
	}
	effOK := chk.Major > 0 && jdkInRange(chk.Major, min, max)
	if effOK && systemJDKOK(chk, min, max) {
		msg := fmt.Sprintf("%s JDK %s（%s）满足 %s", label, chk.Version, chk.Path, want)
		if chk.Source == "conf" {
			msg += "；系统默认 java " + systemJDKText(chk) + " 也满足"
		}
		return jdkDecision{Status: jdkOK, Message: msg}
	}
	cur := currentJDKText(chk)
	if !effOK && chk != nil && chk.Source == "conf" {
		return jdkDecision{Status: jdkFail, Message: fmt.Sprintf(
			"%s %s 不满足 %s；JAVA_HOME 由 %s 指定，update-alternatives 无法生效，请修改或删除该 JAVA_HOME 后重试",
			label, cur, want, chk.ConfFile)}
	}
	reason := fmt.Sprintf("%s %s 不满足 %s", label, cur, want)
	if effOK {
		reason = fmt.Sprintf("%s %s 满足，但系统默认 java 为 %s，systemd 启动时启动脚本会由它推断 JAVA_HOME，不满足 %s",
			label, cur, systemJDKText(chk), want)
	}
	if c := pickJavaCandidate(chk, min, max); c != nil {
		return jdkDecision{Status: jdkInstall, JavaHome: c.Home, Message: fmt.Sprintf(
			"%s；升级时将 update-alternatives 切换 java/javac 到本机已有 %s（JDK %s）", reason, c.Home, c.Version)}
	}
	if pkg != nil {
		return jdkDecision{Status: jdkInstall, Pkg: pkg, Message: fmt.Sprintf(
			"%s；升级时将从 OSS 安装 %s 到 /home/jdk-<版本> 并切换 update-alternatives",
			reason, path.Base(pkg.Key))}
	}
	return jdkDecision{Status: jdkFail, Message: fmt.Sprintf(
		"%s，且本机 /home/jdk 与 OSS %s 下都没有满足的 JDK（需 %s）", reason, prefix, ossJDKHint(prefix, min, max))}
}

// pickJavaCandidate 优先 /home/jdk（Preferred），否则取 Agent 排序后第一个满足范围的。
func pickJavaCandidate(chk *JavaCheckResp, min, max int) *JavaCandidate {
	if chk == nil {
		return nil
	}
	var first *JavaCandidate
	for i := range chk.Candidates {
		c := &chk.Candidates[i]
		if !jdkInRange(c.Major, min, max) {
			continue
		}
		if c.Preferred {
			return c
		}
		if first == nil {
			first = c
		}
	}
	return first
}

func nodeLabel(n *store.SrNode) string { return n.Component + "@" + n.Host }

func nodeSoftwarePath(cl *store.SrCluster, n *store.SrNode) string {
	if sp := strings.TrimSpace(n.SoftwarePath); sp != "" {
		return sp
	}
	return softwareBaseForRole(cl, n.Component) + "/" + strings.ToLower(n.Component)
}

// jdkNodeResult 某节点的 JDK 判定（Err 非空表示 Agent 调用失败）。
type jdkNodeResult struct {
	Node     store.SrNode
	Decision jdkDecision
	Err      error
}

// evalClusterJDK 逐节点调用 Agent java/check；有节点需安装时才列举 OSS。
func (e *Engine) evalClusterJDK(ctx context.Context, cl *store.SrCluster, nodes []store.SrNode, rel *store.SrRelease) []jdkNodeResult {
	min, max := releaseJDKRange(rel)
	out := make([]jdkNodeResult, len(nodes))
	checks := make([]*JavaCheckResp, len(nodes))
	needPkg := false
	for i := range nodes {
		n := &nodes[i]
		out[i].Node = *n
		auth, err := e.agentAuth(n.AgentID)
		if err != nil {
			out[i].Err = err
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		chk, err := e.Agent.JavaCheck(cctx, auth, JavaCheckReq{Role: n.Component, SoftwarePath: nodeSoftwarePath(cl, n), JDKMin: min, JDKMax: max})
		cancel()
		if err != nil {
			out[i].Err = err
			continue
		}
		checks[i] = chk
		if !jdkReady(chk, min, max) {
			needPkg = true
		}
	}
	var pkg *ossJDK
	prefix := ""
	if needPkg {
		pkg, prefix = e.findOSSJDK(ctx, min, max)
	}
	for i := range out {
		if out[i].Err == nil {
			out[i].Decision = decideJDK(nodeLabel(&out[i].Node), rel.Version, checks[i], min, max, pkg, prefix)
		}
	}
	return out
}

func (e *Engine) findOSSJDK(ctx context.Context, min, max int) (*ossJDK, string) {
	s, err := e.loadOSSPlain()
	if err != nil {
		return nil, ""
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	objs, err := listObjectsV2(cctx, s, s.Prefix)
	if err != nil {
		return nil, s.Prefix
	}
	return pickOSSJDK(objs, s.Prefix, min, max), s.Prefix
}

// ossJDKSHA256 读取 jdk-xx.tar.gz.sha256 旁路文件（可选）。
func (e *Engine) ossJDKSHA256(ctx context.Context, pkg *ossJDK) string {
	if pkg == nil || pkg.SHA256Key == "" {
		return ""
	}
	u, err := e.PresignGET(ctx, pkg.SHA256Key, 10*time.Minute)
	if err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if m := sha256HexRe.FindStringSubmatch(string(b)); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// stepJDKPrepare 升级停服前：对不满足目标版本 JDK 要求的节点安装 / 切换 JDK 并复验。
func (e *Engine) stepJDKPrepare(ctx context.Context, cl *store.SrCluster, nodes []store.SrNode, step *store.SrChangeStep) error {
	rid := strings.TrimSpace(cl.ReleaseID)
	rel, err := e.Store.GetSrRelease(rid)
	if err != nil || rel == nil {
		return fmt.Errorf("目标版本不存在（release_id=%s）", rid)
	}
	min, max := releaseJDKRange(rel)
	results := e.evalClusterJDK(ctx, cl, nodes, rel)
	var logs []string
	var fails []string
	installed := map[string]bool{}
	var recheck []store.SrNode
	for _, r := range results {
		switch {
		case r.Err != nil:
			fails = append(fails, fmt.Sprintf("%s: %v", nodeLabel(&r.Node), r.Err))
		case r.Decision.Status == jdkFail:
			fails = append(fails, r.Decision.Message)
		case r.Decision.Status == jdkOK:
			logs = append(logs, r.Decision.Message)
		default:
			recheck = append(recheck, r.Node)
			aid := strings.TrimSpace(r.Node.AgentID)
			if installed[aid] {
				continue
			}
			installed[aid] = true
			msg, err := e.installNodeJDK(ctx, r.Node.AgentID, r.Decision)
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s 安装 JDK 失败: %v", nodeLabel(&r.Node), err))
				continue
			}
			logs = append(logs, fmt.Sprintf("%s %s", r.Node.Host, msg))
		}
	}
	if len(fails) == 0 && !e.Cfg.DryRun {
		for i := range recheck {
			n := &recheck[i]
			auth, err := e.agentAuth(n.AgentID)
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s: %v", nodeLabel(n), err))
				continue
			}
			chk, err := e.Agent.JavaCheck(ctx, auth, JavaCheckReq{Role: n.Component, SoftwarePath: nodeSoftwarePath(cl, n), JDKMin: min, JDKMax: max})
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s 复验: %v", nodeLabel(n), err))
				continue
			}
			if !jdkReady(chk, min, max) {
				fails = append(fails, fmt.Sprintf("%s 安装后复验仍不满足：%s；系统默认 java %s", nodeLabel(n), currentJDKText(chk), systemJDKText(chk)))
				continue
			}
			line := fmt.Sprintf("%s 复验通过 JDK %s（%s）", nodeLabel(n), chk.Version, chk.Path)
			if chk.SystemChecked {
				line += "；系统默认 java " + systemJDKText(chk)
			}
			logs = append(logs, line)
		}
	}
	step.LogText = strings.Join(logs, "\n")
	if len(fails) > 0 {
		return fmt.Errorf("JDK 未就绪，已停止升级（未停服）：%s", strings.Join(fails, "；"))
	}
	return nil
}

func (e *Engine) installNodeJDK(ctx context.Context, agentID string, d jdkDecision) (string, error) {
	auth, err := e.agentAuth(agentID)
	if err != nil {
		return "", err
	}
	req := JavaInstallReq{JavaHome: d.JavaHome, Priority: JDKInstallPriority, DryRun: e.Cfg.DryRun}
	if req.JavaHome == "" {
		if d.Pkg == nil {
			return "", fmt.Errorf("没有可用的 JDK 包")
		}
		if e.Cfg.DryRun {
			req.DownloadURL = "dry-run://" + d.Pkg.Key
		} else {
			u, err := e.PresignGET(ctx, d.Pkg.Key, 2*time.Hour)
			if err != nil {
				return "", err
			}
			req.DownloadURL = u
			req.SHA256 = e.ossJDKSHA256(ctx, d.Pkg)
		}
	}
	resp, err := e.Agent.JavaInstall(ctx, auth, req)
	if err != nil {
		return "", err
	}
	if resp != nil && strings.TrimSpace(resp.Message) != "" {
		return strings.TrimSpace(resp.Message), nil
	}
	return "JDK 已安装", nil
}

// ossJDKHint 缺包时的提示文件名。
func ossJDKHint(prefix string, min, max int) string {
	for _, m := range []int{21, 17, 11, 8} {
		if jdkInRange(m, min, max) {
			return fmt.Sprintf("%sjdk-%d.tar.gz", prefix, m)
		}
	}
	return prefix + "jdk-<主版本>.tar.gz"
}
