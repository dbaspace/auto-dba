-- 平台资源申请：菜单写库 + 内置角色授权（幂等）
-- 用法：在业务元库执行本脚本后，前端刷新或重新登录即可侧栏可见。

INSERT INTO tbl_menus (
  id, parent_id, title, urn, path, permission_desc, icon, sort_order, enabled, created_at, updated_at
) VALUES (
  'm-tickets-resource-apply',
  'm-tickets',
  '平台资源申请',
  'um.sidebar.ops-resource-apply',
  '/ops/resource-apply',
  '查看平台资源申请',
  'appstore',
  18,
  1,
  to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
  to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
)
ON CONFLICT (id) DO UPDATE SET
  parent_id = EXCLUDED.parent_id,
  title = EXCLUDED.title,
  urn = EXCLUDED.urn,
  path = EXCLUDED.path,
  permission_desc = EXCLUDED.permission_desc,
  icon = EXCLUDED.icon,
  sort_order = EXCLUDED.sort_order,
  enabled = 1,
  updated_at = EXCLUDED.updated_at;

-- 内置角色：admin / operator / viewer / developer / dba
INSERT INTO tbl_role_menus (role_id, menu_id)
SELECT r.id, 'm-tickets-resource-apply'
FROM tbl_roles r
WHERE lower(r.code) IN ('admin', 'operator', 'viewer', 'developer', 'dba', 'platform_admin', 'super_admin')
ON CONFLICT (role_id, menu_id) DO NOTHING;

-- 已拥有「工单申请」父菜单的其它角色（含自定义角色）一并授权
INSERT INTO tbl_role_menus (role_id, menu_id)
SELECT rm.role_id, 'm-tickets-resource-apply'
FROM tbl_role_menus rm
WHERE rm.menu_id = 'm-tickets'
  AND NOT EXISTS (
    SELECT 1 FROM tbl_role_menus x
    WHERE x.role_id = rm.role_id AND x.menu_id = 'm-tickets-resource-apply'
  )
ON CONFLICT (role_id, menu_id) DO NOTHING;

-- 校验
SELECT id, parent_id, title, path, urn, enabled, sort_order
FROM tbl_menus
WHERE id = 'm-tickets-resource-apply';

SELECT r.code, r.name
FROM tbl_role_menus rm
JOIN tbl_roles r ON r.id = rm.role_id
WHERE rm.menu_id = 'm-tickets-resource-apply'
ORDER BY r.code;
