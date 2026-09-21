package router

import (
	"net/http"
	"os"
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
	Cfg       *config.Config
	Auth      *handler.AuthHandler
	Inst      *handler.InstanceHandler
	DB        *handler.DatabaseHandler
	Region    *handler.RegionHandler
	Env       *handler.EnvironmentHandler
	Connector *handler.ConnectorHandler
	Args      *handler.GlobalArgHandler
	Ticket    *handler.TicketHandler
	CreateDB  *handler.CreateDBHandler
	Account   *handler.AccountHandler
	Review    *handler.ReviewRuleHandler
	CDC       *handler.CDCHandler
	Slot      *handler.SlotHandler
	Migration *handler.MigrationHandler
	Flashback *handler.FlashbackHandler
	Archive   *handler.ArchiveHandler
	Kafka     *handler.KafkaHandler
	Redis     *handler.RedisHandler // Redis 工单；为 nil 时不注册路由
	Insight   *handler.InsightHandler
	Session   *handler.SessionHandler
	Multilang *handler.MultilangHandler
	External  *handler.ExternalHandler
	DCL       *handler.DCLHandler
	RBAC      *handler.RBACHandler
	Ldap      *handler.LdapHandler
	Sso       *handler.SsoHandler
	SMTP      *handler.SMTPHandler
	Notify    *handler.NotifyHandler
	Terminal  *terminal.Handler
	Static    string
}

