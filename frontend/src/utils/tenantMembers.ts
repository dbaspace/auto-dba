/** 只保留成员名单中的用户名；返回 kept 与被丢掉的名字。 */
export function filterNamesByMembers(
  names: string[] | undefined,
  members: string[],
): { kept: string[]; dropped: string[] } {
  const allow = new Set(members.map((s) => String(s).trim().toLowerCase()).filter(Boolean))
  const kept: string[] = []
  const dropped: string[] = []
  const seen = new Set<string>()
  for (const raw of names || []) {
    const name = String(raw || '').trim()
    if (!name) continue
    const key = name.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    if (allow.has(key)) kept.push(name)
    else dropped.push(name)
  }
  return { kept, dropped }
}
