package router

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/handler"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
	"auto-dba/internal/terminal"
)

type Deps struct {
	Cfg                *config.Config
	Auth               *handler.AuthHandler
	LoginSession       *handler.LoginSessionHandler
	Inst               *handler.InstanceHandler
	DB                 *handler.DatabaseHandler
	AccessDomain       *handler.AccessDomainHandler
	Region             *handler.RegionHandler
	Env                *handler.EnvironmentHandler
	SQLExec            *handler.SQLExecutorHandler
	Connector          *handler.ConnectorHandler
	Args               *handler.GlobalArgHandler
	Jobs               *handler.JobsHandler
	Ticket             *handler.TicketHandler
	CreateDB           *handler.CreateDBHandler
	Account            *handler.AccountHandler
	Review             *handler.ReviewRuleHandler
	CDC                *handler.CDCHandler
	Slot               *handler.SlotHandler
	Migration          *handler.MigrationHandler
	Flashback          *handler.FlashbackHandler
	Archive            *handler.ArchiveHandler
	Kafka              *handler.KafkaHandler
	Redis              *handler.RedisHandler // Redis 工单；为 nil 时不注册路由
	Insight            *handler.InsightHandler
	Session            *handler.SessionHandler
	PgSpace            *handler.PgSpaceHandler
	RedisRDB           *handler.RedisRDBHandler
	AWR                *handler.AWRHandler
	SqlInsightAnalysis *handler.SqlInsightAnalysisHandler
	DbMonitor          *handler.DbMonitorHandler
	InstanceOverview   *handler.OverviewHandler
	DbMonitorProxy     gin.HandlerFunc // mode=remote 时反向代理到独立 db-monitor 服务
	Multilang          *handler.MultilangHandler
	External           *handler.ExternalHandler
	MonitorTarget      *handler.MonitorTargetHandler
	MonitorTag         *handler.MonitorLabelTagHandler
	FeAdmin            *handler.FeAdminHandler
	DsAdmin            *handler.DsAdminHandler
	OSSSettings        *handler.OSSSettingsHandler
	OSSUpload          *handler.OSSUploadHandler
	StarRocksWeb       *handler.StarRocksWebHandler
	HA                 *handler.HAHandler
	Proxy              *handler.ProxyHandler
	Backup             *handler.BackupHandler
	K8s                *handler.K8sHandler
	SrCluster          *handler.SrClusterHandler
	DbDeploy           *handler.DbDeployHandler
	Cmdb               *handler.CmdbHandler
	Todo               *handler.TodoHandler
	Alert              *handler.AlertHandler
	Copilot            *handler.CopilotHandler
	Runbook            *handler.RunbookHandler
	DCL                *handler.DCLHandler
	ResourceApply      *handler.ResourceApplyHandler
	RBAC               *handler.RBACHandler
	Tenant             *handler.TenantHandler
	Ldap               *handler.LdapHandler
	Sso                *handler.SsoHandler
	SMTP               *handler.SMTPHandler
	Notify             *handler.NotifyHandler
	DNS                *handler.DNSHandler
	Terminal           *terminal.Handler
	Static             string
}