func Init(e *gin.Engine, d Deps) {
	e.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
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

	auth := middleware.Auth(d.Cfg.App.JWTSecret, d.Auth.Store,
		"/healthz", "/readyz",
		"/api/v1/auth/login", "/api/v1/auth/captcha", "/api/v1/info",
		"/api/v1/auth/sso/",
		"/api/v1/auth/password/forgot", "/api/v1/auth/password/reset",
	)
	secured := e.Group("/api/v1")
	secured.Use(auth)
	secured.Use(middleware.IdleSession(d.Auth.Store))

	requireURN := func(urn string) gin.HandlerFunc {
		return middleware.RequireURN(d.RBAC.Store, urn)
	}

	secured.GET("/auth/me", d.Auth.Me)
	secured.POST("/auth/logout", d.Auth.Logout)
	secured.PUT("/auth/password", d.Auth.ChangePassword)
	secured.PUT("/auth/profile", d.Auth.UpdateProfile)
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
	}

	secured.GET("/menus/mine", d.RBAC.MineMenus)
	secured.GET("/menus/tree", d.RBAC.MenuTree)
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
	secured.PUT("/users/:id", requireURN(store.URNUsersEdit), d.RBAC.UpdateUser)
	secured.PUT("/users/:id/password", requireURN(store.URNUsersEdit), d.RBAC.ResetUserPassword)
	secured.PUT("/users/:id/disable", requireURN(store.URNUsersEdit), d.RBAC.DisableUser)
	secured.PUT("/users/:id/enable", requireURN(store.URNUsersEdit), d.RBAC.EnableUser)
	secured.PUT("/users/:id/unlock", requireURN(store.URNUsersEdit), d.RBAC.UnlockUser)
	secured.DELETE("/users/:id", requireURN(store.URNUsersDelete), d.RBAC.DeleteUser)

	secured.GET("/instances", d.Inst.List)
	secured.POST("/instances", requireURN(store.URNInstancesCreate), d.Inst.Create)
	secured.POST("/instances/test-connection", d.Inst.TestConnection)
	secured.GET("/instances/:id", d.Inst.Get)
	secured.PUT("/instances/:id", requireURN(store.URNInstancesEdit), d.Inst.Update)
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
	secured.GET("/databases/:id/tables", d.DB.ListTables)
	secured.PUT("/databases/:id", requireURN(store.URNDatabasesEdit), d.DB.Update)
	secured.DELETE("/databases/:id", requireURN(store.URNDatabasesDelete), d.DB.Delete)
	secured.POST("/query", d.DB.Query)

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

	secured.GET("/connectors", d.Connector.List)
	secured.POST("/connectors", requireURN(store.URNConnectorsCreate), d.Connector.Create)
	secured.GET("/connectors/:id", d.Connector.Get)
	secured.PUT("/connectors/:id", requireURN(store.URNConnectorsEdit), d.Connector.Update)
	secured.DELETE("/connectors/:id", requireURN(store.URNConnectorsDelete), d.Connector.Delete)

	secured.GET("/global-args", d.Args.List)
	secured.POST("/global-args", requireURN(store.URNArgsCreate), d.Args.Create)
	secured.PUT("/global-args", requireURN(store.URNArgsEdit), d.Args.Update)
	secured.DELETE("/global-args/:key", requireURN(store.URNArgsDelete), d.Args.Delete)

	t := secured.Group("/tickets")
	t.GET("/targets", d.Ticket.GetTargets)
	t.GET("/db-types", d.Ticket.ListDBTypes)
	t.GET("/instances", d.Ticket.ListInstances)
	t.GET("/instances/:instance_id/databases", d.Ticket.ListInstanceDatabases)
	t.GET("/target-approvers", d.Ticket.GetTargetApprovers)
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
	t.POST("/:id/execute", d.Ticket.Execute)
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
	cdc.GET("/tasks/:id/connector/versions", d.CDC.ConnectorVersionList)
	cdc.GET("/tasks/:id/connector/versions/:versionId", d.CDC.ConnectorVersionDetail)
	cdc.POST("/tasks/:id/connector/versions/:versionId/restore/preview", d.CDC.ConnectorVersionRestorePreview)
	cdc.POST("/tasks/:id/connector/versions/:versionId/restore", d.CDC.ConnectorVersionRestore)
	cdc.DELETE("/tasks/:id", d.CDC.Delete)
	cdc.POST("/tasks/:id/pause", d.CDC.Action("paused", "none"))
	cdc.POST("/tasks/:id/resume", d.CDC.Action("running", "approved"))
	cdc.POST("/tasks/:id/restart", d.CDC.Action("running", "none"))
	cdc.POST("/tasks/:id/retry", d.CDC.Action("running", "none"))

	secured.GET("/sync-slots", d.Slot.List)
	secured.POST("/sync-slots/collect", d.Slot.Collect)
	secured.POST("/sync-slots/restart", d.Slot.Restart)
	secured.POST("/sync-slots/delete", d.Slot.Delete)

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
	fb.GET("/tasks/:id", d.Flashback.Get)
	fb.GET("/tasks/:id/sql", d.Flashback.SQLs)
	fb.GET("/tasks/:id/logs", d.Flashback.Logs)
	fb.POST("/tasks/:id/submit-dml-ticket", d.Flashback.SubmitDML)

	arc := secured.Group("/archive")
	arc.GET("/tasks", d.Archive.List)
	arc.POST("/tasks", d.Archive.Create)
	arc.GET("/tasks/:id", d.Archive.Get)
	arc.PATCH("/tasks/:id", d.Archive.Update)
	arc.GET("/stats", d.Archive.Stats)
	arc.GET("/meta", d.Archive.Meta)
	arc.GET("/runs", d.Archive.ListRuns)
	arc.POST("/check", d.Archive.Check)
	arc.GET("/settings", d.Archive.GetSettings)
	arc.PATCH("/settings", d.Archive.PutSettings)
	arc.POST("/settings/:kind/test", d.Archive.TestSettings)
	arc.POST("/tasks/:id/run", d.Archive.Run)
	arc.POST("/tasks/:id/pause", d.Archive.Pause)
	arc.GET("/tasks/:id/runs", d.Archive.ListRuns)
	arc.POST("/tasks/:id/runs/:runId/stop", d.Archive.StopRun)

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

	secured.GET("/sql-insight", d.Insight.List)
	secured.POST("/sql-insight", d.Insight.Create)
	secured.GET("/pg-activity-snapshots", d.Insight.ListSnapshots)
	secured.GET("/pg-activity-snapshots/stats", d.Insight.SnapshotStats)
	secured.POST("/pg-activity-snapshots/collect", d.Insight.CollectSnapshots)
	secured.GET("/pg-sessions/activities", d.Session.Activities)
	secured.GET("/pg-sessions/locks", d.Session.Locks)
	secured.POST("/pg-sessions/:pid/cancel", d.Session.Cancel)
	secured.POST("/pg-sessions/:pid/terminate", d.Session.Terminate)

	secured.GET("/multilang-template/tasks", d.Multilang.List)
	secured.POST("/multilang-template/tasks", d.Multilang.Create)

	secured.GET("/dsadmin/status", d.External.Dolphin)
	secured.GET("/feadmin/status", d.External.StarRocks)

	if d.Static != "" {
		serveSPA(e, d.Static)
	}
}

func serveSPA(e *gin.Engine, root string) {
	e.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"message": "not found"})
			return
		}
		p := filepath.Join(root, filepath.Clean(c.Request.URL.Path))
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			c.File(p)
			return
		}
		index := filepath.Join(root, "index.html")
		if _, err := os.Stat(index); err == nil {
			c.File(index)
			return
		}
		c.Status(http.StatusNotFound)
	})
}
