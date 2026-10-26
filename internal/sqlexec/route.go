package sqlexec

import "strings"

const (
	ModeLocal   = "local"
	ModeRemote  = "remote"
	ModeMissing = "missing"
)

// Executor 是控制面用来分流的执行器。一台可绑定多个环境 code。
type Executor struct {
	ID       string
	Name     string
	BaseURL  string
	Username string
	Password string
	EnvCodes []string
	Enabled  bool
}

// Decision 说明某个实例环境该怎么跑。
type Decision struct {
	Mode     string
	Executor *Executor
}

// Decide 按实例 idc（环境 code）分流。
// 本服务所在环境直连。已绑定执行器的环境走执行器。
// 本服务环境已配置、但该环境没有执行器时记为 missing，不从控制面直连。
// 尚未配置本服务环境时，未绑定执行器的实例仍直连，避免还没做初始化时把现有工单全部打失败。
func Decide(idc string, localCodes []string, executors []Executor) Decision {
	idc = strings.TrimSpace(idc)
	var hit *Executor
	for i := range executors {
		e := &executors[i]
		if !e.Enabled {
			continue
		}
		for _, code := range e.EnvCodes {
			if idc != "" && strings.EqualFold(strings.TrimSpace(code), idc) {
				cp := *e
				hit = &cp
				break
			}
		}
		if hit != nil {
			break
		}
	}
	localSet := false
	for _, code := range localCodes {
		if strings.TrimSpace(code) != "" {
			localSet = true
			break
		}
	}
	if localSet && idc != "" {
		for _, code := range localCodes {
			if strings.EqualFold(strings.TrimSpace(code), idc) {
				return Decision{Mode: ModeLocal}
			}
		}
	}
	if hit != nil {
		return Decision{Mode: ModeRemote, Executor: hit}
	}
	if !localSet {
		return Decision{Mode: ModeLocal}
	}
	return Decision{Mode: ModeMissing}
}