func Init(e *gin.Engine, d Deps) {
	e.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Tenant-Id"},
		ExposeHeaders:    []string{"X-Access-Token"},
		AllowCredentials: true,
	}))

	e.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	e.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ready"}) })

	api := e.Group("/api/v1")
	api.GET("/info", d.Auth.Info)
	api.POST("/auth/login", d.Auth.Login)
	api.GET("/auth/captcha", d.Auth.Captcha)
	api.POST("/auth/password/forgot", d.Auth.ForgotPassword)
	api.POST("/auth/password/reset", d.Auth.ResetPassword)
	api.GET("/auth/sso/login", d.Auth.SSOStart)
	api.GET("/auth/sso/callback", d.Auth.SSOCallback)
	api.POST("/auth/sso/exchange", d.Auth.SSOExchange)
	api.GET("/auth/sso/ticket", d.Auth.SSOTicket)

	var sessGate middleware.SessionGate
	if d.Auth != nil && d.Auth.Sessions != nil {
		sessGate = d.Auth.Sessions
	}
	auth := middleware.Auth(&d.Cfg.App.JWTSecret, d.Auth.Store, sessGate,
		"/healthz", "/readyz",
		"/api/v1/auth/login", "/api/v1/auth/captcha", "/api/v1/info",
		"/api/v1/auth/sso/",
		"/api/v1/auth/password/forgot", "/api/v1/auth/password/reset",
		"/api/v1/k8s/agents/",
	)
	secured := e.Group("/api/v1")
	secured.Use(auth)
	secured.Use(middleware.IdleSession(d.Auth.Store))
	secured.Use(middleware.TenantGuard(d.Auth.Store,
		"/api/v1/auth/me",
		"/api/v1/auth/logout",
		"/api/v1/auth/password",
		"/api/v1/auth/profile",
		"/api/v1/auth/select-tenant",
		"/api/v1/auth/sessions",
		"/api/v1/tenants",
	))

	gdb := d.Auth.Store
	gSrCl := handler.TenantResourceGuard(gdb, handler.GuardSrCluster)
	gSrCh := handler.TenantResourceGuard(gdb, handler.GuardSrChange)
	gSrAg := handler.TenantResourceGuard(gdb, handler.GuardSrAgent)
	gSrAgD := handler.TenantResourceGuard(gdb, handler.GuardSrAgentDeploy)
	gDdCl := handler.TenantResourceGuard(gdb, handler.GuardDbDeployCluster)
	gDdCh := handler.TenantResourceGuard(gdb, handler.GuardDbDeployChange)
	gDdAg := handler.TenantResourceGuard(gdb, handler.GuardDbDeployAgent)
	gHaCl := handler.TenantResourceGuard(gdb, handler.GuardHACluster)
	gHaCt := handler.TenantResourceGuard(gdb, handler.GuardHAController)
	gFb := handler.TenantResourceGuard(gdb, handler.GuardFlashback)
	gArc := handler.TenantResourceGuard(gdb, handler.GuardArchive)
	gPx := handler.TenantResourceGuard(gdb, handler.GuardProxyService)

	requireURN := func(urn string) gin.HandlerFunc {
		return middleware.RequireURN(d.RBAC.Store, urn)
	}

	secured.GET("/auth/me", d.Auth.Me)
	secured.POST("/auth/select-tenant", d.Auth.SelectTenant)
	secured.POST("/auth/logout", d.Auth.Logout)
	secured.PUT("/auth/password", d.Auth.ChangePassword)
	secured.PUT("/auth/profile", d.Auth.UpdateProfile)

	if d.LoginSession != nil {
		secured.GET("/auth/sessions/mine", d.LoginSession.Mine)
		secured.POST("/auth/sessions/revoke-others", d.LoginSession.RevokeOther)
		secured.GET("/auth/sessions/stats", d.LoginSession.Stats)
		secured.GET("/auth/sessions", d.LoginSession.List)
		secured.POST("/auth/sessions/kick", d.LoginSession.Kick)
	}

	if d.Tenant != nil {
		secured.GET("/tenants/mine", d.Tenant.Mine)
		secured.POST("/tenants/switch", d.Tenant.Switch)
		secured.GET("/tenants/plan-options", d.Tenant.PlanOptions)
		secured.GET("/tenant-acl-options", d.Tenant.ACLOptions)
		secured.GET("/tenants", requireURN(store.URNSidebarTenants), d.Tenant.List)
		secured.POST("/tenants", requireURN(store.URNTenantsCreate), d.Tenant.Create)
		secured.PUT("/tenants/:id", requireURN(store.URNTenantsEdit), d.Tenant.Update)
		secured.DELETE("/tenants/:id", requireURN(store.URNTenantsDelete), d.Tenant.Delete)
		secured.GET("/tenants/:id/members", requireURN(store.URNTenantsMembers), d.Tenant.ListMembers)
		secured.PUT("/tenants/:id/members", requireURN(store.URNTenantsMembers), d.Tenant.UpsertMember)
		secured.DELETE("/tenants/:id/members/:userId", requireURN(store.URNTenantsMembers), d.Tenant.RemoveMember)
		secured.GET("/tenants/:id/grants", requireURN(store.URNTenantsGrants), d.Tenant.ListGrants)
		secured.GET("/tenants/:id/grant-resources", requireURN(store.URNTenantsGrants), d.Tenant.ListGrantResources)
		secured.POST("/tenants/:id/grants", requireURN(store.URNTenantsGrants), d.Tenant.CreateGrant)
		secured.DELETE("/tenants/:id/grants/:grantId", requireURN(store.URNTenantsGrants), d.Tenant.DeleteGrant)
		secured.GET("/tenants/:id/audit-logs", requireURN(store.URNSidebarTenants), d.Tenant.AuditLogs)
		secured.POST("/tenants/:id/impersonate", requireURN(store.URNTenantsImpersonate), d.Tenant.Impersonate)
	}
	secured.GET("/login-logs", d.Auth.ListLoginLogs)
	secured.GET("/query-audits", d.Auth.ListQueryAudits)

	secured.GET("/settings/ldap", d.Ldap.Get)
	secured.PUT("/settings/ldap", requireURN(store.URNLdapEdit), d.Ldap.Update)
	secured.POST("/settings/ldap/test", requireURN(store.URNLdapEdit), d.Ldap.Test)
	if d.Sso != nil {
		secured.GET("/settings/sso", d.Sso.Get)
		secured.PUT("/settings/sso", d.Sso.Update)
		secured.POST("/settings/sso/test", d.Sso.Test)
	}
	if d.SMTP != nil {
		secured.GET("/settings/smtp", d.SMTP.Get)
		secured.PUT("/settings/smtp", d.SMTP.Update)
		secured.POST("/settings/smtp/test", d.SMTP.Test)
	}
	if d.Notify != nil {
		secured.GET("/settings/notify", d.Notify.Get)
		secured.PUT("/settings/notify", d.Notify.Update)
		secured.POST("/settings/notify/test", d.Notify.Test)
	}

	secured.GET("/menus/mine", d.RBAC.MineMenus)
	secured.GET("/menus/tree", d.RBAC.MenuTree)
	secured.GET("/menus/page-actions", d.RBAC.PageActions)
	secured.POST("/menus", requireURN(store.URNMenusCreate), d.RBAC.CreateMenu)
	secured.POST("/menus/:id/move", requireURN(store.URNMenusEdit), d.RBAC.MoveMenu)
	secured.PUT("/menus/:id", requireURN(store.URNMenusEdit), d.RBAC.UpdateMenu)
	secured.DELETE("/menus/:id", requireURN(store.URNMenusDelete), d.RBAC.DeleteMenu)

	secured.GET("/role-options", d.RBAC.ListRoleOptions)
	secured.GET("/roles", d.RBAC.ListRoles)
	secured.POST("/roles", requireURN(store.URNRolesCreate), d.RBAC.CreateRole)
	secured.PUT("/roles/:id", requireURN(store.URNRolesEdit), d.RBAC.UpdateRole)
	secured.DELETE("/roles/:id", requireURN(store.URNRolesDelete), d.RBAC.DeleteRole)
	secured.GET("/roles/:id/users", d.RBAC.ListRoleUsers)
	secured.GET("/roles/:id/menus", d.RBAC.GetRoleMenus)
	secured.PUT("/roles/:id/menus", requireURN(store.URNRolesEdit), d.RBAC.PutRoleMenus)

	secured.GET("/users", d.RBAC.ListUsers)
	secured.POST("/users", requireURN(store.URNUsersCreate), d.RBAC.CreateUser)
	secured.POST("/users/lark-sync", requireURN(store.URNUsersEdit), d.RBAC.SyncLarkUsers)
	secured.PUT("/users/:id", requireURN(store.URNUsersEdit), d.RBAC.UpdateUser)
	secured.PUT("/users/:id/password", requireURN(store.URNUsersEdit), d.RBAC.ResetUserPassword)
	secured.PUT("/users/:id/disable", requireURN(store.URNUsersEdit), d.RBAC.DisableUser)
	secured.PUT("/users/:id/enable", requireURN(store.URNUsersEdit), d.RBAC.EnableUser)
	secured.PUT("/users/:id/unlock", requireURN(store.URNUsersEdit), d.RBAC.UnlockUser)
	secured.DELETE("/users/:id", requireURN(store.URNUsersDelete), d.RBAC.DeleteUser)

	if d.AccessDomain != nil {
		secured.GET("/access-domains", d.AccessDomain.List)
		secured.POST("/access-domains/sync", requireURN(store.URNDomainsSync), d.AccessDomain.Sync)
		secured.POST("/access-domains/batch-delete", requireURN(store.URNDomainsDelete), d.AccessDomain.DeleteBatch)
		secured.DELETE("/access-domains/:id", requireURN(store.URNDomainsDelete), d.AccessDomain.Delete)
		secured.POST("/access-domains/assign-tenant", requireURN(store.URNTenantsImpersonate), d.AccessDomain.AssignTenantBatch)
		secured.POST("/access-domains/:id/assign-tenant", requireURN(store.URNTenantsImpersonate), d.AccessDomain.AssignTenant)
	}

	secured.GET("/instances", d.Inst.List)
	secured.POST("/instances", requireURN(store.URNInstancesCreate), d.Inst.Create)
	secured.POST("/instances/test-connection", d.Inst.TestConnection)
	secured.POST("/instances/sync-hosts-from-monitor", requireURN(store.URNInstancesCreate), d.Inst.SyncHostsFromMonitor)
	secured.GET("/instances/:id", d.Inst.Get)
	secured.PUT("/instances/:id", requireURN(store.URNInstancesEdit), d.Inst.Update)
	secured.POST("/instances/:id/assign-tenant", requireURN(store.URNTenantsImpersonate), d.Inst.AssignTenant)
	secured.DELETE("/instances/:id", requireURN(store.URNInstancesDelete), d.Inst.Delete)
	if d.Terminal != nil {
		secured.POST("/instances/:id/open-terminal", d.Terminal.OpenInstance)
		secured.POST("/terminal", d.Terminal.Open)
		// 页面与 WS 用 session JWT（query token），不走登录 Auth
		api.GET("/terminal", d.Terminal.Page)
		api.GET("/terminal/ws", d.Terminal.WS)
		d.Terminal.MountStatic(e)
	}
	secured.GET("/instances/:id/kafka/topics", d.Inst.ListKafkaTopics)
	secured.POST("/instances/:id/kafka/topics/stats", d.Inst.KafkaTopicStats)
	secured.DELETE("/instances/:id/kafka/topics/:topic", d.Inst.DeleteKafkaTopic)
	secured.GET("/instances/:id/kafka/topics/:topic/partitions", d.Inst.GetKafkaTopicPartitions)
	secured.GET("/instances/:id/kafka/topics/:topic/configs", d.Inst.GetKafkaTopicConfigs)
	secured.GET("/instances/:id/kafka/topics/:topic/logs", d.Inst.GetKafkaTopicLogs)
	secured.GET("/instances/:id/kafka/topics/:topic/messages", d.Inst.QueryKafkaTopicMessages)
	secured.GET("/instances/:id/redis/databases", d.Inst.ListRedisDatabases)
	secured.POST("/instances/:id/redis/execute", d.Inst.ExecuteRedis)

	secured.GET("/instances/:id/databases", d.DB.ListLive)
	secured.GET("/databases", d.DB.List)
	secured.POST("/databases", requireURN(store.URNDatabasesCreate), d.DB.Create)
	secured.POST("/databases/batch", requireURN(store.URNDatabasesCreate), d.DB.CreateBatch)
	secured.POST("/databases/assign-tenant", requireURN(store.URNTenantsImpersonate), d.DB.AssignTenantBatch)
	secured.POST("/databases/batch-approvers", requireURN(store.URNDatabasesEdit), d.DB.BatchApprovers)
	secured.POST("/databases/batch-access-users", requireURN(store.URNDatabasesEdit), d.DB.BatchAccessUsers)
	secured.POST("/databases/batch-delete", requireURN(store.URNDatabasesDelete), d.DB.DeleteBatch)
	secured.GET("/databases/:id/tables", d.DB.ListTables)
	secured.PUT("/databases/:id", requireURN(store.URNDatabasesEdit), d.DB.Update)
	secured.POST("/databases/:id/assign-tenant", requireURN(store.URNTenantsImpersonate), d.DB.AssignTenant)
	secured.DELETE("/databases/:id", requireURN(store.URNDatabasesDelete), d.DB.Delete)
	secured.POST("/query", d.DB.Query)
	secured.POST("/query/table-info", d.DB.TableInfo)
	secured.GET("/query/favorites", d.DB.ListQueryFavorites)
	secured.POST("/query/favorites", d.DB.CreateQueryFavorite)
	secured.DELETE("/query/favorites/:id", d.DB.DeleteQueryFavorite)
	secured.GET("/query/history", d.DB.ListMyQueryHistory)

	secured.GET("/regions", d.Region.List)
	secured.POST("/regions", requireURN(store.URNRegionsCreate), d.Region.Create)
	secured.PUT("/regions/:id", requireURN(store.URNRegionsEdit), d.Region.Update)
	secured.DELETE("/regions/:id", requireURN(store.URNRegionsDelete), d.Region.Delete)
	secured.GET("/regions/:id/envs", d.Region.ListEnvs)
	secured.PUT("/regions/:id/envs", requireURN(store.URNRegionsBind), d.Region.PutEnvs)

	secured.GET("/environments", d.Env.List)
	secured.POST("/environments", requireURN(store.URNEnvironmentsCreate), d.Env.Create)
	secured.PUT("/environments/:id", requireURN(store.URNEnvironmentsEdit), d.Env.Update)
	secured.DELETE("/environments/:id", requireURN(store.URNEnvironmentsDelete), d.Env.Delete)

	if d.SQLExec != nil {
		execURN := requireURN(store.URNSidebarSQLExecutors)
		secured.GET("/sql-executors", execURN, d.SQLExec.List)
		secured.PUT("/sql-executors/local-idc", execURN, d.SQLExec.SaveLocalIDC)
		secured.POST("/sql-executors", execURN, d.SQLExec.Create)
		secured.PUT("/sql-executors/:id", execURN, d.SQLExec.Update)
		secured.DELETE("/sql-executors/:id", execURN, d.SQLExec.Delete)
		secured.POST("/sql-executors/:id/probe", execURN, d.SQLExec.Probe)
	}

	if d.DNS != nil {
		view := requireURN(store.URNSidebarDNS)
		secured.GET("/dns/zones", view, d.DNS.ListZones)
		secured.POST("/dns/zones", requireURN(store.URNDNSCreate), d.DNS.CreateZone)
		secured.PUT("/dns/zones/:id", requireURN(store.URNDNSEdit), d.DNS.UpdateZone)
		secured.DELETE("/dns/zones/:id", requireURN(store.URNDNSDelete), d.DNS.DeleteZone)
		secured.GET("/dns/ranges", view, d.DNS.ListRanges)
		secured.POST("/dns/ranges", requireURN(store.URNDNSCreate), d.DNS.CreateRange)
		secured.PUT("/dns/ranges/:id", requireURN(store.URNDNSEdit), d.DNS.UpdateRange)
		secured.DELETE("/dns/ranges/:id", requireURN(store.URNDNSDelete), d.DNS.DeleteRange)
		secured.GET("/dns-records", view, d.DNS.ListRecords)
		secured.POST("/dns-records", requireURN(store.URNDNSCreate), d.DNS.CreateRecords)
		secured.PUT("/dns-records/:id", requireURN(store.URNDNSEdit), d.DNS.UpdateRecord)
		secured.DELETE("/dns-records/:id", requireURN(store.URNDNSDelete), d.DNS.DeleteRecord)
		secured.POST("/dns-records/batch-delete", requireURN(store.URNDNSDelete), d.DNS.BatchDeleteRecords)
		secured.GET("/dns/nodes", requireURN(store.URNSidebarDNSNodes), d.DNS.ListNodes)
		secured.GET("/dns/metrics", requireURN(store.URNSidebarDNSNodes), d.DNS.Metrics)
		secured.GET("/dns/changes", requireURN(store.URNSidebarDNSChanges), d.DNS.ListChanges)
		secured.GET("/dns/changes/:batch", view, d.DNS.GetChange)
		secured.POST("/dns/changes/:batch/rollback", requireURN(store.URNDNSRollback), d.DNS.RollbackChange)
		secured.GET("/dns/inspect", requireURN(store.URNSidebarDNSInspect), d.DNS.Inspect)
		secured.GET("/dns/reverse", view, d.DNS.Reverse)
		secured.GET("/dns/export", view, d.DNS.Export)
		secured.POST("/dns/import/preview", requireURN(store.URNDNSImport), d.DNS.ImportPreview)
		secured.POST("/dns/import/apply", requireURN(store.URNDNSImport), d.DNS.ImportApply)
		secured.POST("/dns/resolve/simulate", requireURN(store.URNSidebarDNSTools), d.DNS.Simulate)
		secured.POST("/dns/resolve/probe", requireURN(store.URNSidebarDNSTools), d.DNS.Probe)
		secured.POST("/dns/resolve/node", requireURN(store.URNSidebarDNSTools), d.DNS.NodeQuery)
		secured.GET("/dns/policies", view, d.DNS.ListPolicies)
		secured.PUT("/dns/policies/:zone_id", requireURN(store.URNDNSPolicy), d.DNS.SavePolicy)
		secured.GET("/dns/ttl-holds", view, d.DNS.ListTTLHolds)
		secured.POST("/dns/ttl-holds", requireURN(store.URNDNSEdit), d.DNS.LowerTTL)
		secured.POST("/dns/ttl-holds/:id/restore", requireURN(store.URNDNSEdit), d.DNS.RestoreTTL)
	}

	if d.MonitorTag != nil {
		secured.GET("/monitor-tags", d.MonitorTag.List)
		secured.POST("/monitor-tags", requireURN(store.URNMonitorTagsCreate), d.MonitorTag.Create)
		secured.PUT("/monitor-tags/:id", requireURN(store.URNMonitorTagsEdit), d.MonitorTag.Update)
		secured.DELETE("/monitor-tags/:id", requireURN(store.URNMonitorTagsDelete), d.MonitorTag.Delete)
	}

	if d.Todo != nil {
		td := secured.Group("/todos")
		td.GET("", d.Todo.List)
		td.GET("/users", d.Todo.Users)
		td.POST("/images", d.Todo.UploadImage)
		td.GET("/images/:iid", d.Todo.Image)
		td.GET("/:id", d.Todo.Get)
		td.POST("", requireURN(store.URNTodoCreate), d.Todo.Create)
		td.PUT("/:id", requireURN(store.URNTodoEdit), d.Todo.Update)
		td.POST("/:id/subtasks", requireURN(store.URNTodoFollow), d.Todo.AddSubtask)
		td.PUT("/:id/subtasks/:sid", requireURN(store.URNTodoFollow), d.Todo.UpdateSubtask)
		td.POST("/:id/subtasks/:sid/progress", requireURN(store.URNTodoFollow), d.Todo.SubtaskProgress)
		td.DELETE("/:id/subtasks/:sid", requireURN(store.URNTodoEdit), d.Todo.DeleteSubtask)
		td.POST("/:id/handover", requireURN(store.URNTodoFollow), d.Todo.Handover)
		td.POST("/:id/close", requireURN(store.URNTodoFollow), d.Todo.Close)
		td.POST("/:id/reopen", requireURN(store.URNTodoFollow), d.Todo.Reopen)
		td.DELETE("/:id", requireURN(store.URNTodoDelete), d.Todo.Delete)
	}

	if d.Alert != nil {
		aik := secured.Group("/aiops")
		aik.GET("/knowledge", d.Alert.ListKnowledge)
		aik.GET("/knowledge/db", d.Alert.ListKnowledgeDB)
		aik.POST("/knowledge/learn", d.Alert.LearnKnowledge)
		aik.DELETE("/knowledge/db", d.Alert.DeleteKnowledgeDB)
		aik.GET("/knowledge/match", d.Alert.MatchKnowledgePreview)
		aik.GET("/incidents", d.Alert.ListIncidents)
		aik.POST("/rag/search", d.Alert.RAGSearch)

		if d.Copilot != nil {
			cp := secured.Group("/copilot")
			cp.GET("/models", requireURN(store.URNCopilotChat), d.Copilot.Models)
			cp.POST("/chat", requireURN(store.URNCopilotChat), d.Copilot.Chat)
			cp.GET("/sessions", requireURN(store.URNCopilotChat), d.Copilot.ListSessions)
			cp.GET("/sessions/:id/turns", requireURN(store.URNCopilotChat), d.Copilot.GetSessionTurns)
		}
		if d.Runbook != nil {
			rb := secured.Group("/runbook")
			rb.POST("/preview", d.Runbook.Preview)
			rb.POST("/execute", requireURN(store.URNRunbookExecute), d.Runbook.Execute)
		}

		al := secured.Group("/alerts")
		al.GET("", d.Alert.List)
		al.GET("/stats", d.Alert.Stats)
		al.GET("/activity", requireURN(store.URNSidebarAlertActivity), d.Alert.Activity)
		al.POST("/postmortem", d.Alert.Postmortem)
		al.GET("/:id", d.Alert.Get)
		al.GET("/:id/events", d.Alert.Events)
		al.GET("/:id/context-bundle", d.Alert.ContextBundle)
		al.GET("/:id/runbook", d.Alert.RunbookPreview)
		al.POST("/:id/copilot", requireURN(store.URNCopilotChat), d.Alert.CopilotChat)
		al.POST("/:id/claim", requireURN(store.URNAlertClaim), d.Alert.Claim)
		al.POST("/:id/handover", requireURN(store.URNAlertClaim), d.Alert.Handover)
		al.POST("/:id/processing", requireURN(store.URNAlertClaim), d.Alert.Processing)
		al.POST("/:id/resolve", requireURN(store.URNAlertResolve), d.Alert.Resolve)
		al.POST("/:id/false-positive", requireURN(store.URNAlertResolve), d.Alert.FalsePositive)
		al.POST("/:id/silence", requireURN(store.URNAlertSilence), d.Alert.QuickSilence)
		al.POST("/:id/todo", requireURN(store.URNTodoCreate), d.Alert.CreateTodo)

		as := secured.Group("/alert-silences")
		as.GET("", d.Alert.ListSilences)
		as.POST("", requireURN(store.URNAlertSilence), d.Alert.CreateSilence)
		as.POST("/:id/cancel", requireURN(store.URNAlertSilence), d.Alert.CancelSilence)

		ar := secured.Group("/alert-rules")
		ar.GET("", d.Alert.ListRules)
		ar.GET("/templates", d.Alert.ListExprTemplates)
		ar.POST("/test", requireURN(store.URNAlertRuleEdit), d.Alert.TestRule)
		ar.POST("/compile-preview", requireURN(store.URNAlertRuleEdit), d.Alert.CompilePreview)
		ar.GET("/:id", d.Alert.GetRule)
		ar.POST("", requireURN(store.URNAlertRuleEdit), d.Alert.CreateRule)
		ar.PUT("/:id", requireURN(store.URNAlertRuleEdit), d.Alert.UpdateRule)
		ar.DELETE("/:id", requireURN(store.URNAlertRuleDelete), d.Alert.DeleteRule)
		ar.POST("/:id/test", requireURN(store.URNAlertRuleEdit), d.Alert.TestRule)

		ai := secured.Group("/alert-inhibitions")
		ai.GET("", d.Alert.ListInhibitions)
		ai.POST("", requireURN(store.URNAlertRuleEdit), d.Alert.CreateInhibition)
		ai.PUT("/:id", requireURN(store.URNAlertRuleEdit), d.Alert.UpdateInhibition)
		ai.DELETE("/:id", requireURN(store.URNAlertRuleDelete), d.Alert.DeleteInhibition)

		art := secured.Group("/alert-routes")
		art.GET("", d.Alert.ListRoutes)
		art.POST("", requireURN(store.URNAlertRuleEdit), d.Alert.CreateRoute)
		art.PUT("/:id", requireURN(store.URNAlertRuleEdit), d.Alert.UpdateRoute)
		art.DELETE("/:id", requireURN(store.URNAlertRuleDelete), d.Alert.DeleteRoute)

		ao := secured.Group("/alert-oncall")
		ao.GET("", d.Alert.ListOncall)
		ao.POST("", requireURN(store.URNAlertRuleEdit), d.Alert.CreateOncall)
		ao.PUT("/:id", requireURN(store.URNAlertRuleEdit), d.Alert.UpdateOncall)
		ao.DELETE("/:id", requireURN(store.URNAlertRuleDelete), d.Alert.DeleteOncall)
	}

	if d.Cmdb != nil {
		cm := secured.Group("/cmdb")
		cm.GET("/cis", d.Cmdb.ListCIs)
		cm.GET("/cis/:type/:id", d.Cmdb.GetCI)
		cm.GET("/cis/:type/:id/graph", d.Cmdb.Graph)
		cm.GET("/cis/:type/:id/impact", d.Cmdb.Impact)
		cm.GET("/cis/:type/:id/tags", d.Cmdb.GetCITags)
		cm.PUT("/cis/:type/:id/tags", requireURN(store.URNCmdbEdit), d.Cmdb.SetCITags)
		cm.GET("/cis/:type/:id/snapshots", d.Cmdb.ListSnapshots)
		cm.POST("/cis/:type/:id/snapshots", requireURN(store.URNCmdbEdit), d.Cmdb.CreateSnapshot)
		cm.GET("/relations", d.Cmdb.ListRelations)
		cm.POST("/relations", requireURN(store.URNCmdbEdit), d.Cmdb.CreateRelation)
		cm.DELETE("/relations/:id", requireURN(store.URNCmdbEdit), d.Cmdb.DeleteRelation)
		cm.GET("/tags", d.Cmdb.ListTagDefs)
		cm.POST("/tags", requireURN(store.URNCmdbEdit), d.Cmdb.UpsertTagDef)
		cm.PUT("/tags/:id", requireURN(store.URNCmdbEdit), d.Cmdb.UpsertTagDef)
		cm.DELETE("/tags/:id", requireURN(store.URNCmdbEdit), d.Cmdb.DeleteTagDef)
		cm.POST("/tags/sync-monitor", requireURN(store.URNCmdbEdit), d.Cmdb.SyncMonitorTags)
		cm.GET("/audit", d.Cmdb.ListAudit)
		cm.GET("/discover/from-monitor", d.Cmdb.DiscoverFromMonitor)
		cm.GET("/discover/from-clusters", d.Cmdb.DiscoverFromClusters)
		cm.GET("/discover/health", d.Cmdb.HealthOverview)
		cm.GET("/discover/databases", d.Cmdb.DiscoverDatabases)
		cm.POST("/discover/databases/register", requireURN(store.URNCmdbEdit), d.Cmdb.RegisterDatabases)
		cm.GET("/drifts", d.Cmdb.ListDrifts)
		cm.GET("/by-app", d.Cmdb.ListByApp)
		cm.GET("/export", requireURN(store.URNCmdbImport), d.Cmdb.Export)
		cm.POST("/import", requireURN(store.URNCmdbImport), d.Cmdb.Import)
		cm.GET("/sync/jobs", d.Cmdb.ListSyncJobs)
		cm.POST("/sync/jobs", requireURN(store.URNCmdbSync), d.Cmdb.UpsertSyncJob)
		cm.PUT("/sync/jobs/:id", requireURN(store.URNCmdbSync), d.Cmdb.UpsertSyncJob)
		cm.DELETE("/sync/jobs/:id", requireURN(store.URNCmdbSync), d.Cmdb.DeleteSyncJob)
		cm.POST("/sync/jobs/:id/run", requireURN(store.URNCmdbSync), d.Cmdb.RunSyncJob)
		cm.GET("/sync/runs", d.Cmdb.ListSyncRuns)
	}

	secured.GET("/connectors", d.Connector.List)
	secured.POST("/connectors", requireURN(store.URNConnectorsCreate), d.Connector.Create)
	secured.GET("/connectors/:id", d.Connector.Get)
	secured.PUT("/connectors/:id", requireURN(store.URNConnectorsEdit), d.Connector.Update)
	secured.DELETE("/connectors/:id", requireURN(store.URNConnectorsDelete), d.Connector.Delete)

	secured.GET("/global-args", d.Args.List)
	secured.POST("/global-args", requireURN(store.URNArgsCreate), d.Args.Create)
	secured.PUT("/global-args", requireURN(store.URNArgsEdit), d.Args.Update)
	secured.DELETE("/global-args/:key", requireURN(store.URNArgsDelete), d.Args.Delete)

	if d.Jobs != nil {
		jg := secured.Group("/jobs")
		jgView := requireURN(store.URNSidebarScheduledJobs)
		jg.GET("", jgView, d.Jobs.List)
		jg.GET("/cluster", jgView, d.Jobs.Cluster)
		jg.GET("/business-schedules", jgView, d.Jobs.BusinessSchedules)
		jg.GET("/:key", jgView, d.Jobs.Get)
		jg.GET("/:key/runs", jgView, d.Jobs.Runs)
		jg.GET("/:key/audits", jgView, d.Jobs.Audits)
		jg.PUT("/:key", requireURN(store.URNScheduledJobsEdit), d.Jobs.Update)
		jg.POST("/:key/trigger", requireURN(store.URNScheduledJobsTrigger), d.Jobs.Trigger)
	}

	t := secured.Group("/tickets")
	t.GET("/targets", d.Ticket.GetTargets)
	t.GET("/db-types", d.Ticket.ListDBTypes)
	t.GET("/instances", d.Ticket.ListInstances)
	t.GET("/instances/:instance_id/databases", d.Ticket.ListInstanceDatabases)
	t.GET("/target-approvers", d.Ticket.GetTargetApprovers)
	t.GET("/env-groups", d.Ticket.EnvGroups)
	t.POST("/check-sql", d.Ticket.CheckSQL)
	t.GET("/stats", d.Ticket.Stats)
	t.GET("/dashboard/stats", d.Ticket.DashboardStats)
	t.GET("/todos", d.Ticket.Todos)
	t.GET("/activities", d.Ticket.Activities)
	t.POST("", d.Ticket.Submit)
	t.GET("", d.Ticket.List)
	t.GET("/:id", d.Ticket.GetDetail)
	t.PUT("/:id/approve", d.Ticket.Approve)
	t.PUT("/:id/reject", d.Ticket.Reject)
	t.PUT("/:id/cancel", d.Ticket.Cancel)
	t.PUT("/:id/assignee", d.Ticket.TransferAssignee)
	t.POST("/:id/execute", d.Ticket.Execute)
	t.POST("/:id/stop", d.Ticket.Stop)
	t.POST("/:id/export", d.Ticket.Export)

	cd := secured.Group("/create-db")
	cd.GET("/instances", d.CreateDB.ListInstances)
	cd.GET("/approvers", d.CreateDB.ListApprovers)
	cd.POST("/check-sql", d.CreateDB.CheckSQL)
	cd.POST("", d.CreateDB.Submit)

	acc := secured.Group("/db-accounts")
	acc.GET("", d.Account.List)
	acc.POST("", requireURN(store.URNAccountsCreate), d.Account.Create)
	acc.GET("/:id", d.Account.Get)
	acc.PUT("/:id", requireURN(store.URNAccountsEdit), d.Account.Update)
	acc.DELETE("/:id", requireURN(store.URNAccountsDelete), d.Account.Delete)

	rr := secured.Group("/review-rules")
	rr.GET("/ping", d.Review.Ping)
	rr.GET("/goinception", d.Review.GetGoInception)
	rr.PUT("/goinception", requireURN(store.URNReviewRulesEdit), d.Review.UpdateGoInception)
	rr.POST("/goinception/ping", d.Review.PingGoInception)
	rr.GET("", d.Review.List)
	rr.POST("", requireURN(store.URNReviewRulesCreate), d.Review.Create)
	rr.PUT("", requireURN(store.URNReviewRulesEdit), d.Review.Update)

	secured.POST("/dcl-grants", d.DCL.Submit)

	if d.ResourceApply != nil {
		ra := secured.Group("/resource-apply")
		ra.GET("/candidates", d.ResourceApply.Candidates)
		ra.GET("/approvers", d.ResourceApply.Approvers)
		ra.GET("/target-users", d.ResourceApply.TargetUsers)
		ra.POST("", d.ResourceApply.Submit)
	}

	cdc := secured.Group("/cdc-sync")
	cdc.GET("/instances", d.CDC.ListInstances)
	cdc.GET("/instances/:id/databases/:db/tables/:table/columns", d.CDC.TableColumns)
	cdc.POST("/instances/:id/columns/batch", d.CDC.BatchColumns)
	cdc.POST("/tasks/precheck", d.CDC.Precheck)
	cdc.POST("/tasks/debezium-preview", d.CDC.DebeziumPreview)
	cdc.POST("/tasks/preview-target-sql", d.CDC.PreviewTargetSQL)
	cdc.POST("/tasks/batch-add-table/precheck", d.CDC.BatchAddTablePrecheck)
	cdc.POST("/tasks/batch-add-table", d.CDC.BatchAddTable)
	cdc.POST("/tasks/batch-alter-schema", d.CDC.BatchAlterSchema)
	cdc.POST("/tasks/synced-tables/batch", d.CDC.SyncedTablesBatch)
	cdc.GET("/tasks/init-list", d.CDC.InitList)
	cdc.GET("/tasks/reinit/candidates", d.CDC.ReinitCandidates)
	cdc.POST("/tasks/reinit/batch", d.CDC.ReinitBatch)
	cdc.POST("/discover/tables", d.CDC.DiscoverTables)
	cdc.POST("/discover/databases", d.CDC.DiscoverDatabases)
	cdc.GET("/schema-alerts", d.CDC.ListSchemaAlerts)
	cdc.POST("/schema-alerts/scan-all", d.CDC.ScanAllSchemaAlerts)
	cdc.POST("/tasks", d.CDC.Create)
	cdc.POST("/tasks/:id/schema-diff", d.CDC.SchemaDiff)
	cdc.GET("/tasks/:id/schema-alerts", d.CDC.SchemaAlerts)
	cdc.POST("/tasks/:id/schema-alerts/scan", d.CDC.ScanSchemaAlerts)
	cdc.POST("/tasks/:id/schema-migrate-sql", d.CDC.SchemaMigrateSQL)
	cdc.GET("/tasks/:id/publication", d.CDC.Publication)
	cdc.POST("/tasks/:id/publication/tables", d.CDC.AddPublicationTables)
	cdc.DELETE("/tasks/:id/publication/tables", d.CDC.RemovePublicationTables)
	cdc.GET("/tasks/:id/synced-tables", d.CDC.SyncedTables)
	cdc.GET("/tasks/:id/add-table/available-tables", d.CDC.AvailableTables)
	cdc.POST("/tasks/:id/delete", d.CDC.RequestDelete)
	cdc.GET("/tasks", d.CDC.List)
	cdc.GET("/tasks/:id", d.CDC.Get)
	cdc.GET("/tasks/:id/status", d.CDC.Status)
	cdc.GET("/tasks/:id/tables", d.CDC.Tables)
	cdc.GET("/tasks/:id/tables/:mappingId/ddl", d.CDC.MappingDDL)
	cdc.GET("/tasks/:id/logs", d.CDC.Logs)
	cdc.GET("/tasks/:id/connector/status", d.CDC.ConnectorRuntimeStatus)
	cdc.GET("/tasks/:id/connector", d.CDC.Connector)
	cdc.PUT("/tasks/:id/connector", d.CDC.ConnectorUpdate)
	cdc.POST("/tasks/:id/repair-source", d.CDC.RepairSource)
	cdc.GET("/tasks/:id/connector/versions", d.CDC.ConnectorVersionList)
	cdc.GET("/tasks/:id/connector/versions/:versionId", d.CDC.ConnectorVersionDetail)
	cdc.POST("/tasks/:id/connector/versions/:versionId/restore/preview", d.CDC.ConnectorVersionRestorePreview)
	cdc.POST("/tasks/:id/connector/versions/:versionId/restore", d.CDC.ConnectorVersionRestore)
	cdc.DELETE("/tasks/:id", d.CDC.Delete)
	cdc.POST("/tasks/:id/pause", d.CDC.Action("paused", "none"))
	cdc.POST("/tasks/:id/resume", d.CDC.Action("running", "approved"))
	cdc.POST("/tasks/:id/restart", d.CDC.Action("running", "none"))
	cdc.POST("/tasks/:id/retry", d.CDC.Action("running", "none"))
	cdc.POST("/tasks/:id/reinit/preview", d.CDC.ReinitPreview)
	cdc.POST("/tasks/:id/reinit", d.CDC.Reinit)

	secured.GET("/sync-slots", d.Slot.List)
	secured.POST("/sync-slots/collect", d.Slot.Collect)
	secured.GET("/sync-slots/collect", d.Slot.CollectProgress)
	secured.POST("/sync-slots/restart", d.Slot.Restart)
	secured.GET("/sync-slots/restart", d.Slot.RestartProgress)
	secured.GET("/sync-slots/logs", d.Slot.OpLogs)
	secured.POST("/sync-slots/delete", d.Slot.Delete)

	if d.InstanceOverview != nil {
		secured.GET("/instance-overview", d.InstanceOverview.List)
		secured.POST("/instance-overview/collect", d.InstanceOverview.Collect)
	}

	mig := secured.Group("/migration")
	mig.GET("/settings", d.Migration.GetSettings)
	mig.PUT("/settings", d.Migration.PutSettings)
	mig.POST("/oss/test", d.Migration.TestOSS)
	mig.GET("/tasks", d.Migration.List)
	mig.POST("/tasks", d.Migration.Create)
	mig.POST("/tasks/precheck", d.Migration.Precheck)
	mig.POST("/schema-compare", d.Migration.SchemaCompare)
	mig.POST("/schema-compare/tasks", d.Migration.SchemaCompareCreate)
	mig.GET("/schema-compare/tasks", d.Migration.SchemaCompareList)
	mig.GET("/schema-compare/tasks/:id", d.Migration.SchemaCompareGet)
	mig.GET("/schema-compare/tasks/:id/export", d.Migration.SchemaCompareExport)
	mig.DELETE("/schema-compare/tasks/:id", d.Migration.SchemaCompareDelete)
	mig.GET("/tasks/:id", d.Migration.Get)
	mig.PUT("/tasks/:id", d.Migration.Update)
	mig.DELETE("/tasks/:id", d.Migration.Delete)
	mig.GET("/tasks/:id/objects", d.Migration.Objects)
	mig.GET("/tasks/:id/logs", d.Migration.Logs)
	mig.GET("/tasks/:id/prechecks", d.Migration.Prechecks)
	mig.GET("/tasks/:id/progress", d.Migration.Progress)
	mig.POST("/tasks/:id/verify", d.Migration.VerifyPost)
	mig.GET("/tasks/:id/verify", d.Migration.VerifyGet)
	mig.GET("/tasks/:id/verify/result", d.Migration.VerifyGet)
	mig.POST("/tasks/:id/precheck", d.Migration.Precheck)
	mig.POST("/tasks/:id/run-precheck", d.Migration.Precheck)
	mig.POST("/tasks/:id/start", d.Migration.Start)
	mig.POST("/tasks/:id/run", d.Migration.Start)
	mig.POST("/tasks/:id/cancel", d.Migration.Cancel)
	mig.POST("/tasks/:id/retry", d.Migration.Retry)
	mig.POST("/tasks/:id/pause", d.Migration.Pause)
	mig.POST("/tasks/:id/resume", d.Migration.Resume)
	mig.POST("/tasks/:id/schedule", d.Migration.Schedule)
	mig.POST("/tasks/:id/cancel-schedule", d.Migration.CancelSchedule)
	mig.POST("/tasks/:id/terminate", d.Migration.Terminate)
	mig.POST("/tasks/:id/submit-approval", d.Migration.SubmitApproval)
	mig.POST("/tasks/:id/approve", d.Migration.Approve)
	mig.POST("/tasks/:id/reject", d.Migration.Reject)
	mig.GET("/instances/:id/connect", d.Migration.Connect)
	mig.GET("/instances/:id/databases", d.Migration.Databases)
	mig.POST("/instances/:id/databases/tables", d.Migration.BatchTables)
	mig.GET("/instances/:id/databases/:db/tables", d.Migration.Tables)

	fb := secured.Group("/flashback")
	fb.GET("/cloud-settings", d.Flashback.GetCloudSettings)
	fb.PUT("/cloud-settings", d.Flashback.PutCloudSettings)
	fb.GET("/tasks", d.Flashback.List)
	fb.POST("/tasks", d.Flashback.Create)
	fb.POST("/tasks/precheck", d.Flashback.Precheck)
	fb.GET("/tasks/:id", gFb, d.Flashback.Get)
	fb.GET("/tasks/:id/sql", gFb, d.Flashback.SQLs)
	fb.GET("/tasks/:id/logs", gFb, d.Flashback.Logs)
	fb.POST("/tasks/:id/submit-dml-ticket", gFb, d.Flashback.SubmitDML)

	arc := secured.Group("/archive")
	arc.GET("/tasks", d.Archive.List)
	arc.POST("/tasks", d.Archive.Create)
	arc.GET("/tasks/:id", gArc, d.Archive.Get)
	arc.PATCH("/tasks/:id", gArc, d.Archive.Update)
	arc.GET("/stats", d.Archive.Stats)
	arc.GET("/meta", d.Archive.Meta)
	arc.GET("/runs", d.Archive.ListRuns)
	arc.POST("/check", d.Archive.Check)
	arc.GET("/settings", d.Archive.GetSettings)
	arc.PATCH("/settings", d.Archive.PutSettings)
	arc.POST("/settings/:kind/test", d.Archive.TestSettings)
	arc.POST("/tasks/:id/run", gArc, d.Archive.Run)
	arc.POST("/tasks/:id/pause", gArc, d.Archive.Pause)
	arc.GET("/tasks/:id/runs", gArc, d.Archive.ListRuns)
	arc.POST("/tasks/:id/runs/:runId/stop", gArc, d.Archive.StopRun)

	secured.GET("/kafka/orders", d.Kafka.List)
	secured.POST("/kafka/orders", d.Kafka.Create)
	secured.GET("/kafka/instances", d.Kafka.Instances)

	if d.Redis != nil {
		secured.GET("/redis/command-catalog", d.Redis.Catalog)
		secured.GET("/redis/command-rules", d.Redis.GetCommandRules)
		secured.PUT("/redis/command-rules", d.Redis.UpdateCommandRules)
		secured.GET("/redis/instances", d.Redis.Instances)
		secured.POST("/redis/orders", d.Redis.Create)
	}

	if d.HA != nil {
		hg := secured.Group("/ha")
		hg.GET("/meta/modes", d.HA.Meta)
		hg.GET("/candidates", d.HA.ListCandidateSources)
		hg.POST("/candidates/import-dbdeploy", d.HA.ImportDbDeployMembers)
		hg.GET("/clusters", d.HA.ListClusters)
		hg.POST("/clusters", d.HA.CreateCluster)
		hg.GET("/clusters/:id", gHaCl, d.HA.GetCluster)
		hg.PUT("/clusters/:id", gHaCl, d.HA.UpdateCluster)
		hg.DELETE("/clusters/:id", gHaCl, d.HA.DeleteCluster)
		hg.GET("/clusters/:id/topology", gHaCl, d.HA.Topology)
		hg.GET("/clusters/:id/mha/conf", gHaCl, d.HA.GetMHAConf)
		hg.POST("/clusters/:id/mha/conf/preview", gHaCl, d.HA.PreviewMHAConf)
		hg.PUT("/clusters/:id/mha/conf", gHaCl, d.HA.SaveMHAConf)
		hg.POST("/clusters/:id/mha/conf/push", gHaCl, d.HA.PushMHAConf)
		hg.GET("/clusters/:id/mha/manager/status", gHaCl, d.HA.GetMHAManagerStatus)
		hg.POST("/clusters/:id/mha/manager/:action", gHaCl, d.HA.MHAManagerCtrl)
		hg.POST("/clusters/:id/mha/packages/install", gHaCl, d.HA.InstallMHAPackages)
		hg.GET("/clusters/:id/mha/packages/runs", gHaCl, d.HA.ListMHAInstallRuns)
		hg.GET("/clusters/:id/pgaf/state", gHaCl, d.HA.GetPGAFState)
		hg.GET("/clusters/:id/pgaf/service/status", gHaCl, d.HA.GetPGAFServiceStatus)
		hg.POST("/clusters/:id/pgaf/service/:action", gHaCl, d.HA.PGAFServiceCtrl)
		hg.POST("/clusters/:id/pgaf/packages/install", gHaCl, d.HA.InstallPGAFPackages)
		hg.GET("/clusters/:id/pgaf/packages/runs", gHaCl, d.HA.ListPGAFInstallRuns)
		hg.GET("/clusters/:id/patroni/cluster", gHaCl, d.HA.GetPatroniCluster)
		hg.POST("/clusters/:id/patroni/packages/install", gHaCl, d.HA.InstallPatroniPackages)
		hg.GET("/clusters/:id/patroni/packages/runs", gHaCl, d.HA.ListPatroniInstallRuns)
		hg.GET("/mha/releases", d.HA.ListMHAReleases)
		hg.POST("/mha/releases", d.HA.CreateMHARelease)
		hg.PATCH("/mha/releases/:id", d.HA.UpdateMHARelease)
		hg.DELETE("/mha/releases/:id", d.HA.DeleteMHARelease)
		hg.POST("/mha/releases/:id/artifacts", d.HA.UpsertMHAReleaseArtifact)
		hg.POST("/mha/releases/:id/presign", d.HA.PresignMHARelease)
		hg.GET("/mha/oss", d.HA.GetMHAOSS)
		hg.PUT("/mha/oss", d.HA.PutMHAOSS)
		hg.GET("/pgaf/releases", d.HA.ListPGAFReleases)
		hg.POST("/pgaf/releases", d.HA.CreatePGAFRelease)
		hg.PATCH("/pgaf/releases/:id", d.HA.UpdatePGAFRelease)
		hg.DELETE("/pgaf/releases/:id", d.HA.DeletePGAFRelease)
		hg.POST("/pgaf/releases/:id/artifacts", d.HA.UpsertPGAFReleaseArtifact)
		hg.POST("/pgaf/releases/:id/presign", d.HA.PresignPGAFRelease)
		hg.GET("/pgaf/oss", d.HA.GetPGAFOSS)
		hg.PUT("/pgaf/oss", d.HA.PutPGAFOSS)
		hg.GET("/patroni/releases", d.HA.ListPatroniReleases)
		hg.POST("/patroni/releases", d.HA.CreatePatroniRelease)
		hg.PATCH("/patroni/releases/:id", d.HA.UpdatePatroniRelease)
		hg.DELETE("/patroni/releases/:id", d.HA.DeletePatroniRelease)
		hg.POST("/patroni/releases/:id/artifacts", d.HA.UpsertPatroniReleaseArtifact)
		hg.POST("/patroni/releases/:id/presign", d.HA.PresignPatroniRelease)
		hg.GET("/patroni/oss", d.HA.GetPatroniOSS)
		hg.PUT("/patroni/oss", d.HA.PutPatroniOSS)
		hg.POST("/precheck", d.HA.Precheck)
		hg.POST("/changes", d.HA.SubmitChange)
		hg.GET("/tasks", d.HA.ListTasks)
		hg.GET("/controllers", d.HA.ListControllers)
		hg.POST("/controllers", d.HA.CreateController)
		hg.DELETE("/controllers/:id", gHaCt, d.HA.DeleteController)
		hg.POST("/controllers/:id/env-check", gHaCt, d.HA.EnvCheckController)
	}

	if d.Proxy != nil {
		pg := secured.Group("/proxy")
		pg.GET("/meta", d.Proxy.Meta)
		pg.GET("/services", d.Proxy.List)
		pg.POST("/services/deploy", d.Proxy.Deploy) // 静态路径须先于 /services/:id
		pg.POST("/services", d.Proxy.Create)
		pg.POST("/services/:id/assign-tenant", requireURN(store.URNTenantsImpersonate), gPx, d.Proxy.AssignTenant)
		pg.GET("/services/:id", d.Proxy.Get)
		pg.PUT("/services/:id", d.Proxy.Update)
		pg.DELETE("/services/:id", d.Proxy.Delete)
		pg.POST("/preview", d.Proxy.Preview)
		pg.POST("/services/:id/conf/push", d.Proxy.Push)
		pg.POST("/services/:id/conf/validate", d.Proxy.Validate)
		pg.GET("/services/:id/service/status", d.Proxy.ServiceStatus)
		pg.POST("/services/:id/service/:action", d.Proxy.ServiceCtrl)
		pg.GET("/services/:id/lvs/ipvs", d.Proxy.LVSIpvs)
		pg.GET("/services/:id/lvs/status", d.Proxy.LVSStatus)
		pg.GET("/services/:id/versions", d.Proxy.VersionList)
		pg.GET("/services/:id/versions/:versionId", d.Proxy.VersionDetail)
		pg.POST("/services/:id/versions/:versionId/restore", d.Proxy.VersionRestore)
		pg.GET("/services/:id/deploy/jobs/:jobId", d.Proxy.DeployJob)
	}

	if d.Backup != nil {
		bg := secured.Group("/backup")
		bg.GET("/meta", d.Backup.Meta)
		bg.GET("/policies", d.Backup.ListPolicies)
		bg.POST("/policies", d.Backup.CreatePolicy)
		bg.GET("/policies/:id", d.Backup.GetPolicy)
		bg.PATCH("/policies/:id", d.Backup.UpdatePolicy)
		bg.DELETE("/policies/:id", d.Backup.DeletePolicy)
		bg.POST("/policies/:id/enabled", d.Backup.SetPolicyEnabled)
		bg.POST("/policies/:id/run", d.Backup.RunPolicy)
		bg.GET("/runs", d.Backup.ListRuns)
		bg.GET("/runs/:id", d.Backup.GetRun)
		bg.POST("/restore", d.Backup.CreateRestore)
		bg.GET("/storages", d.Backup.ListStorages)
		bg.POST("/storages", d.Backup.CreateStorage)
		bg.PATCH("/storages/:id", d.Backup.UpdateStorage)
		bg.DELETE("/storages/:id", d.Backup.DeleteStorage)
		bg.GET("/agents", d.Backup.ListAgents)
		bg.POST("/agents", d.Backup.CreateAgent)
		bg.GET("/agents/status", d.Backup.ListAgentStatus)
		bg.POST("/agents/preflight", d.Backup.PreflightAgent)
		bg.POST("/agents/deploy", d.Backup.DeployAgent)
		bg.GET("/agents/deploys", d.Backup.ListDeploys)
		bg.GET("/agents/deploys/:id", d.Backup.GetDeploy)
		bg.DELETE("/agents/:id", d.Backup.DeleteAgent)
		bg.POST("/agents/:id/probe", d.Backup.ProbeAgent)
		bg.POST("/agents/:id/start", d.Backup.StartAgent)
		bg.POST("/agents/:id/stop", d.Backup.StopAgent)
		bg.POST("/agents/:id/redeploy", d.Backup.RedeployAgent)
	}

	if d.Backup != nil {
		// 平台级 Agent API（与 /backup/agents 同源；配置中心主入口）
		pg := secured.Group("/platform/agents")
		pg.GET("", d.Backup.ListAgents)
		pg.POST("", d.Backup.CreateAgent)
		pg.GET("/status", d.Backup.ListAgentStatus)
		pg.POST("/preflight", d.Backup.PreflightAgent)
		pg.POST("/deploy", d.Backup.DeployAgent)
		pg.GET("/deploys", d.Backup.ListDeploys)
		pg.GET("/deploys/:id", d.Backup.GetDeploy)
		if d.SQLExec != nil {
			pg.GET("/sql-executors", d.SQLExec.List)
			pg.GET("/sql-executors/deploys", d.SQLExec.ListDeploys)
			pg.POST("/sql-executors/deploy", d.SQLExec.Deploy)
		}
		pg.DELETE("/:id", d.Backup.DeleteAgent)
		pg.POST("/:id/probe", d.Backup.ProbeAgent)
		pg.POST("/:id/start", d.Backup.StartAgent)
		pg.POST("/:id/stop", d.Backup.StopAgent)
		pg.POST("/:id/redeploy", d.Backup.RedeployAgent)
	}

	if d.K8s != nil {
		kg := secured.Group("/k8s")
		kg.GET("/meta", d.K8s.Meta)
		kg.GET("/summary", d.K8s.MultiClusterSummary)
		kg.GET("/cost", d.K8s.CostEstimate)
		kg.POST("/change-tickets", d.K8s.CreateK8sChangeTicket)
		kg.GET("/db-clusters", d.K8s.ListDBClusters)
		kg.GET("/db-clusters/:dbId", d.K8s.GetDBCluster)
		kg.POST("/db-clusters/:dbId/scale", requireURN(store.URNK8sDBScale), d.K8s.ScaleDBCluster)
		kg.POST("/db-clusters/:dbId/upgrade", requireURN(store.URNK8sDBUpgrade), d.K8s.UpgradeDBCluster)
		kg.POST("/db-clusters/:dbId/switchover", requireURN(store.URNK8sDBSwitchover), d.K8s.SwitchoverDBCluster)
		kg.POST("/db-clusters/:dbId/backup", d.K8s.BackupDBCluster)
		kg.DELETE("/db-clusters/:dbId", requireURN(store.URNK8sDBDelete), d.K8s.DeleteDBCluster)

		kg.GET("/clusters", d.K8s.ListClusters)
		kg.POST("/clusters", requireURN(store.URNK8sClusterCreate), d.K8s.CreateCluster)
		kg.GET("/clusters/:id", d.K8s.GetCluster)
		kg.PATCH("/clusters/:id", requireURN(store.URNK8sClusterEdit), d.K8s.UpdateCluster)
		kg.DELETE("/clusters/:id", requireURN(store.URNK8sClusterDelete), d.K8s.DeleteCluster)
		kg.POST("/clusters/:id/probe", d.K8s.ProbeCluster)
		kg.GET("/clusters/:id/overview", d.K8s.Overview)
		kg.GET("/clusters/:id/audits", d.K8s.ListAudits)
		kg.GET("/clusters/:id/namespaces/bindings", d.K8s.ListNamespaceBindings)
		kg.POST("/clusters/:id/namespaces/assign", requireURN(store.URNK8sClusterAssign), d.K8s.AssignNamespace)
		kg.DELETE("/clusters/:id/namespaces/bindings/:bindId", requireURN(store.URNK8sClusterAssign), d.K8s.DeleteNamespaceBinding)

		kg.GET("/clusters/:id/resources/:gvr", d.K8s.ListResources)
		kg.GET("/clusters/:id/resources/:gvr/:ns/:name", d.K8s.GetResource)
		kg.DELETE("/clusters/:id/resources/:gvr/:ns/:name", requireURN(store.URNK8sWorkloadDelete), d.K8s.DeleteResource)
		kg.GET("/clusters/:id/api-resources", d.K8s.DiscoverResources)
		kg.POST("/clusters/:id/apply", requireURN(store.URNK8sYAMLApply), d.K8s.ApplyYAML)

		kg.GET("/clusters/:id/pods/:ns/:name/log", requireURN(store.URNK8sPodLog), d.K8s.GetPodLog)
		kg.GET("/clusters/:id/pods/:ns/:name/exec", requireURN(store.URNK8sPodExec), d.K8s.ExecPod)

		kg.POST("/clusters/:id/workloads/:kind/:ns/:name/scale", requireURN(store.URNK8sWorkloadScale), d.K8s.ScaleWorkload)
		kg.POST("/clusters/:id/workloads/:kind/:ns/:name/restart", requireURN(store.URNK8sWorkloadRestart), d.K8s.RestartWorkload)
		kg.POST("/clusters/:id/workloads/:kind/:ns/:name/rollback", requireURN(store.URNK8sWorkloadRollback), d.K8s.RollbackWorkload)
		kg.POST("/clusters/:id/namespaces/:ns/quota", requireURN(store.URNK8sQuotaEdit), d.K8s.ApplyQuota)
		kg.POST("/clusters/:id/nodes/:name/cordon", d.K8s.CordonNode)
		kg.POST("/clusters/:id/nodes/:name/drain", requireURN(store.URNK8sNodeDrain), d.K8s.DrainNode)

		kg.POST("/clusters/:id/operators/detect", d.K8s.DetectOperators)
		kg.POST("/clusters/:id/operators/install", d.K8s.InstallOperator)
		kg.GET("/clusters/:id/operators", d.K8s.ListOperators)
		kg.POST("/clusters/:id/db-clusters", requireURN(store.URNK8sDBCreate), d.K8s.CreateDBCluster)
		kg.GET("/clusters/:id/apps", d.K8s.ListHelmApps)
		kg.POST("/clusters/:id/apps/upgrade", requireURN(store.URNK8sYAMLApply), d.K8s.HelmUpgradeInstall)
		kg.POST("/clusters/:id/apps/rollback", requireURN(store.URNK8sWorkloadRollback), d.K8s.HelmRollback)
	}

	if d.K8s != nil {
		// Agent 反向隧道：免 JWT（生产应校验 agent token）
		api.GET("/k8s/agents/:agentId/tunnel", d.K8s.AgentTunnel)
	}

	if d.SrCluster != nil {
		sg := secured.Group("/sr-cluster")
		sg.GET("/meta", d.SrCluster.Meta)
		sg.GET("/agents", d.SrCluster.ListAgents)
		sg.POST("/agents", d.SrCluster.CreateAgent)
		sg.POST("/agents/deploy", d.SrCluster.DeployAgent)
		sg.POST("/agents/deploy-batch", d.SrCluster.DeployAgentBatch)
		sg.GET("/agents/deploys", d.SrCluster.ListAgentDeploys)
		sg.GET("/agents/deploys/:id", gSrAgD, d.SrCluster.GetAgentDeploy)
		sg.GET("/agents/status", d.SrCluster.ListAgentStatus)
		sg.DELETE("/agents/:id", gSrAg, d.SrCluster.DeleteAgent)
		sg.POST("/agents/:id/probe", gSrAg, d.SrCluster.ProbeAgent)
		sg.POST("/agents/:id/start", gSrAg, d.SrCluster.StartAgent)
		sg.POST("/agents/:id/stop", gSrAg, d.SrCluster.StopAgent)
		sg.GET("/oss", d.SrCluster.GetOSS)
		sg.PUT("/oss", d.SrCluster.PutOSS)
		sg.GET("/layout-defaults", d.SrCluster.GetLayoutDefaults)
		sg.PUT("/layout-defaults", d.SrCluster.PutLayoutDefaults)
		sg.POST("/oss/test", d.SrCluster.TestOSS)
		sg.GET("/releases", d.SrCluster.ListReleases)
		sg.POST("/releases", d.SrCluster.CreateRelease)
		sg.PATCH("/releases/:id", d.SrCluster.UpdateRelease)
		sg.DELETE("/releases/:id", d.SrCluster.DeleteRelease)
		sg.POST("/releases/:id/artifacts", d.SrCluster.UpsertArtifact)
		sg.POST("/releases/:id/artifacts/upload", d.SrCluster.UploadArtifact)
		sg.POST("/releases/:id/artifacts/sync-from-oss", d.SrCluster.SyncArtifactsFromOSS)
		sg.GET("/env-policies", d.SrCluster.ListEnvPolicies)
		sg.PUT("/env-policies", d.SrCluster.UpsertEnvPolicy)
		sg.GET("/conf-templates", d.SrCluster.ListConfTemplates)
		sg.PUT("/conf-templates", d.SrCluster.UpsertConfTemplate)
		sg.POST("/conf-templates/reset", d.SrCluster.ResetConfTemplates)
		sg.GET("/clusters", d.SrCluster.ListClusters)
		sg.POST("/probe-fe-roles", d.SrCluster.ProbeFERoles)
		sg.POST("/probe-layout", d.SrCluster.ProbeLayout)
		sg.POST("/clusters", d.SrCluster.CreateCluster)
		sg.POST("/clusters/:id/assign-tenant", requireURN(store.URNTenantsImpersonate), gSrCl, d.SrCluster.AssignTenant)
		sg.POST("/tickets", d.SrCluster.CreateTicket)
		sg.GET("/clusters/:id", gSrCl, d.SrCluster.GetCluster)
		sg.GET("/clusters/:id/live", gSrCl, d.SrCluster.GetClusterLive)
		sg.POST("/clusters/:id/sync-registry", gSrCl, d.SrCluster.SyncRegistry)
		sg.POST("/clusters/:id/probe-layout", gSrCl, d.SrCluster.ProbeClusterLayout)
		sg.PATCH("/clusters/:id", gSrCl, d.SrCluster.UpdateCluster)
		sg.DELETE("/clusters/:id", gSrCl, d.SrCluster.DeleteCluster)
		sg.GET("/clusters/:id/nodes", gSrCl, d.SrCluster.ListNodes)
		sg.POST("/clusters/:id/nodes", gSrCl, d.SrCluster.CreateNode)
		sg.POST("/clusters/:id/nodes/precheck", gSrCl, d.SrCluster.PrecheckAddNode)
		sg.DELETE("/clusters/:id/nodes/:nodeId", gSrCl, d.SrCluster.DeleteNode)
		sg.POST("/clusters/:id/init", gSrCl, d.SrCluster.StartInit)
		sg.POST("/clusters/:id/reinit", gSrCl, d.SrCluster.StartReinit)
		sg.POST("/clusters/:id/mark-ready", gSrCl, d.SrCluster.MarkReady)
		sg.POST("/clusters/:id/upgrade", gSrCl, d.SrCluster.StartUpgrade)
		sg.POST("/clusters/:id/upgrade/precheck", gSrCl, d.SrCluster.PrecheckUpgrade)
		sg.GET("/clusters/:id/configs", gSrCl, d.SrCluster.GetConfigs)
		sg.PUT("/clusters/:id/configs", gSrCl, d.SrCluster.PutConfigs)
		sg.GET("/clusters/:id/configs/preview", gSrCl, d.SrCluster.PreviewConfigs)
		sg.PUT("/clusters/:id/nodes/:nodeId/config", gSrCl, d.SrCluster.PutNodeConfig)
		sg.POST("/clusters/:id/config-apply", gSrCl, d.SrCluster.StartConfigApply)
		sg.GET("/clusters/:id/nodes/:nodeId/conf-remote", gSrCl, d.SrCluster.GetRemoteConf)
		sg.POST("/clusters/:id/nodes/:nodeId/conf-diff", gSrCl, d.SrCluster.ConfDiff)
		sg.GET("/clusters/:id/nodes/:nodeId/conf-history", gSrCl, d.SrCluster.ConfHistory)
		sg.GET("/clusters/:id/conf-revisions/:rid", gSrCl, d.SrCluster.GetConfRevision)
		sg.GET("/clusters/:id/conf-revision-diff", gSrCl, d.SrCluster.ConfRevisionDiff)
		sg.POST("/clusters/:id/conf-patch", gSrCl, d.SrCluster.ConfPatch)
		sg.POST("/clusters/:id/conf-rollback", gSrCl, d.SrCluster.StartConfigRollback)
		sg.POST("/clusters/:id/nodes/:nodeId/add", gSrCl, d.SrCluster.StartAddNode)
		sg.POST("/clusters/:id/nodes/:nodeId/restart", gSrCl, d.SrCluster.StartRestartNode)
		sg.POST("/clusters/:id/nodes/:nodeId/start", gSrCl, d.SrCluster.StartNodeStart)
		sg.POST("/clusters/:id/nodes/:nodeId/stop", gSrCl, d.SrCluster.StartNodeStop)
		sg.POST("/clusters/:id/nodes/:nodeId/remove", gSrCl, d.SrCluster.StartRemoveNode)
		sg.GET("/changes", d.SrCluster.ListChanges)
		sg.GET("/changes/:id", gSrCh, d.SrCluster.GetChange)
		sg.POST("/changes/:id/cancel", gSrCh, d.SrCluster.CancelChange)
		sg.POST("/changes/:id/retry", gSrCh, d.SrCluster.RetryChange)
		sg.POST("/changes/:id/rollback-node", gSrCh, d.SrCluster.RollbackUpgradeNode)
	}
	if d.DbDeploy != nil {
		dg := secured.Group("/db-deploy")
		dg.GET("/meta", d.DbDeploy.Meta)
		dg.GET("/engines", d.DbDeploy.ListEngines)
		dg.GET("/releases", d.DbDeploy.ListReleases)
		dg.POST("/releases", d.DbDeploy.UpsertRelease)
		dg.PUT("/releases/:id", d.DbDeploy.UpsertRelease)
		dg.DELETE("/releases/:id", d.DbDeploy.DeleteRelease)
		dg.POST("/releases/:id/presign", d.DbDeploy.PresignRelease)
		dg.POST("/releases/:id/upload", d.DbDeploy.UploadRelease)
		dg.POST("/releases/:id/sync-from-oss", d.DbDeploy.SyncReleaseFromOSS)
		dg.POST("/instance-probe", d.DbDeploy.ProbeInstance)
		dg.GET("/conf-templates", d.DbDeploy.ListConfTemplates)
		dg.PUT("/conf-templates", d.DbDeploy.PutConfTemplate)
		dg.POST("/conf-templates/reset", d.DbDeploy.ResetConfTemplate)
		dg.GET("/clusters/:id/conf", gDdCl, d.DbDeploy.GetClusterConf)
		dg.PUT("/clusters/:id/conf", gDdCl, d.DbDeploy.PutClusterConf)
		dg.GET("/clusters/:id/conf/preview", gDdCl, d.DbDeploy.PreviewClusterConf)
		dg.POST("/clusters/:id/conf/preview", gDdCl, d.DbDeploy.PreviewClusterConf)
		dg.GET("/clusters", d.DbDeploy.ListClusters)
		dg.POST("/clusters", d.DbDeploy.UpsertCluster)
		dg.POST("/clusters/:id/assign-tenant", requireURN(store.URNTenantsImpersonate), gDdCl, d.DbDeploy.AssignTenant)
		dg.POST("/tickets", d.DbDeploy.CreateTicket)
		dg.GET("/clusters/:id", gDdCl, d.DbDeploy.GetCluster)
		dg.PUT("/clusters/:id", gDdCl, d.DbDeploy.UpsertCluster)
		dg.DELETE("/clusters/:id", gDdCl, d.DbDeploy.DeleteCluster)
		dg.POST("/clusters/:id/deploy", gDdCl, d.DbDeploy.StartDeploy)
		dg.POST("/clusters/:id/ha-init", gDdCl, d.DbDeploy.StartHaInit)
		dg.POST("/clusters/:id/ha/preflight", gDdCl, d.DbDeploy.HAPreflight)
		dg.GET("/clusters/:id/nodes", gDdCl, d.DbDeploy.ListNodes)
		dg.POST("/clusters/:id/nodes", gDdCl, d.DbDeploy.UpsertNode)
		dg.POST("/clusters/:id/nodes/precheck", gDdCl, d.DbDeploy.PrecheckAddNode)
		dg.DELETE("/clusters/:id/nodes/:nodeId", gDdCl, d.DbDeploy.DeleteNode)
		dg.POST("/clusters/:id/nodes/:nodeId/start", gDdCl, d.DbDeploy.StartNode)
		dg.POST("/clusters/:id/nodes/:nodeId/stop", gDdCl, d.DbDeploy.StopNode)
		dg.POST("/clusters/:id/nodes/:nodeId/restart", gDdCl, d.DbDeploy.RestartNode)
		dg.POST("/clusters/:id/nodes/:nodeId/add", gDdCl, d.DbDeploy.AddNode)
		dg.POST("/clusters/:id/nodes/:nodeId/remove", gDdCl, d.DbDeploy.RemoveNode)
		dg.GET("/changes", d.DbDeploy.ListChanges)
		dg.GET("/changes/:id", gDdCh, d.DbDeploy.GetChange)
		dg.POST("/changes/:id/retry", gDdCh, d.DbDeploy.RetryChange)
		dg.POST("/changes/:id/cancel", gDdCh, d.DbDeploy.CancelChange)
		dg.GET("/agents", d.DbDeploy.ListAgents)
		dg.POST("/agents", d.DbDeploy.UpsertAgent)
		dg.DELETE("/agents/:id", gDdAg, d.DbDeploy.DeleteAgent)
		dg.POST("/agents/deploy", d.DbDeploy.DeployAgent)
		dg.GET("/agents/deploys", d.DbDeploy.ListAgentDeploys)
		dg.GET("/agents/deploys/:id", d.DbDeploy.GetAgentDeploy)
		dg.GET("/oss", d.DbDeploy.GetOSS)
		dg.PUT("/oss", d.DbDeploy.PutOSS)
	}

	secured.GET("/sql-insight", d.Insight.List)
	secured.POST("/sql-insight", d.Insight.Create)
	secured.GET("/pg-activity-snapshots", d.Insight.ListSnapshots)
	secured.GET("/pg-activity-snapshots/stats", d.Insight.SnapshotStats)
	secured.GET("/pg-activity-snapshots/summary", d.Insight.SnapshotSummary)
	secured.POST("/pg-activity-snapshots/collect", requireURN(store.URNSqlInsightCollect), d.Insight.CollectSnapshots)
	secured.GET("/pg-sessions/activities", d.Session.Activities)
	secured.GET("/pg-sessions/locks", d.Session.Locks)
	secured.POST("/pg-sessions/:pid/cancel", d.Session.Cancel)
	secured.POST("/pg-sessions/:pid/terminate", d.Session.Terminate)
	secured.GET("/mysql-sessions/processlist", d.Session.MySQLProcesslist)
	secured.POST("/mysql-sessions/:id/kill", d.Session.MySQLKill)
	secured.GET("/mysql-sessions/digests", d.Session.MySQLDigests)

	if d.PgSpace != nil {
		ps := secured.Group("/pg-space")
		ps.GET("/instances", d.PgSpace.Instances)
		ps.GET("/realtime", d.PgSpace.Realtime)
		ps.GET("/databases", d.PgSpace.Databases)
		ps.GET("/snapshots", d.PgSpace.Snapshots)
		ps.GET("/trend", d.PgSpace.Trend)
		ps.GET("/summary", d.PgSpace.Summary)
		ps.GET("/advice", d.PgSpace.Advice)
		ps.GET("/advice/summary", d.PgSpace.AdviceSummary)
		ps.POST("/collect", requireURN(store.URNSqlInsightSpaceCollect), d.PgSpace.Collect)
		ps.GET("/collect/status", d.PgSpace.CollectStatus)
		ps.GET("/collect/jobs", d.PgSpace.CollectJobs)
		ps.POST("/collect/pause", requireURN(store.URNSqlInsightSpaceCollect), d.PgSpace.CollectPause)
		ps.POST("/collect/resume", requireURN(store.URNSqlInsightSpaceCollect), d.PgSpace.CollectResume)
		ps.POST("/collect/cancel", requireURN(store.URNSqlInsightSpaceCollect), d.PgSpace.CollectCancel)
	}
	if d.RedisRDB != nil {
		rr := secured.Group("/redis-rdb")
		rr.GET("/instances", d.RedisRDB.Instances)
		rr.GET("/agents", d.RedisRDB.Agents)
		rr.GET("/online", d.RedisRDB.Online)
		rr.POST("/upload", d.RedisRDB.Upload)
		rr.POST("/fetch", d.RedisRDB.Fetch)
		rr.GET("/jobs", d.RedisRDB.Jobs)
		rr.GET("/jobs/:id", d.RedisRDB.Job)
		rr.GET("/jobs/:id/report", d.RedisRDB.Report)
		rr.DELETE("/jobs/:id", d.RedisRDB.DeleteJob)
	}

	if d.AWR != nil {
		ar := secured.Group("/awr-reports")
		ar.GET("", d.AWR.List)
		ar.GET("/detail", d.AWR.Detail)
		ar.GET("/export", d.AWR.Export)
		ar.GET("/summary", d.AWR.Summary)
		ar.GET("/summary/export", d.AWR.SummaryExport)
		ar.POST("/generate", requireURN(store.URNSqlInsightAwrGenerate), d.AWR.Generate)
		ar.GET("/generate/status", d.AWR.GenerateStatus)
		ar.GET("/generate/jobs", d.AWR.GenerateJobs)
		ar.POST("/generate/pause", requireURN(store.URNSqlInsightAwrGenerate), d.AWR.GeneratePause)
		ar.POST("/generate/resume", requireURN(store.URNSqlInsightAwrGenerate), d.AWR.GenerateResume)
		ar.POST("/generate/cancel", requireURN(store.URNSqlInsightAwrGenerate), d.AWR.GenerateCancel)
	}

	if d.SqlInsightAnalysis != nil {
		si := secured.Group("/sql-insight/analysis")
		si.GET("/catalog", d.SqlInsightAnalysis.Catalog)
		si.GET("/metrics", d.SqlInsightAnalysis.ListMetrics)
		si.POST("/metrics", d.SqlInsightAnalysis.CreateMetric)
		si.PUT("/metrics/:id", d.SqlInsightAnalysis.UpdateMetric)
		si.DELETE("/metrics/:id", d.SqlInsightAnalysis.DeleteMetric)
		si.GET("/rules", d.SqlInsightAnalysis.ListRules)
		si.POST("/rules", d.SqlInsightAnalysis.CreateRule)
		si.PUT("/rules/:id", d.SqlInsightAnalysis.UpdateRule)
		si.DELETE("/rules/:id", d.SqlInsightAnalysis.DeleteRule)
		si.GET("/runs", d.SqlInsightAnalysis.ListRuns)
		si.POST("/runs", requireURN(store.URNSqlInsightAnalyze), d.SqlInsightAnalysis.CreateRun)
		si.GET("/runs/:id", d.SqlInsightAnalysis.GetRun)
		si.GET("/findings", d.SqlInsightAnalysis.ListFindings)
		si.POST("/findings/:id/feedback", d.SqlInsightAnalysis.Feedback)
		si.GET("/summary", d.SqlInsightAnalysis.Summary)
	}

	if d.DbMonitorProxy != nil {
		secured.Any("/db-monitor", d.DbMonitorProxy)
		secured.Any("/db-monitor/*path", d.DbMonitorProxy)
	} else if d.DbMonitor != nil {
		dm := secured.Group("/db-monitor")
		dm.GET("/catalog", d.DbMonitor.Catalog)
		dm.GET("/instances", d.DbMonitor.Instances)
		dm.GET("/clusters", d.DbMonitor.Clusters)
		dm.GET("/overview", d.DbMonitor.Overview)
		dm.GET("/panels", d.DbMonitor.Panels)
		dm.POST("/query", d.DbMonitor.Query)
	}

	secured.GET("/multilang-template/tasks", d.Multilang.List)
	secured.POST("/multilang-template/tasks", d.Multilang.Create)

	secured.GET("/dsadmin/status", d.External.Dolphin)
	secured.GET("/feadmin/status", d.External.StarRocks)

	if d.MonitorTarget != nil {
		mt := secured.Group("/monitor-targets")
		mt.GET("", d.MonitorTarget.List)
		mt.POST("", d.MonitorTarget.Create)
		mt.GET("/preview", d.MonitorTarget.Preview)
		mt.GET("/:id", d.MonitorTarget.Get)
		mt.PUT("/:id", d.MonitorTarget.Update)
		mt.DELETE("/:id", d.MonitorTarget.Delete)
	}
	if d.FeAdmin != nil {
		fe := secured.Group("/feadmin")
		fe.GET("", d.FeAdmin.List)
		fe.POST("", d.FeAdmin.Create)
		fe.POST("/sync-from-monitor", d.FeAdmin.SyncFromMonitor)
		fe.POST("/resolve-credential", d.FeAdmin.ResolveCredential)
		fe.GET("/:id", d.FeAdmin.Get)
		fe.PUT("/:id", d.FeAdmin.Update)
		fe.DELETE("/:id", d.FeAdmin.Delete)
	}
	if d.DsAdmin != nil {
		ds := secured.Group("/dsadmin")
		ds.GET("", d.DsAdmin.List)
		ds.POST("", d.DsAdmin.Create)
		ds.GET("/credential", d.DsAdmin.Credential)
		ds.GET("/warm", d.DsAdmin.Warm)
		ds.GET("/:id", d.DsAdmin.Get)
		ds.PUT("/:id", d.DsAdmin.Update)
		ds.DELETE("/:id", d.DsAdmin.Delete)
		secured.GET("/ds-console/:env", d.DsAdmin.ConsoleBootstrap)
		secured.Any("/ds-proxy/:env/*filepath", d.DsAdmin.Proxy)
	}
	if d.OSSSettings != nil {
		oss := secured.Group("/oss-settings")
		oss.GET("", d.OSSSettings.List)
		oss.POST("", d.OSSSettings.Create)
		oss.POST("/test", d.OSSSettings.Test)
		oss.GET("/:id", d.OSSSettings.Get)
		oss.PUT("/:id", d.OSSSettings.Update)
		oss.DELETE("/:id", d.OSSSettings.Delete)
	}
	if d.OSSUpload != nil {
		up := secured.Group("/oss-uploads")
		up.GET("", d.OSSUpload.List)
		up.POST("", d.OSSUpload.Create)
		up.GET("/:id", d.OSSUpload.Get)
		up.PUT("/:id/parts/:n", d.OSSUpload.PutPart)
		up.POST("/:id/complete", d.OSSUpload.Complete)
		up.POST("/:id/abort", d.OSSUpload.Abort)
	}
	if d.StarRocksWeb != nil {
		secured.POST("/starrocks-web/page", d.StarRocksWeb.FetchPage)
		secured.Any("/starrocks-fe-proxy/:id/*filepath", d.StarRocksWeb.FeProxy)
		secured.Any("/starrocks-be-proxy/:host/*filepath", d.StarRocksWeb.BeProxy)
		// 本地 vendored StarRocks css/js（无需登录；代理 HTML 改写到此路径）
		handler.MountStarRocksWebroot(e)
	}

	if d.Static != "" {
		serveSPA(e, d.Static)
	}
}

