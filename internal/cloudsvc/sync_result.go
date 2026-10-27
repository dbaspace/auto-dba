package cloudsvc

import "fmt"

// finalizeSyncResult：有真实失败且没有任何成功时才返回 error；
// 仅缺密钥跳过视为成功（定时任务不报警，前端用 skipped 提示）。
func finalizeSyncResult(res *SyncResult) error {
	if res == nil {
		return nil
	}
	if res.Synced == 0 && len(res.Errors) > 0 {
		return fmt.Errorf("%s", res.Errors[0])
	}
	return nil
}
