export function dbLabel(d) {
  const name = d?.db_name || d?.name || ''
  const domains = (d?.domains || []).map((t) => String(t).trim()).filter(Boolean)
  const tags = (d?.tags || []).map((t) => String(t).trim()).filter(Boolean)
  let label = name
  if (domains.length) label += ` @ ${domains.join(',')}`
  if (tags.length) label += ` [${tags.join(',')}]`
  return label
}