// spaAssetExt：带这些扩展名的请求在文件缺失时返回 404，禁止回落 index.html
// （否则浏览器会把 HTML 当 JS/CSS 解析，表现为白屏）。
var spaAssetExt = map[string]struct{}{
	".js": {}, ".mjs": {}, ".css": {}, ".map": {},
	".svg": {}, ".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".ico": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".eot": {},
	".json": {}, ".wasm": {}, ".txt": {},
}

func isSPAAssetPath(urlPath string) bool {
	ext := strings.ToLower(path.Ext(urlPath))
	_, ok := spaAssetExt[ext]
	return ok
}

// safeSPAJoin 将 URL 路径解析到 root 下的本地文件；拒绝跳出 root。
// rel 为空表示请求根路径（应回落 index）。
func safeSPAJoin(root, urlPath string) (full string, ok bool) {
	cleaned := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	rel := strings.TrimPrefix(cleaned, "/")
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	rootClean := filepath.Clean(root)
	if rel == "" || rel == "." {
		return rootClean, true
	}
	full = filepath.Join(rootClean, filepath.FromSlash(rel))
	fullClean := filepath.Clean(full)
	sep := string(os.PathSeparator)
	if fullClean != rootClean && !strings.HasPrefix(fullClean, rootClean+sep) {
		return "", false
	}
	return fullClean, true
}

func serveSPA(e *gin.Engine, root string) {
	root = filepath.Clean(root)
	index := filepath.Join(root, "index.html")
	e.NoRoute(func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		if strings.HasPrefix(urlPath, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"message": "not found"})
			return
		}
		full, ok := safeSPAJoin(root, urlPath)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		if full != root {
			if info, err := os.Stat(full); err == nil && !info.IsDir() {
				if strings.HasPrefix(urlPath, "/assets/") {
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				} else if strings.EqualFold(filepath.Base(full), "index.html") {
					c.Header("Cache-Control", "no-cache")
				}
				c.File(full)
				return
			}
			if isSPAAssetPath(urlPath) {
				c.Status(http.StatusNotFound)
				return
			}
		}
		if _, err := os.Stat(index); err == nil {
			c.Header("Cache-Control", "no-cache")
			c.File(index)
			return
		}
		c.Status(http.StatusNotFound)
	})
}
