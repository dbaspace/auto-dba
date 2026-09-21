export interface EnvOption {
  value: string
  label: string
}

export function buildEnvOptions(envRows: any[] = [], instances: any[] = []): EnvOption[] {
  const codes = new Map<string, string>()
  const byId = new Map<string, any>()
  for (const e of envRows || []) {
    if (e?.id) byId.set(e.id, e)
    const code = e?.code || ''
    if (code) codes.set(code, e.name || code)
  }
  for (const i of instances || []) {
    if (i?.idc && !codes.has(i.idc)) codes.set(i.idc, i.idc)
    const env = byId.get(i?.env_id)
    if (env?.code && !codes.has(env.code)) codes.set(env.code, env.name || env.code)
  }
  if (!codes.size) {
    codes.set('test', '测试')
    codes.set('prod', '生产')
  }
  return [...codes.entries()].map(([value, label]) => ({ value, label }))
}
