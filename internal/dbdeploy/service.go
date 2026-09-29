package dbdeploy

import (
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/srcluster"
	"auto-dba/internal/store"
)

// Service 控制面编排（版本/OSS 签发等）。
type Service struct {
	Store  *store.DB
	Secret string
}

func NewService(st *store.DB, secretKey string) *Service {
	return &Service{Store: st, Secret: secretKey}
}

// PresignRelease 按引擎 OSS 绑定签发 Release 对象短时下载 URL。
func (s *Service) PresignRelease(engine, releaseID string, expire time.Duration) (urlStr, objectKey, checksum string, err error) {
	rel, err := s.Store.GetDbDeployRelease(releaseID)
	if err != nil {
		return "", "", "", err
	}
	if rel == nil {
		return "", "", "", fmt.Errorf("版本不存在")
	}
	eng := store.DbDeployEngineMySQL
	if engine != "" {
		eng = engine
	} else {
		eng = rel.Engine
	}
	if rel.Engine != eng && engine != "" {
		return "", "", "", fmt.Errorf("版本引擎不匹配")
	}
	bind, err := s.Store.GetDbDeployOSSBind(rel.Engine)
	if err != nil {
		return "", "", "", err
	}
	if strings.TrimSpace(bind.OSSSettingID) == "" {
		return "", "", "", fmt.Errorf("请先在工作台设置中选择配置中心 OSS，并填写制品 prefix")
	}
	plain, err := s.Store.GetOSSSettingPlain(bind.OSSSettingID)
	if err != nil || plain == nil {
		return "", "", "", fmt.Errorf("配置中心 OSS 不存在")
	}
	sk, err := secret.Decrypt(s.Secret, plain.SecretEnc)
	if err != nil {
		return "", "", "", err
	}
	key := strings.TrimSpace(rel.ObjectKey)
	if key == "" {
		return "", "", "", fmt.Errorf("object_key 为空")
	}
	if expire <= 0 {
		expire = 2 * time.Hour
	}
	urlStr, err = srcluster.PresignGETURL(plain.Endpoint, plain.Bucket, key, plain.AccessKey, sk, expire)
	if err != nil {
		return "", "", "", err
	}
	return urlStr, key, rel.Checksum, nil
}

// Meta 前端元数据（EngineRegistry 驱动）。
func Meta(engine string) map[string]any {
	engine = strings.ToLower(strings.TrimSpace(engine))
	out := map[string]any{
		"engines":      engineIDsForMeta(),
		"deploy_kinds": []map[string]string{{"id": "self", "label": "自建"}, {"id": "cloud", "label": "云托管"}},
		"notes":        "自建装机：OSS + Db Agent 变更单；仅登记/云入口不装机。Hub 以 /db-deploy/engines 为准。",
		"registry":     ListEnginesForAPI(),
	}
	if engine == "" {
		topos := map[string][]map[string]string{}
		haModes := map[string][]string{}
		defaults := map[string]any{}
		for _, e := range AllEngines() {
			if e.Workbench != WorkbenchDbDeploy {
				continue
			}
			topos[e.ID] = topoMaps(e.Topologies)
			haModes[e.ID] = e.HAModes
			defaults[e.ID] = e.Defaults
		}
		if redis := GetEngine("codis"); redis != nil {
			topos["codis"] = topoMaps(redis.Topologies)
			haModes["codis"] = redis.HAModes
			defaults["codis"] = redis.Defaults
		}
		out["topologies"] = topos
		out["ha_modes"] = haModes
		out["defaults"] = defaults
		return out
	}

	e := GetEngine(engine)
	if e == nil {
		out["engine"] = engine
		out["error"] = "未知引擎"
		return out
	}
	out["engine"] = e.ID
	out["label"] = e.Label
	out["capability"] = e.Capability
	out["capability_label"] = capabilityLabel(e.Capability)
	out["allow_deploy"] = e.AllowDeploy
	out["allow_cloud"] = e.AllowCloud
	out["workbench"] = e.Workbench
	out["desc"] = e.Desc
	out["roles"] = e.Roles
	out["topologies"] = topoMaps(e.Topologies)
	out["ha_modes"] = e.HAModes
	out["defaults"] = e.Defaults
	if ag := agentForWorkbench(e.Workbench); ag.Kind != "" {
		out["agent"] = ag
		out["agent_kind"] = ag.Kind
		out["agent_label"] = ag.Label
		out["agent_port"] = ag.Port
	}
	if e.ID == "redis" || e.ID == "codis" {
		out["codis_roles"] = []string{"codis_dashboard", "codis_proxy", "codis_server"}
	}
	presets := []map[string]string{{"id": "all", "label": "全部"}}
	presets = append(presets, topoMaps(e.Topologies)...)
	out["filter_presets"] = presets
	if e.Capability == CapabilityRegister {
		out["notes"] = e.Desc + " 装机未开放：请用「登记已有」或云单入口。"
	} else if e.Desc != "" {
		out["notes"] = e.Desc
	}
	return out
}

func topoMaps(list []TopoSpec) []map[string]string {
	out := make([]map[string]string, 0, len(list))
	for _, t := range list {
		out = append(out, map[string]string{"id": t.ID, "label": t.Label, "kind": t.Kind})
	}
	return out
}

func engineIDsForMeta() []string {
	ids := []string{}
	for _, e := range AllEngines() {
		if e.Workbench == WorkbenchDbDeploy {
			ids = append(ids, e.ID)
		}
	}
	ids = append(ids, "codis")
	return ids
}
