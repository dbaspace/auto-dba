#!/usr/bin/env bash
# 租户隔离 10 项回归（依赖已启动的 :8800 与用户 abc/租户 abc）
set -u
BASE=${BASE:-http://127.0.0.1:8800/api/v1}
ABC_TID=24828cbc-1899-4a21-8cf6-762b788a11bf
PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); echo "PASS $1"; }
ko() { FAIL=$((FAIL + 1)); echo "FAIL $1 :: ${2:-}"; }

tok() { python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])'; }
id_of() { python3 -c 'import sys,json; print((json.load(sys.stdin).get("data") or {}).get("id",""))'; }

ALOGIN=$(curl -sS -X POST "$BASE/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}')
AT=$(echo "$ALOGIN" | tok)
CT=$(echo "$ALOGIN" | python3 -c 'import sys,json; print(json.load(sys.stdin)["user"]["current_tenant"]["id"])')
IMP=$(curl -sS -X POST "$BASE/tenants/tenant-default/impersonate" -H "Authorization: Bearer $AT" -H "X-Tenant-Id: $CT")
DT=$(echo "$IMP" | tok)
DAUTH="Authorization: Bearer $DT"
DXT="X-Tenant-Id: tenant-default"

FOREIGN_INST=43555cf3-3792-49f7-8fec-13d0593b77d3
FOREIGN_DB=$(curl -sS "$BASE/databases?instance_id=$FOREIGN_INST" -H "$DAUTH" -H "$DXT" | python3 -c 'import sys,json
for d in json.load(sys.stdin).get("data") or []:
  print(d["id"]); break')
FOREIGN_ISSUE=$(curl -sS "$BASE/tickets?current=1&page_size=20" -H "$DAUTH" -H "$DXT" | python3 -c '
import sys,json
d=json.load(sys.stdin); items=d.get("data")
if isinstance(items, dict): items=items.get("list") or items.get("items") or []
if not isinstance(items, list): items=[]
for a in items:
  if a.get("id"):
    print(a["id"]); break')

RULE=$(curl -sS -X POST "$BASE/alert-rules" -H "$DAUTH" -H "$DXT" -H 'Content-Type: application/json' -d "{
  \"name\":\"iso-rule-$(date +%s)\",
  \"metric_expr\":\"up\",
  \"severity\":\"warning\",
  \"for_duration\":\"1m\",
  \"enabled\":true,
  \"tiers\":[{\"level\":\"warning\",\"op\":\"==\",\"threshold\":1}],
  \"scope\":{\"mode\":\"global\"}
}")
FOREIGN_RULE=$(echo "$RULE" | id_of)
if [[ -z "$FOREIGN_RULE" ]]; then
  FOREIGN_RULE=$(curl -sS "$BASE/alert-rules" -H "$DAUTH" -H "$DXT" | python3 -c 'import sys,json
for r in json.load(sys.stdin).get("data") or []:
  print(r["id"]); break' 2>/dev/null || true)
fi
REL=$(curl -sS -X POST "$BASE/cmdb/relations" -H "$DAUTH" -H "$DXT" -H 'Content-Type: application/json' \
  -d "{\"src_type\":\"instance\",\"src_id\":\"$FOREIGN_INST\",\"rel_type\":\"related\",\"dst_type\":\"database\",\"dst_id\":\"$FOREIGN_DB\",\"remark\":\"iso-final\"}")
RID=$(echo "$REL" | id_of)

ACC=$(curl -sS -X POST "$BASE/accounts" -H "$DAUTH" -H "$DXT" -H 'Content-Type: application/json' -d "{
  \"instance_id\":\"$FOREIGN_INST\",\"username\":\"iso_u_$(date +%s)\",\"password\":\"IsoTest123!\",\"db_type\":\"postgres\"
}")
FOREIGN_ACC=$(echo "$ACC" | id_of)

FOREIGN_RUN=$(curl -sS "$BASE/backup/runs?limit=20" -H "$DAUTH" -H "$DXT" | python3 -c 'import sys,json
for a in json.load(sys.stdin).get("data") or []: print(a["id"]); break' 2>/dev/null || true)
FOREIGN_HA=$(curl -sS "$BASE/ha/clusters" -H "$DAUTH" -H "$DXT" | python3 -c 'import sys,json
for a in json.load(sys.stdin).get("data") or []: print(a["id"]); break' 2>/dev/null || true)
FOREIGN_PROXY=$(curl -sS "$BASE/proxy/services" -H "$DAUTH" -H "$DXT" | python3 -c 'import sys,json
for a in json.load(sys.stdin).get("data") or []: print(a["id"]); break' 2>/dev/null || true)

echo "seed db=$FOREIGN_DB issue=$FOREIGN_ISSUE rule=$FOREIGN_RULE acc=$FOREIGN_ACC run=$FOREIGN_RUN ha=$FOREIGN_HA proxy=$FOREIGN_PROXY rid=$RID"

BT=$(curl -sS -X POST "$BASE/auth/login" -H 'Content-Type: application/json' -d '{"username":"abc","password":"abc123"}' | tok)
BAUTH="Authorization: Bearer $BT"
BXT="X-Tenant-Id: $ABC_TID"

N=$(curl -sS "$BASE/instances" -H "$BAUTH" -H "$BXT" | python3 -c "import sys,json
items=json.load(sys.stdin).get('data') or []
bad=sum(1 for i in items if i.get('tenant_id') and i.get('tenant_id')!='$ABC_TID')
print(f'{len(items)} {bad}')")
read -r CNT BAD <<<"$N"
[[ "${CNT:-0}" -ge 1 && "${BAD:-1}" -eq 0 ]] && ok T1_list_instances || ko T1_list_instances "$N"

C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' "$BASE/instances/$FOREIGN_INST" -H "$BAUTH" -H "$BXT")
[[ "$C" == "404" ]] && ok T2_get_foreign_inst || ko T2_get_foreign_inst "$C"

C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' "$BASE/databases/$FOREIGN_DB" -H "$BAUTH" -H "$BXT")
[[ "$C" == "404" ]] && ok T3_get_foreign_db || ko T3_get_foreign_db "$C"

C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' "$BASE/tickets/$FOREIGN_ISSUE" -H "$BAUTH" -H "$BXT")
[[ "$C" == "404" ]] && ok T4_get_foreign_ticket || ko T4_get_foreign_ticket "$C $(head -c 80 /tmp/iso.json)"

RULE_PATH=alert-rules
if [[ -n "${FOREIGN_RULE:-}" ]]; then
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' -X DELETE "$BASE/$RULE_PATH/$FOREIGN_RULE" -H "$BAUTH" -H "$BXT")
  ST=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/$RULE_PATH/$FOREIGN_RULE" -H "$DAUTH" -H "$DXT")
  [[ "$C" == "404" && "$ST" == "200" ]] && ok T5_del_foreign_rule || ko T5_del_foreign_rule "del=$C still=$ST $(head -c 80 /tmp/iso.json)"
else
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' "$BASE/cmdb/cis/instance/$FOREIGN_INST" -H "$BAUTH" -H "$BXT")
  [[ "$C" == "404" ]] && ok T5_get_foreign_ci_404 || ko T5_get_foreign_ci_404 "$C $(head -c 80 /tmp/iso.json)"
fi

if [[ -n "${FOREIGN_ACC:-}" ]]; then
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' -X DELETE "$BASE/accounts/$FOREIGN_ACC" -H "$BAUTH" -H "$BXT")
  ST=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/accounts/$FOREIGN_ACC" -H "$DAUTH" -H "$DXT")
  [[ "$C" == "404" && "$ST" == "200" ]] && ok T6_del_foreign_account || ko T6_del_foreign_account "del=$C still=$ST $(head -c 80 /tmp/iso.json)"
else
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' -X DELETE "$BASE/databases/$FOREIGN_DB" -H "$BAUTH" -H "$BXT")
  STILL=$(curl -sS "$BASE/databases?instance_id=$FOREIGN_INST" -H "$DAUTH" -H "$DXT" | python3 -c "import sys,json; print(sum(1 for d in json.load(sys.stdin).get('data') or [] if d.get('id')=='$FOREIGN_DB'))")
  if [[ "$C" == "404" && "$STILL" == "1" ]]; then ok T6_del_foreign_db
  elif [[ "$C" == "403" && "$STILL" == "1" ]]; then ok T6_del_foreign_db_blocked_exists
  else ko T6_del_foreign_db "del=$C still=$STILL $(head -c 80 /tmp/iso.json)"; fi
fi

CNT=$(curl -sS "$BASE/cmdb/relations" -H "$BAUTH" -H "$BXT" | python3 -c "import sys,json; print(sum(1 for r in json.load(sys.stdin).get('data') or [] if r.get('id')=='$RID'))")
[[ "$CNT" == "0" ]] && ok T7_cmdb_list_isolated || ko T7_cmdb_list_isolated "cnt=$CNT rid=$RID"

C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' -X DELETE "$BASE/cmdb/relations/$RID" -H "$BAUTH" -H "$BXT")
ST=$(curl -sS "$BASE/cmdb/relations" -H "$DAUTH" -H "$DXT" | python3 -c "import sys,json; print(sum(1 for r in json.load(sys.stdin).get('data') or [] if r.get('id')=='$RID'))")
[[ "$C" == "404" && "$ST" == "1" ]] && ok T8_cmdb_del_blocked || ko T8_cmdb_del_blocked "del=$C still=$ST $(cat /tmp/iso.json)"

OWN=$(curl -sS "$BASE/databases" -H "$BAUTH" -H "$BXT" | python3 -c 'import sys,json
for d in json.load(sys.stdin).get("data") or []:
  if d.get("db_name")=="db_autosql":
    print(d["id"]); break')
Q=$(curl -sS -X POST "$BASE/query" -H "$BAUTH" -H "$BXT" -H 'Content-Type: application/json' \
  -d "{\"database_id\":\"$OWN\",\"sql\":\"SELECT 1 AS ok\",\"limit\":1}")
echo "$Q" | python3 -c 'import sys,json;d=json.load(sys.stdin);raise SystemExit(0 if (d.get("data") or {}).get("rows")==[[1]] else 1)' \
  && ok T9_query_own || ko T9_query_own "$(echo "$Q" | head -c 160)"

if [[ -n "${FOREIGN_RUN:-}" ]]; then
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' "$BASE/backup/runs/$FOREIGN_RUN" -H "$BAUTH" -H "$BXT")
  [[ "$C" == "404" ]] && ok T10_backup_run_404 || ko T10_backup_run_404 "$C"
elif [[ -n "${FOREIGN_HA:-}" ]]; then
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' -X DELETE "$BASE/ha/clusters/$FOREIGN_HA" -H "$BAUTH" -H "$BXT")
  ST=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/ha/clusters/$FOREIGN_HA" -H "$DAUTH" -H "$DXT")
  [[ "$C" == "404" && "$ST" == "200" ]] && ok T10_ha_del_404 || ko T10_ha_del_404 "del=$C still=$ST"
elif [[ -n "${FOREIGN_PROXY:-}" ]]; then
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' -X DELETE "$BASE/proxy/services/$FOREIGN_PROXY" -H "$BAUTH" -H "$BXT")
  ST=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/proxy/services/$FOREIGN_PROXY" -H "$DAUTH" -H "$DXT")
  [[ "$C" == "404" && "$ST" == "200" ]] && ok T10_proxy_del_404 || ko T10_proxy_del_404 "del=$C still=$ST"
elif [[ -n "${FOREIGN_RULE:-}" ]]; then
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' "$BASE/$RULE_PATH/$FOREIGN_RULE" -H "$BAUTH" -H "$BXT")
  [[ "$C" == "404" ]] && ok T10_get_foreign_rule_404 || ko T10_get_foreign_rule_404 "$C"
else
  C=$(curl -sS -o /tmp/iso.json -w '%{http_code}' "$BASE/cmdb/cis/instance/$FOREIGN_INST" -H "$BAUTH" -H "$BXT")
  [[ "$C" == "404" ]] && ok T10_get_foreign_ci_404 || ko T10_get_foreign_ci_404 "$C"
fi

first_id() { python3 -c 'import sys,json
d=json.load(sys.stdin).get("data") or []
if isinstance(d, dict): d=d.get("list") or d.get("items") or []
for a in d:
  if a.get("id"): print(a["id"]); break' 2>/dev/null || true; }
ids_of() { python3 -c 'import sys,json
d=json.load(sys.stdin).get("data") or []
if isinstance(d, dict): d=d.get("list") or d.get("items") or []
print(" ".join(a.get("id","") for a in d))' 2>/dev/null || true; }
# 第二批：路由守卫 / 列表隔离
F_SR=$(curl -sS "$BASE/sr-cluster/clusters" -H "$DAUTH" -H "$DXT" | first_id)
F_DD=$(curl -sS "$BASE/db-deploy/clusters" -H "$DAUTH" -H "$DXT" | first_id)
F_POL=$(curl -sS "$BASE/backup/policies" -H "$DAUTH" -H "$DXT" | first_id)
echo "seed2 sr=$F_SR dd=$F_DD policy=$F_POL"

if [[ -n "$F_SR" ]]; then
  C=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/sr-cluster/clusters/$F_SR/configs" -H "$BAUTH" -H "$BXT")
  O=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/sr-cluster/clusters/$F_SR/configs" -H "$DAUTH" -H "$DXT")
  [[ "$C" == "404" && "$O" == "200" ]] && ok T11_sr_configs_guard || ko T11_sr_configs_guard "abc=$C owner=$O"
else
  C=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/sr-cluster/clusters/nonexist/init" -H "$BAUTH" -H "$BXT")
  [[ "$C" == "404" ]] && ok T11_sr_init_guard || ko T11_sr_init_guard "$C"
fi

if [[ -n "$F_DD" ]]; then
  C=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/db-deploy/clusters/$F_DD/deploy" -H "$BAUTH" -H "$BXT" -H 'Content-Type: application/json' -d '{}')
  O=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/db-deploy/clusters/$F_DD/nodes" -H "$DAUTH" -H "$DXT")
  [[ "$C" == "404" && "$O" == "200" ]] && ok T12_dd_deploy_guard || ko T12_dd_deploy_guard "abc=$C owner=$O"
else
  C=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/db-deploy/clusters/nonexist/deploy" -H "$BAUTH" -H "$BXT" -H 'Content-Type: application/json' -d '{}')
  [[ "$C" == "404" ]] && ok T12_dd_deploy_guard || ko T12_dd_deploy_guard "$C"
fi

DSR=$(curl -sS "$BASE/sr-cluster/agents" -H "$DAUTH" -H "$DXT" | ids_of)
BSR=$(curl -sS "$BASE/sr-cluster/agents" -H "$BAUTH" -H "$BXT" | ids_of)
LEAK=0; for x in $BSR; do [[ " $DSR " == *" $x "* ]] && LEAK=1; done
[[ "$LEAK" == "0" ]] && ok T13_sr_agents_isolated || ko T13_sr_agents_isolated "abc=$BSR"

DHC=$(curl -sS "$BASE/ha/controllers" -H "$DAUTH" -H "$DXT" | ids_of)
BHC=$(curl -sS "$BASE/ha/controllers" -H "$BAUTH" -H "$BXT" | ids_of)
LEAK=0; for x in $BHC; do [[ " $DHC " == *" $x "* ]] && LEAK=1; done
[[ "$LEAK" == "0" ]] && ok T14_ha_controllers_isolated || ko T14_ha_controllers_isolated "abc=$BHC"

if [[ -n "$F_POL" ]]; then
  C=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/backup/policies/$F_POL/run" -H "$BAUTH" -H "$BXT")
  [[ "$C" == "404" ]] && ok T15_backup_policy_run_404 || ko T15_backup_policy_run_404 "$C"
else
  C=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE "$BASE/backup/policies/nonexist" -H "$BAUTH" -H "$BXT")
  [[ "$C" == "404" ]] && ok T15_backup_policy_del_404 || ko T15_backup_policy_del_404 "$C"
fi

KO=$(curl -sS -X POST "$BASE/kafka/orders" -H "$DAUTH" -H "$DXT" -H 'Content-Type: application/json' -d '{"cluster":"iso","topic":"iso_t","action":"create","description":"iso"}' | id_of)
KC=$(curl -sS "$BASE/kafka/orders" -H "$BAUTH" -H "$BXT" | python3 -c "import sys,json; print(sum(1 for r in json.load(sys.stdin).get('data') or [] if r.get('id')=='$KO'))")
KD=$(curl -sS "$BASE/kafka/orders" -H "$DAUTH" -H "$DXT" | python3 -c "import sys,json; print(sum(1 for r in json.load(sys.stdin).get('data') or [] if r.get('id')=='$KO'))")
[[ -n "$KO" && "$KC" == "0" && "$KD" == "1" ]] && ok T16_kafka_orders_isolated || ko T16_kafka_orders_isolated "id=$KO abc=$KC owner=$KD"

# 第三批：P1 旁路
# 对照用「非审计」admin + 默认租户，避免审计全平台列表把 abc 种子 ID 算进 DTAG 造成误报。
DTAG=$(curl -sS "$BASE/cmdb/tags" -H "Authorization: Bearer $AT" -H "$DXT" | ids_of)
BTAGJ=$(curl -sS "$BASE/cmdb/tags" -H "$BAUTH" -H "$BXT")
BTAG=$(echo "$BTAGJ" | ids_of)
BTAGN=$(echo "$BTAGJ" | python3 -c 'import sys,json; print(len(json.load(sys.stdin).get("data") or []))')
LEAK=0; for x in $BTAG; do [[ " $DTAG " == *" $x "* ]] && LEAK=1; done
# abc 不得看到默认租户标签定义 ID，且本租户应有自动种子（n>=1）。
[[ "$LEAK" == "0" && "${BTAGN:-0}" -ge 1 ]] && ok T17_cmdb_tagdefs_per_tenant || ko T17_cmdb_tagdefs_per_tenant "leak=$LEAK n=$BTAGN"

C=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/cmdb/cis/instance/$FOREIGN_INST/tags" -H "$BAUTH" -H "$BXT")
O=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/cmdb/cis/instance/$FOREIGN_INST/tags" -H "$DAUTH" -H "$DXT")
[[ "$C" == "404" && "$O" == "200" ]] && ok T18_ci_tags_guard || ko T18_ci_tags_guard "abc=$C owner=$O"

F_AL=$(curl -sS "$BASE/alerts?tab=all" -H "$DAUTH" -H "$DXT" | first_id)
C=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/alerts/${F_AL:-nonexist}/events" -H "$BAUTH" -H "$BXT")
[[ "$C" == "404" ]] && ok T19_alert_events_guard || ko T19_alert_events_guard "alert=$F_AL code=$C"

R=$(curl -sS -X POST "$BASE/cdc-sync/tasks" -H "$BAUTH" -H "$BXT" -H 'Content-Type: application/json' \
  -d "{\"name\":\"iso-cdc\",\"task_type\":\"init\",\"source_instance_id\":\"$FOREIGN_INST\",\"source_db\":\"postgres\"}")
echo "$R" | grep -q '源实例不存在' && ok T20_cdc_create_foreign_src || ko T20_cdc_create_foreign_src "$(echo "$R" | head -c 160)"

# 集群部署：List/Get 带回 tenant_id；跨租 404
ISO_DD=$(curl -sS -X POST "$BASE/db-deploy/clusters" -H "$DAUTH" -H "$DXT" -H 'Content-Type: application/json' \
  -d "{\"engine\":\"mysql\",\"name\":\"iso-dd-tid-$RANDOM\",\"environment\":\"test\",\"topology\":\"standalone\",\"deploy_kind\":\"self\",\"status\":\"draft\"}")
ISO_DD_ID=$(echo "$ISO_DD" | python3 -c 'import sys,json; print((json.load(sys.stdin).get("data") or {}).get("id") or "")' 2>/dev/null || true)
ISO_GET_TID=$(curl -sS "$BASE/db-deploy/clusters/${ISO_DD_ID:-none}" -H "$DAUTH" -H "$DXT" | python3 -c 'import sys,json; print((json.load(sys.stdin).get("data") or {}).get("tenant_id") or "")' 2>/dev/null || true)
ISO_LIST_TID=$(ISO_DD_ID="$ISO_DD_ID" curl -sS "$BASE/db-deploy/clusters" -H "$DAUTH" -H "$DXT" | python3 -c '
import sys,json,os
want=os.environ.get("ISO_DD_ID","")
for c in json.load(sys.stdin).get("data") or []:
  if c.get("id")==want:
    print(c.get("tenant_id") or ""); break
')
ISO_X=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/db-deploy/clusters/${ISO_DD_ID:-none}" -H "$BAUTH" -H "$BXT")
[[ -n "$ISO_DD_ID" && -n "$ISO_GET_TID" && "$ISO_GET_TID" == "$ISO_LIST_TID" && "$ISO_X" == "404" ]] \
  && ok T21_dd_cluster_tenant_id || ko T21_dd_cluster_tenant_id "id=$ISO_DD_ID get=$ISO_GET_TID list=$ISO_LIST_TID abc=$ISO_X"
# 非审计成员不得划拨集群
ISO_ASG=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/db-deploy/clusters/${ISO_DD_ID:-none}/assign-tenant" \
  -H "$BAUTH" -H "$BXT" -H 'Content-Type: application/json' -d '{"tenant_id":"'"$ABC_TID"'"}')
[[ "$ISO_ASG" == "403" || "$ISO_ASG" == "404" ]] && ok T22_dd_assign_forbidden || ko T22_dd_assign_forbidden "code=$ISO_ASG"
ISO_PX=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/proxy/services/none/assign-tenant" \
  -H "$BAUTH" -H "$BXT" -H 'Content-Type: application/json' -d '{"tenant_id":"'"$ABC_TID"'"}')
[[ "$ISO_PX" == "403" || "$ISO_PX" == "404" ]] && ok T23_proxy_assign_forbidden || ko T23_proxy_assign_forbidden "code=$ISO_PX"
ISO_DBA=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/databases/none/assign-tenant" \
  -H "$BAUTH" -H "$BXT" -H 'Content-Type: application/json' -d '{"tenant_id":"'"$ABC_TID"'"}')
[[ "$ISO_DBA" == "403" || "$ISO_DBA" == "404" ]] && ok T24_db_assign_forbidden || ko T24_db_assign_forbidden "code=$ISO_DBA"

[[ -n "$ISO_DD_ID" ]] && curl -sS -X DELETE "$BASE/db-deploy/clusters/$ISO_DD_ID" -H "$DAUTH" -H "$DXT" >/dev/null || true

[[ -n "${FOREIGN_ACC:-}" ]] && curl -sS -X DELETE "$BASE/accounts/$FOREIGN_ACC" -H "$DAUTH" -H "$DXT" >/dev/null || true
[[ -n "${RID:-}" ]] && curl -sS -X DELETE "$BASE/cmdb/relations/$RID" -H "$DAUTH" -H "$DXT" >/dev/null || true

echo "==== SUMMARY pass=$PASS fail=$FAIL ===="
exit "$FAIL"
