const g=[{label:"直接删除",value:"delete_only"},{label:"备份到目标实例",value:"backup_target"},{label:"归档到 OSS",value:"archive_oss"},{label:"自定义脚本",value:"custom_script"}],m=[{label:"立即执行",value:"immediate"},{label:"定时任务",value:"scheduled"}],E=[{label:"Shell",value:"shell"},{label:"Python 3+",value:"python"},{label:"Go",value:"go"},{label:"SQL",value:"sql"}],T=[{label:"自动（按预估数据量）",value:"auto"},{label:"CTE",value:"cte"},{label:"Shard",value:"shard"}],h={sql:`-- 在此编写 SQL
DELETE FROM t WHERE create_time < ...`,python:`def archive_delete():
    # 删除逻辑
    return deleted  # 整数行数；失败请 raise
`,go:`package main

// 填写区：host/dbname/tables 为空时用源端；删除条件 create_time < today-retainDays
var (
	host       = ""
	port       = 5432
	user       = ""
	password   = ""
	dbname     = ""
	tables     = ""
	retainDays = 30
	limit      = 1000
)

func main() {
}
`,shell:`# 在此编写 Shell 脚本
echo "archive"`},o=/^-- auto-dba-archive:([a-z_]+),([a-z_]+)\s*\n?/,d=/^-- auto-dba-schedule:([^\n]*)\n?/m,f=/^-- auto-dba-extra:([^\n]*)\n?/m;function y(e){var t;return((t=g.find(n=>n.value===e))==null?void 0:t.label)||"备份到目标实例"}function $(e){var t;return((t=m.find(n=>n.value===e))==null?void 0:t.label)||"立即执行"}function p(e){return String(e||"").replace(o,"").replace(d,"").replace(f,"").trim()}function S(e){if(!e||typeof e!="object")return{};const t={};for(const[n,a]of Object.entries(e)){if(a==null)continue;const s=typeof a=="string"?a.trim():a;s===""||s===!1||(t[n]=s)}return t}function O(e,t,n,a,s){const r=p(e);let u=`-- auto-dba-archive:${t||"backup_target"},${n||"immediate"}
`;(n||"immediate")==="scheduled"&&String(a||"").trim()&&(u+=`-- auto-dba-schedule:${String(a).trim()}
`);const c=S(s);return Object.keys(c).length&&(u+=`-- auto-dba-extra:${JSON.stringify(c)}
`),u+r}function b(e){const t=String(e||""),n=t.match(o),a=t.match(d),s=t.match(f);let r={};try{r=s!=null&&s[1]?JSON.parse(s[1]):{}}catch{r={}}return(!r||typeof r!="object")&&(r={}),{strategy:(n==null?void 0:n[1])||r.strategy||"backup_target",run_mode:(n==null?void 0:n[2])||r.run_mode||"immediate",schedule_expr:((a==null?void 0:a[1])||r.schedule_expr||"").trim(),sql:p(t),extra:r}}function x(e){const t=String(e||"").replace(/\s+$/g,"");return t.trim()?Object.values(h).some(n=>t===String(n).replace(/\s+$/g,"")):!0}function v(e){const t=e==null?void 0:e.approval_status;return t==="pending"||t==="rejected"||t==="canceled"?!1:!t&&!(e!=null&&e.issue_id)?!0:t==="approved"}function j(e){return!e||e.status==="running"||e.status==="paused"?!1:v(e)}function R(e){return{running:"执行中",executed:"成功",success:"成功",failed:"失败",paused:"已停用",pending:"待审批",scheduled:"定时待执行",rejected:"已驳回",canceled:"已撤销",approved:"已通过",init:"待执行"}[e]||e||"-"}function k(e){return e==="executed"||e==="success"||e==="approved"?"success":e==="failed"||e==="rejected"?"danger":e==="running"||e==="pending"?"warning":"info"}function P(e){if(!e)return"-";const t=new Date(e);if(Number.isNaN(t.getTime()))return String(e);const n=a=>String(a).padStart(2,"0");return`${t.getFullYear()}-${n(t.getMonth()+1)}-${n(t.getDate())} ${n(t.getHours())}:${n(t.getMinutes())}:${n(t.getSeconds())}`}function N(e,t=[]){const n=t.find(_=>_.id===e.source_instance_id),a=n?`${n.name||n.host||n.domain_name}`:e.source_host||"",s=e.source_db||"",r=e.table_name||e.source_table||"",u=b(e.condition),c=u.extra||{},i=[a,s].filter(Boolean).join(" / ")+(r&&r!=="script"?`.${r}`:"");if(u.strategy==="custom_script")return`${a||i||"-"}（自定义脚本）`;if(c.oss_bucket)return`${i||"-"} → OSS:${c.oss_bucket}`;const l=[c.target_host,c.target_db,c.target_table].filter(Boolean).join("/");return l?`${i||"-"} → ${l}`:i||"-"}function D(e){const{run_mode:t,schedule_expr:n}=b(e.condition);return(e.schedule_expr?"scheduled":t)==="scheduled"?e.schedule_expr||n||"定时任务":"立即执行"}function M(e){return{pending:"待审批",approved:"已通过",rejected:"已驳回",canceled:"已撤销",none:"—"}[e]||e||"—"}export{T as P,m as R,g as S,k as a,R as b,N as c,b as d,O as e,P as f,D as g,M as h,j as i,v as j,E as k,h as l,x as m,$ as r,y as s};
