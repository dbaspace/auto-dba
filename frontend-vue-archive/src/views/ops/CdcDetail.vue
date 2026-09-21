<template>
  <div class="cdc-shell cdc-detail">
    <div class="cdc-back">
      <el-button text type="primary" @click="$router.push('/ops/cdc')">← 返回列表</el-button>
    </div>
    <div class="cdc-tag-row">
      <el-tag v-if="task.task_type" size="small" :type="typeTag(task.task_type)">{{ typeText(task.task_type) }}</el-tag>
      <el-tag v-if="task.status && !pending" size="small" :type="statusTag(task.status)">{{ statusText(task.status) }}</el-tag>
      <el-tag v-if="extra.env" size="small" :type="envTag(extra.env)">{{ extra.env }}</el-tag>
      <el-tag v-if="task.approval_status" size="small" :type="task.approval_status === 'approved' ? 'success' : (task.approval_status === 'rejected' ? 'danger' : 'warning')">{{ approvalText(task.approval_status) }}</el-tag>
      <span v-if="task.parent_id" class="muted">关联：<button type="button" class="name-link" @click="$router.push('/ops/cdc/' + task.parent_id)">{{ task.parent_id }}</button></span>
      <div class="ops">
        <el-button v-if="task.status === 'paused' && !pending" type="primary" @click="act('resume')">恢复</el-button>
        <el-button v-if="task.status === 'running' && !pending" @click="act('pause')">暂停</el-button>
        <el-button v-if="task.status === 'running' && !pending" @click="act('restart')">重启</el-button>
        <el-button v-if="(task.status === 'error' || task.status === 'failed') && !pending" type="primary" @click="act('retry')">重试</el-button>
      </div>
    </div>

    <el-alert
      v-if="pending"
      type="warning"
      show-icon
      :closable="false"
      class="mb"
      title="任务待审批"
    >
      <template #default>
        审批通过后才会开始编排同步链路。
        <el-button v-if="task.issue_id" link type="primary" @click="$router.push('/ops/tickets/' + task.issue_id)">查看工单 #{{ task.issue_id }}</el-button>
      </template>
    </el-alert>
    <el-alert
      v-if="errorStep || extra.error_message"
      type="error"
      show-icon
      :closable="false"
      class="mb"
      :title="errorStep ? `任务异常（${errorStep}）` : '任务异常'"
      :description="extra.error_message || '可在操作列点击「重试」从该失败步骤起点续跑。'"
    />
    <el-alert
      v-if="extra.auto_create_table === false && !task.target_instance_id"
      type="info"
      show-icon
      :closable="false"
      class="mb"
      title="手动 DDL 模式，未配置目标端"
      description="该任务关闭了自动建表且未配置目标端 StarRocks，系统不会自动创建目标表与 Routine Load；请在目标库手动建表后自行配置导入。全链路状态中 StarRocks / Routine Load 节点的未就绪属预期。"
    />
    <el-alert
      v-else-if="extra.auto_create_table === false"
      type="info"
      show-icon
      :closable="false"
      class="mb"
      title="手动 DDL：本任务不会自动建表 / Routine Load"
    />

    <div v-if="showApprovalBar" class="cdc-approval-bar" :class="isCreatorOnly ? 'is-creator' : 'is-approver'">
      <div>
        <div class="bar-title">{{ approvalBarTitle }}</div>
        <p class="bar-hint">{{ approvalBarHint }}</p>
      </div>
      <div class="ops">
        <el-button v-if="!isCreatorOnly" type="primary" :loading="approving" @click="openApprove">通过</el-button>
        <el-button type="danger" :loading="approving" @click="openReject">{{ isCreatorOnly ? '撤回' : '驳回' }}</el-button>
      </div>
    </div>

    <el-alert
      v-if="showSchemaBanner"
      class="cdc-schema-banner"
      :type="schemaBannerType"
      show-icon
      :closable="false"
      :title="schemaBannerTitle"
    >
      <template #default>
        <div v-if="schemaAlerts?.message && schemaAlerts.state !== 'OK'">{{ schemaAlerts.message }}</div>
        <div v-if="schemaAlerts?.drift_table_count" class="muted mt">共 {{ schemaAlerts.drift_table_count }} 张目标表存在结构差异</div>
        <div v-if="schemaAlerts?.checked_at" class="muted">上次检查：{{ schemaAlerts.checked_at }}</div>
      </template>
      <template #action>
        <div class="cdc-schema-actions">
          <el-button size="small" :loading="schemaScanning" @click="loadSchemaAlerts(true)">立即检查</el-button>
          <el-button size="small" @click="tab = 'tables'">去表映射</el-button>
          <el-button size="small" type="primary" plain @click="$router.push({ path: '/ops/cdc/create', query: { type: 'alter_schema', parent_id: task.id } })">源表加列向导</el-button>
        </div>
      </template>
    </el-alert>

    <el-tabs v-model="tab" class="cdc-tabs">
      <el-tab-pane label="基本信息" name="overview" />
      <el-tab-pane label="全链路状态" name="pipeline" />
      <el-tab-pane name="tables">
        <template #label>
          <el-badge :value="tableDriftCount || undefined" :hidden="!tableDriftCount" :is-dot="schemaHasDrift && !tableDriftCount">
            表映射明细
          </el-badge>
        </template>
      </el-tab-pane>
      <el-tab-pane v-if="isInit" label="Publication 表" name="publication" />
      <el-tab-pane label="Debezium 配置" name="debezium" />
      <el-tab-pane label="操作日志" name="logs" />
    </el-tabs>
    <div v-show="tab === 'overview'" v-loading="loading" class="overview-stack">
      <el-card shadow="never" class="cdc-card small">
        <template #header><span class="card-title">源端 / 目标端</span></template>
        <el-descriptions :column="descCols" size="small">
          <el-descriptions-item label="源端 PostgreSQL">{{ instLabel(source) }}</el-descriptions-item>
          <el-descriptions-item label="目标端 StarRocks">{{ task.target_instance_id ? instLabel(target) : '未配置' }}</el-descriptions-item>
          <el-descriptions-item label="PG 数据库">{{ sourceScopeLabel }}</el-descriptions-item>
          <el-descriptions-item label="目标数据库">{{ task.target_db || '未配置' }}</el-descriptions-item>
          <el-descriptions-item label="Schema">{{ extra.source_schema || 'public' }}</el-descriptions-item>
          <el-descriptions-item label="环境">
            <el-tag size="small" :type="envTag(extra.env)">{{ extra.env || '—' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="StarRocks 分桶数 BUCKETS"><strong>{{ extra.sr_auto_buckets || 10 }}</strong></el-descriptions-item>
          <el-descriptions-item label="StarRocks 表副本数"><strong>{{ extra.sr_replication_num || 1 }}</strong></el-descriptions-item>
        </el-descriptions>
      </el-card>
      <el-card shadow="never" class="cdc-card small">
        <template #header><span class="card-title">同步配置</span></template>
        <el-descriptions :column="descCols" size="small">
          <el-descriptions-item label="Debezium 模式">
            <el-tag size="small" :type="extra.debezium_mode === 'before_after' ? 'warning' : ''">{{ extra.debezium_mode || 'after_only' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="快照模式">{{ extra.snapshot_mode || 'initial' }}</el-descriptions-item>
          <el-descriptions-item label="过滤删除事件">{{ extra.filter_delete_events !== false ? '开启' : '关闭' }}</el-descriptions-item>
          <el-descriptions-item label="表模型">{{ keyModelLabel(extra.table_model || extra.key_model) }}</el-descriptions-item>
          <el-descriptions-item label="同步表数">
            <span>源端 <span class="c-src">{{ countView.source }}</span></span>
            <span class="muted"> · </span>
            <span>目标 <span class="c-tgt">{{ countView.target }}</span></span>
            <template v-if="countView.shard">
              <span class="muted"> · </span>
              <span>分表 <span class="c-shard">{{ countView.shard }}</span></span>
            </template>
          </el-descriptions-item>
          <el-descriptions-item label="动态分区">{{ extra.sr_dynamic_partition_enable ? '已开启' : '未开启' }}</el-descriptions-item>
        </el-descriptions>
      </el-card>
      <el-card shadow="never" class="cdc-card small">
        <template #header>
          <div class="card-head">
            <span class="card-title">Debezium 集群</span>
            <button type="button" class="name-link" @click="tab = 'debezium'">完整配置详见「Debezium 配置」Tab</button>
          </div>
        </template>
        <el-descriptions :column="descCols" size="small">
          <el-descriptions-item label="Connector ID">{{ extra.connector_id || '—' }}</el-descriptions-item>
          <el-descriptions-item label="Worker URL">
            <a v-if="workerURL" :href="workerURL" target="_blank" rel="noreferrer" class="name-link">{{ workerURL }}</a>
            <span v-else>—</span>
          </el-descriptions-item>
          <el-descriptions-item label="Kafka 消息格式">{{ extra.message_schema_mode === 'with_schema' ? '带 Schema' : '不带 Schema' }}</el-descriptions-item>
          <el-descriptions-item label="Kafka Bootstrap"><code>{{ extra.kafka_bootstrap_servers || '—' }}</code></el-descriptions-item>
        </el-descriptions>
      </el-card>
      <el-card shadow="never" class="cdc-card small">
        <template #header><span class="card-title">命名信息</span></template>
        <el-descriptions :column="descCols" size="small">
          <el-descriptions-item label="Publication"><code>{{ extra.publication_name || '—' }}</code></el-descriptions-item>
          <el-descriptions-item label="Slot"><code>{{ extra.slot_name || '—' }}</code></el-descriptions-item>
          <el-descriptions-item label="Topic 前缀" :span="2"><code>{{ extra.topic_prefix || '—' }}</code></el-descriptions-item>
        </el-descriptions>
      </el-card>
      <el-card shadow="never" class="cdc-card small">
        <template #header><span class="card-title">任务信息</span></template>
        <el-descriptions :column="descCols" size="small">
          <el-descriptions-item label="任务类型">{{ typeText(task.task_type) }}</el-descriptions-item>
          <el-descriptions-item label="关联任务">
            <button v-if="task.parent_id" type="button" class="name-link" @click="$router.push('/ops/cdc/' + task.parent_id)">{{ task.parent_id }}</button>
            <span v-else>—</span>
          </el-descriptions-item>
          <el-descriptions-item label="任务描述" :span="2">{{ note || '—' }}</el-descriptions-item>
          <el-descriptions-item label="创建人">{{ createdBy || '—' }}</el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ task.created_at || '—' }}</el-descriptions-item>
          <el-descriptions-item label="审核人">{{ assignedTo || '—' }}</el-descriptions-item>
          <el-descriptions-item label="审批状态">
            <el-button v-if="task.issue_id" link type="primary" @click="$router.push('/ops/tickets/' + task.issue_id)">{{ approvalText(task.approval_status) }}</el-button>
            <span v-else>{{ approvalText(task.approval_status) }}</span>
          </el-descriptions-item>
        </el-descriptions>
      </el-card>
    </div>

    <el-card v-show="tab === 'pipeline'" shadow="never" class="cdc-card wide">
      <div class="pipeline-toolbar mb-3 flex items-center justify-between">
        <div class="flex items-center gap-2">
          <span class="font-medium text-sm">全链路状态</span>
          <span class="text-xs text-gray-500">
            {{ pending || ['running','init','creating','configuring','provisioning'].includes(task.status)
              ? `自动刷新 8s · 上次更新 ${pipelineRefreshedAt || '—'}`
              : `已停止自动刷新 · 上次更新 ${pipelineRefreshedAt || '—'}` }}
          </span>
        </div>
        <el-button size="small" :loading="pipelineLoading" @click="refreshPipeline">手动刷新</el-button>
      </div>
      <el-alert
        v-if="pipeline.healthy === false"
        type="warning"
        show-icon
        :closable="false"
        class="mb"
        :title="pipeline.unhealthy_reason || '链路未完全就绪'"
      />
      <PipelineVisualization :nodes="pipelineNodes" @open-connector="openConnectorRuntime" />
      <div v-if="(pipe.routine_loads || []).length" class="table-scroll">
        <el-table :data="pipe.routine_loads" size="small" class="mt" empty-text="无 Routine Load">
          <el-table-column prop="table" label="目标表" />
          <el-table-column prop="name" label="Routine Load" />
          <el-table-column prop="state" label="状态" width="140" />
        </el-table>
      </div>
    </el-card>

    <el-card v-show="tab === 'tables'" shadow="never" class="cdc-card wide">
      <template #header>
        <div class="card-head">
          <div class="map-title">
            <span class="card-title">共 {{ visibleTableRows.length }} 组映射</span>
            <el-switch v-if="isInit && task.status === 'running'" v-model="showDeletedMaps" size="small" inline-prompt active-text="显示已移除" inactive-text="隐藏已移除" @change="reloadTables" />
          </div>
          <div class="map-tools">
            <el-input v-model="tableQ" clearable size="small" placeholder="搜索表" style="width:180px" />
            <el-button v-if="isInit && !pending" size="small" :loading="schemaScanning" @click="loadSchemaAlerts(true)">检测结构差异</el-button>
            <el-button size="small" @click="reloadTables">刷新</el-button>
          </div>
        </div>
      </template>
      <div class="table-scroll">
      <el-table :data="pagedTableRows" size="small" empty-text="暂无映射表">
        <el-table-column label="源表" min-width="220">
          <template #default="{ row }">
            <el-tag v-if="row.is_shard_group || (row.source_tables || []).length > 1" size="small" type="warning">分表 {{ (row.source_tables || []).length }}</el-tag>
            <el-tag v-else size="small">单表</el-tag>
            <code class="ml">{{ (row.source_tables || [row.name]).join(', ') }}</code>
          </template>
        </el-table-column>
        <el-table-column label="目标表" min-width="140">
          <template #default="{ row }">
            <code :class="{ muted: row.status === 'deleted' }">{{ row.target_table }}</code>
            <el-tag v-if="row.status === 'deleted'" size="small">已移除</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="KEY 列" min-width="140">
          <template #default="{ row }">
            <el-tag v-for="k in (row.key_columns || row.primary_keys || [])" :key="k" size="small" class="key-chip">{{ k }}</el-tag>
            <span v-if="!(row.key_columns || row.primary_keys || []).length" class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="Kafka Topic" min-width="200">
          <template #default="{ row }">
            <div v-if="(row.topic_names || []).length" class="topic-list">
              <code v-for="t in row.topic_names.slice(0, 3)" :key="t">{{ t }}</code>
            </div>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="结构差异" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.schema_has_drift" size="small" type="warning">缺列 {{ (row.schema_missing_columns || []).length || '' }}</el-tag>
            <el-tag v-else-if="schemaAlerts?.state === 'OK'" size="small" type="success">一致</el-tag>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="Routine Load" min-width="180">
          <template #default="{ row }">
            <code>{{ row.routine_load_name || '—' }}</code>
            <el-tag v-if="row.routine_load_status" size="small" :type="String(row.routine_load_status).toUpperCase() === 'RUNNING' ? 'success' : ''" class="ml">{{ row.routine_load_status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="mappingSourceTag(row.source)">{{ mappingSourceText(row.source) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" :disabled="!isInit || pending || row.status === 'deleted'" @click="diffOne(row)">+ 同步新字段</el-button>
            <el-button link type="primary" @click="showMappingDDL(row)">查看 DDL</el-button>
          </template>
        </el-table-column>
      </el-table>
      </div>
      <div class="cdc-pager">
        <el-pagination
          v-model:current-page="tablePage"
          v-model:page-size="tablePageSize"
          :total="visibleTableRows.length"
          :page-sizes="[10, 20, 50]"
          layout="total, prev, pager, next, sizes"
        />
      </div>
    </el-card>

    <el-card v-show="tab === 'publication'" shadow="never" class="cdc-card wide">
      <template #header>
        <div class="card-head">
          <span class="card-title">Publication {{ publication.publication_name || extra.publication_name || '' }}</span>
          <div>
            <el-button @click="loadPublication">刷新</el-button>
            <el-button v-if="!pending" @click="openPub('add')">追加表</el-button>
            <el-button v-if="!pending" type="danger" @click="openPub('remove')">移除表</el-button>
          </div>
        </div>
      </template>
      <el-alert v-if="publication.message" type="warning" show-icon :closable="false" class="mb" :title="publication.message" />
      <div class="table-scroll">
        <el-table :data="pubTableRows" empty-text="暂无 Publication 表">
          <el-table-column prop="name" label="表名" />
          <el-table-column label="来源" width="140">
            <template #default="{ row }">{{ row.live ? 'PG 实时' : '任务记录' }}</template>
          </el-table-column>
        </el-table>
      </div>
    </el-card>

    <el-card v-show="tab === 'debezium'" shadow="never" class="cdc-card wide">
      <CdcDebeziumConfigTab
        v-if="task.id"
        :task-id="task.id"
        :extra="extra"
        :pending="pending"
        @updated="load"
      />
    </el-card>
    <el-card v-show="tab === 'logs'" shadow="never" class="cdc-card">
      <div class="log-filters">
        <el-select v-model="logStep" clearable placeholder="步骤" style="width:200px" @change="loadLogs">
          <el-option v-for="s in logSteps" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
        <el-select v-model="logLevel" clearable placeholder="级别" style="width:100px" @change="loadLogs">
          <el-option label="INFO" value="INFO" />
          <el-option label="WARN" value="WARN" />
          <el-option label="ERROR" value="ERROR" />
        </el-select>
        <el-input v-model="logQ" clearable placeholder="搜索内容" style="width:180px" @keyup.enter="loadLogs" @clear="loadLogs" />
        <el-button type="primary" @click="loadLogs">筛选</el-button>
        <el-button @click="loadLogs">刷新</el-button>
      </div>
      <div class="table-scroll">
        <el-table :data="logs" empty-text="暂无操作日志" row-key="id">
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ formatLogTime(row.created_at || row.at) }}</template>
          </el-table-column>
          <el-table-column label="级别" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="row.level === 'ERROR' || row.log_level === 'ERROR' ? 'danger' : (row.level === 'WARN' || row.log_level === 'WARN' ? 'warning' : 'info')">{{ row.level || row.log_level }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="步骤" width="200">
            <template #default="{ row }">{{ logStepText(row.step) }}</template>
          </el-table-column>
          <el-table-column prop="message" label="消息" min-width="280" show-overflow-tooltip />
          <el-table-column label="详情" width="80">
            <template #default="{ row }">
              <el-button link type="primary" @click="openLogDetail(row)">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div class="log-pager">
        <el-pagination
          v-model:current-page="logPage"
          v-model:page-size="logPageSize"
          :total="logTotal"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="loadLogs"
          @size-change="loadLogs"
        />
      </div>
    </el-card>

    <el-dialog v-model="pubOpen" :title="pubMode === 'add' ? '追加同步表' : '移除同步表'" width="560px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item v-if="pubMode === 'add' && availablePubTables.length" label="可选表">
          <el-checkbox-group v-model="pubSelected">
            <el-checkbox v-for="t in availablePubTables" :key="t" :label="t" :value="t">{{ t }}</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item :label="pubMode === 'add' ? '或手动输入表名（逗号分隔）' : '表名（逗号分隔）'">
          <el-input v-model="pubTables" type="textarea" :rows="3" placeholder="如 orders, users" />
        </el-form-item>
        <el-form-item label="审核人">
          <el-input v-model="pubReviewer" placeholder="审批通过后执行" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pubOpen = false">取消</el-button>
        <el-button type="primary" :loading="pubSubmitting" @click="submitPub">提交审批</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="diffOpen" title="源表加列" width="720px" destroy-on-close>
      <el-alert type="info" show-icon :closable="false" class="mb" title="对比 PG 有而 StarRocks 无的列；提交后走 alter_schema 审批。" />
      <el-table :data="diffCols" size="small" empty-text="无待加列或尚未对比">
        <el-table-column width="48">
          <template #default="{ row }"><el-checkbox v-model="row.selected" /></template>
        </el-table-column>
        <el-table-column prop="name" label="列名" />
        <el-table-column prop="pg_type" label="PG" width="140" />
        <el-table-column prop="sr_type" label="SR" width="140" />
      </el-table>
      <el-input v-if="migrateSQL" :model-value="migrateSQL" type="textarea" :rows="8" readonly class="cfg-pre mt" />
      <el-form-item label="重建 Routine Load 位点" class="mt">
        <el-radio-group v-model="diffOffset">
          <el-radio value="OFFSET_BEGINNING">OFFSET_BEGINNING</el-radio>
          <el-radio value="OFFSET_END">OFFSET_END</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="审核人" class="mt">
        <el-input v-model="diffReviewer" placeholder="审批通过后执行" />
      </el-form-item>
      <template #footer>
        <el-button @click="diffOpen = false">取消</el-button>
        <el-button @click="loadMigrateSQL">预览迁移 SQL</el-button>
        <el-button type="primary" :loading="diffSubmitting" :disabled="!diffCols.some((c) => c.selected)" @click="submitAlter">提交加列审批</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="ddlOpen" :title="(ddlTarget || '映射') + ' DDL'" width="720px">
      <pre class="cfg-pre">{{ ddlSQL || '暂无 DDL' }}</pre>
      <template #footer>
        <el-button @click="ddlOpen = false">关闭</el-button>
      </template>
    </el-dialog>
    <el-drawer v-model="runtimeOpen" title="Debezium 运行状态" size="480px">
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item label="Connector">{{ runtime.name || extra.connector_name || '—' }}</el-descriptions-item>
        <el-descriptions-item label="状态">{{ runtime.state || runtime.worst_state || '—' }}</el-descriptions-item>
        <el-descriptions-item label="Worker">{{ runtime.worker_id || workerURL || '—' }}</el-descriptions-item>
      </el-descriptions>
      <el-table v-if="(runtime.tasks || []).length" :data="runtime.tasks" size="small" class="mt">
        <el-table-column prop="id" label="Task" width="80" />
        <el-table-column prop="state" label="状态" />
        <el-table-column prop="worker_id" label="Worker" />
      </el-table>
    </el-drawer>

    <el-dialog v-model="approveOpen" :title="isDeleteApproval ? '确认通过删除' : '审批通过'" width="480px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="审批意见（可选）">
          <el-input v-model="approveComment" type="textarea" :rows="3" placeholder="可留空" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="approveOpen = false">取消</el-button>
        <el-button type="primary" :loading="approving" @click="approveTicket">{{ isDeleteApproval ? '确认通过删除' : '确认通过' }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="logDetailOpen" title="日志详情" width="720px" destroy-on-close>
      <div class="log-detail-meta muted">
        {{ formatLogTime(logDetail.created_at || logDetail.at) }} · {{ logStepText(logDetail.step) }}
        <el-tag size="small" :type="(logDetail.level || logDetail.log_level) === 'ERROR' ? 'danger' : ((logDetail.level || logDetail.log_level) === 'WARN' ? 'warning' : 'info')">{{ logDetail.level || logDetail.log_level }}</el-tag>
        <span v-if="logDetail.operator"> · {{ logDetail.operator }}</span>
      </div>
      <p class="log-detail-msg">{{ logDetail.message }}</p>
      <pre class="log-detail-pre">{{ logDetail.detail || '（无 detail）' }}</pre>
    </el-dialog>
    <el-dialog v-model="rejectOpen" :title="isCreatorOnly ? '撤回待审批' : '审批驳回'" width="480px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="isCreatorOnly ? '撤回原因' : '驳回意见'">
          <el-input v-model="rejectComment" type="textarea" :rows="4" :placeholder="isCreatorOnly ? '请填写撤回原因/说明（必填）' : '必填'" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectOpen = false">取消</el-button>
        <el-button type="danger" :loading="approving" @click="rejectTicket">{{ isCreatorOnly ? '确认撤回' : '确认驳回' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import CdcDebeziumConfigTab from './CdcDebeziumConfigTab.vue'
import PipelineVisualization from './PipelineVisualization.vue'
import {
  approvalText,
  assignedToOf,
  createdByOf,
  currentUsername,
  decodeCdcExtra,
  envTag,
  formatLagBytes,
  formatLogTime,
  formatSourceScopeLabel,
  LOG_STEP_OPTIONS,
  logStepText,
  connectorWorkerURL,
  instLabel,
  isValidCdcTaskID,
  keyModelLabel,
  mappingSourceTag,
  mappingSourceText,
  pipelineBundle,
  pipelineNodeTone,
  sameUser,
  statusTag,
  syncTableCountView,
  statusText,
  typeTag,
  typeText,
} from './cdc'
import './cdc.css'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const instances = ref([])
const tab = ref('overview')
const pipeline = ref({})
const logs = ref([])
const logStep = ref('')
const logLevel = ref('')
const logQ = ref('')
const logPage = ref(1)
const logPageSize = ref(50)
const logTotal = ref(0)
const logSteps = LOG_STEP_OPTIONS
const logDetailOpen = ref(false)
const logDetail = ref({})
const schemaAlerts = ref(null)
const schemaScanning = ref(false)
const pipelineLoading = ref(false)
const pipelineRefreshedAt = ref('')
const approveOpen = ref(false)
const rejectOpen = ref(false)
const approveComment = ref('')
const rejectComment = ref('')
const tableRows = ref([])
const tableQ = ref('')
const showDeletedMaps = ref(false)
const tablePage = ref(1)
const tablePageSize = ref(10)
const ddlOpen = ref(false)
const ddlSQL = ref('')
const ddlTarget = ref('')
const runtimeOpen = ref(false)
const runtime = ref({})
const publication = ref({})
const pubOpen = ref(false)
const pubMode = ref('add')
const pubTables = ref('')
const pubReviewer = ref('')
const pubSubmitting = ref(false)
const diffOpen = ref(false)
const diffTarget = ref(null)
const diffCols = ref([])
const diffReviewer = ref('')
const diffSubmitting = ref(false)
const migrateSQL = ref('')
const approving = ref(false)
const pubSelected = ref([])
const availablePubTables = ref([])
const syncedTables = ref([])
const diffOffset = ref('OFFSET_BEGINNING')
let pollTimer = 0

const pending = computed(() => task.value.approval_status === 'pending' || task.value.status === 'pending')
const extra = computed(() => decodeCdcExtra(task.value.description).extra)
const errorStep = computed(() => pipeline.value.error_step || extra.value.error_step || '')
const note = computed(() => decodeCdcExtra(task.value.description).text)
const sourceScopeLabel = computed(() => formatSourceScopeLabel(task.value.source_db, extra.value.source_db_prefix, extra.value.source_db_names))
const source = computed(() => instances.value.find((i) => i.id === task.value.source_instance_id))
const target = computed(() => instances.value.find((i) => i.id === task.value.target_instance_id))
const isInit = computed(() => task.value.task_type === 'init')
const countView = computed(() => syncTableCountView(extra.value))
const workerURL = computed(() => connectorWorkerURL(extra.value, {}))
const visibleTableRows = computed(() => {
  const q = tableQ.value.trim().toLowerCase()
  return (tableRows.value || []).filter((r) => {
    if (!q) return true
    const blob = `${(r.source_tables || []).join(' ')} ${r.target_table || ''} ${(r.topic_names || []).join(' ')}`.toLowerCase()
    return blob.includes(q)
  })
})
const pagedTableRows = computed(() => {
  const start = (tablePage.value - 1) * tablePageSize.value
  return visibleTableRows.value.slice(start, start + tablePageSize.value)
})
const createdBy = computed(() => createdByOf(task.value, extra.value))
const assignedTo = computed(() => assignedToOf(task.value, extra.value))
const me = computed(() => currentUsername())
const isApprover = computed(() => pending.value && task.value.issue_id && (!assignedTo.value || sameUser(me.value, assignedTo.value)))
const isCreatorOnly = computed(() => pending.value && task.value.issue_id && sameUser(me.value, createdBy.value) && !isApprover.value)
const showApprovalBar = computed(() => pending.value && task.value.issue_id && (isApprover.value || isCreatorOnly.value || !me.value))
const isDeleteApproval = computed(() => task.value.task_type === 'delete_task')
const approvalBarTitle = computed(() => {
  if (isCreatorOnly.value) return isDeleteApproval.value ? '撤回删除申请' : '撤回待审批'
  return isDeleteApproval.value ? '删除审批' : '审批操作'
})
const approvalBarHint = computed(() => {
  if (isCreatorOnly.value) {
    return isDeleteApproval.value
      ? '您是申请人：点击「撤回」并填写原因后，取消本次删除申请，任务恢复原有运行状态。'
      : '您是创建人：点击「撤回」并填写原因后，任务进入「已驳回」。'
  }
  if (isDeleteApproval.value) {
    return assignedTo.value
      ? `删除审批人为「${assignedTo.value}」。通过后将清理 Publication / Slot / Connector / Routine Load 等资源；驳回则保留任务。`
      : '通过后执行删除并清理资源；驳回则取消删除申请。'
  }
  return assignedTo.value
    ? `本任务指定审批人为「${assignedTo.value}」，请在本区域完成通过/驳回。`
    : '请在本区域完成审批；列表页仅支持查询与进入详情。'
})
const tableDriftCount = computed(() => Number(schemaAlerts.value?.drift_table_count || 0))
const schemaHasDrift = computed(() => schemaAlerts.value?.state === 'WARN' || schemaAlerts.value?.state === 'ERROR')
const showSchemaBanner = computed(() => isInit.value && !pending.value && (schemaHasDrift.value || schemaAlerts.value?.state === 'OK' || schemaAlerts.value?.state === 'SKIPPED' || schemaAlerts.value))
const schemaBannerType = computed(() => {
  const st = String(schemaAlerts.value?.state || '').toUpperCase()
  if (st === 'ERROR') return 'error'
  if (st === 'WARN' && tableDriftCount.value) return 'warning'
  if (st === 'OK') return 'success'
  return 'info'
})
const schemaBannerTitle = computed(() => {
  const st = String(schemaAlerts.value?.state || '').toUpperCase()
  if (st === 'ERROR') return schemaAlerts.value?.message || '表结构巡检失败'
  if (st === 'WARN' && tableDriftCount.value) return schemaAlerts.value?.message || '表结构巡检有差异'
  if (st === 'OK') return '源端与目标表结构一致'
  if (st === 'SKIPPED') return '未执行结构检查'
  return '表结构巡检'
})
const pipe = computed(() => pipelineBundle(pipeline.value))
const pipelineNodes = computed(() => {
  const p = pipe.value || {}
  const pg = p.pg_source || p.pg || {}
  const pub = p.publication || {}
  const slot = p.slot || {}
  const conn = p.connector || {}
  const kafka = p.kafka || {}
  const sr = p.starrocks || {}
  const rls = p.routine_loads || []
  const runningRL = rls.filter((j) => String(j.state || '').toUpperCase() === 'RUNNING').length
  const lag = slot.replication_lag_bytes ?? slot.lag_bytes
  const manual = extra.value.auto_create_table === false && !task.value.target_instance_id
  return [
    {
      id: 'pg',
      name: 'PG Source',
      detail: pg.host && pg.port
        ? `${pg.host}:${pg.port}`
        : ((pub.name || slot.name) ? '源端可达（Publication/Slot 已探测）' : (pg.message || '等待探测')),
      tone: pipelineNodeTone(pg.state || ((pub.name || slot.name) ? 'READY' : '')),
      extras: [],
      message: pg.message || '',
    },
    {
      id: 'publication',
      name: 'Publication',
      detail: pub.name || extra.value.publication_name || '未上报',
      tone: pipelineNodeTone(pub.exists ? 'READY' : (pub.state || 'UNKNOWN')),
      extras: [
        { label: 'Publication 表', value: String(pub.table_count ?? 0) },
        { label: '存在', value: pub.exists ? '是' : '否' },
      ],
      message: pub.message || '',
    },
    {
      id: 'slot',
      name: 'Replication Slot',
      detail: slot.name || extra.value.slot_name || '未上报',
      tone: pipelineNodeTone(slot.active ? 'READY' : (slot.state || 'UNKNOWN')),
      extras: [
        { label: '状态', value: slot.active ? 'active' : 'inactive' },
        { label: 'lag', value: formatLagBytes(lag) },
      ],
      message: slot.message || slot.restart_lsn || '',
    },
    {
      id: 'debezium',
      name: 'Debezium Connector',
      detail: conn.name || extra.value.connector_name || '未上报',
      tone: pipelineNodeTone(conn.state),
      extras: [
        { label: '状态', value: conn.state || '—' },
        { label: '模式', value: extra.value.debezium_mode || conn.debezium_mode || '—' },
        ...((conn.tasks || []).length
          ? [{ label: 'tasks', value: `${(conn.tasks || []).filter((t) => String(t.state).toUpperCase() === 'RUNNING').length}/${conn.tasks.length} RUNNING` }]
          : []),
      ],
      message: conn.message || '',
      ready: Boolean(conn.name || extra.value.connector_name),
    },
    {
      id: 'kafka',
      name: 'Kafka Topics',
      detail: 'Kafka 消息总线',
      tone: pipelineNodeTone(kafka.state),
      extras: [
        { label: 'state', value: kafka.state || '—' },
        { label: 'topics', value: String(kafka.topic_count ?? (p.kafka_topics || []).length) },
        ...(kafka.all_exist != null ? [{ label: 'all_exist', value: kafka.all_exist ? '是' : '否' }] : []),
      ],
      message: kafka.message || '',
    },
    {
      id: 'starrocks',
      name: 'StarRocks',
      detail: manual
        ? '目标端未配置（手动模式，无需建表）'
        : (sr.host && sr.port ? `${sr.host}:${sr.port}` : (sr.message || task.value.target_db || '等待')),
      tone: manual ? 'is-ok' : pipelineNodeTone(sr.state),
      extras: manual
        ? [{ label: '说明', value: '手动 DDL' }]
        : [{ label: 'state', value: sr.state || '—' }, { label: '配置表数', value: String(pub.table_count ?? rls.length) }],
    },
    {
      id: 'routine_load',
      name: 'Routine Load',
      detail: '导入任务（依赖目标表）',
      tone: manual ? 'is-ok' : pipelineNodeTone(sr.state),
      extras: rls.length
        ? [
          { label: '运行/总数', value: `${runningRL}/${rls.length}` },
          { label: '同步任务', value: rls.slice(0, 2).map((j) => j.name || j.table).join(', ') + (rls.length > 2 ? '…' : '') },
        ]
        : [{ label: '状态', value: '等待' }],
    },
  ]
})
const descCols = ref(window.innerWidth >= 900 ? 2 : 1)
function syncDescCols() {
  descCols.value = window.innerWidth >= 900 ? 2 : 1
}
const pubTableRows = computed(() => {
  const live = publication.value.tables || []
  const rec = publication.value.task_tables || extra.value.tables || []
  const set = new Set()
  const out = []
  for (const n of live) {
    set.add(String(n).toLowerCase())
    out.push({ name: n, live: true })
  }
  for (const n of rec) {
    if (!set.has(String(n).toLowerCase())) out.push({ name: n, live: false })
  }
  return out
})

function taskID() {
  const id = task.value.id || route.params.id
  return isValidCdcTaskID(id) ? String(id).trim() : ''
}

async function refreshPipeline() {
  const id = taskID()
  if (!id) return
  pipelineLoading.value = true
  try {
    const { data } = await api.get(`/cdc-sync/tasks/${id}/status`, { silent: true })
    pipeline.value = data.data || {}
    pipelineRefreshedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  } finally {
    pipelineLoading.value = false
  }
}

async function load() {
  const id = taskID()
  if (!id) {
    clearInterval(pollTimer)
    return
  }
  loading.value = true
  try {
    const [t, inst, st, tb] = await Promise.all([
      api.get(`/cdc-sync/tasks/${id}`),
      api.get('/instances'),
      api.get(`/cdc-sync/tasks/${id}/status`, { silent: true }).catch(() => ({ data: {} })),
      api.get(`/cdc-sync/tasks/${id}/tables`, { params: tableQuery(), silent: true }).catch(() => ({ data: {} })),
    ])
    task.value = t.data.data || {}
    instances.value = inst.data.data || []
    pipeline.value = st.data.data || {}
    pipelineRefreshedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
    const raw = tb.data.data || []
    tableRows.value = raw.map((x) => (typeof x === 'string'
      ? { name: x, target_table: x, source_tables: [x] }
      : { name: (x.source_tables || [])[0], ...x }))
    mergeRLFromPipeline()
    await loadLogs()
    if (task.value.task_type === 'init' || task.value.parent_id) await loadSchemaAlerts(false)
    if (task.value.task_type === 'init') await loadPublication()
    schedulePoll()
  } finally {
    loading.value = false
  }
}

function schedulePoll() {
  clearInterval(pollTimer)
  const should = pending.value || ['running', 'init', 'creating', 'configuring', 'provisioning', 'failed', 'error'].includes(task.value.status)
  if (!should || !taskID()) return
  pollTimer = setInterval(() => {
    const id = taskID()
    if (!id) {
      clearInterval(pollTimer)
      return
    }
    api.get(`/cdc-sync/tasks/${id}/status`, { silent: true }).then(({ data }) => {
      pipeline.value = data.data || {}
      pipelineRefreshedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
    }).catch(() => {})
    api.get(`/cdc-sync/tasks/${id}`, { silent: true }).then(({ data }) => {
      task.value = data.data || task.value
    }).catch(() => {})
    if (tab.value === 'logs') loadLogs()
  }, 8000)
}

function openLogDetail(row) {
  logDetail.value = row || {}
  logDetailOpen.value = true
}

async function loadLogs() {
  const id = taskID()
  if (!id) return
  const { data } = await api.get(`/cdc-sync/tasks/${id}/logs`, {
    params: { step: logStep.value, level: logLevel.value, log_level: logLevel.value, q: logQ.value, page: logPage.value, page_size: logPageSize.value },
    silent: true,
  }).catch(() => ({ data: {} }))
  const body = data.data
  const list = Array.isArray(body) ? body : (body?.list || [])
  logs.value = list
  logTotal.value = body?.pagination?.total ?? data.total ?? list.length
}

async function loadSchemaAlerts(refresh = false) {
  const id = taskID()
  if (!id) return
  if (refresh) schemaScanning.value = true
  const { data } = refresh
    ? await api.post(`/cdc-sync/tasks/${id}/schema-alerts/scan`).catch(() => ({ data: {} }))
    : await api.get(`/cdc-sync/tasks/${id}/schema-alerts`).catch(() => ({ data: {} }))
  schemaAlerts.value = data.data || null
  schemaScanning.value = false
  if (!schemaAlerts.value?.items) return
  const by = new Map((schemaAlerts.value.items || []).map((it) => [String(it.target_table || '').toLowerCase(), it]))
  tableRows.value = tableRows.value.map((row) => {
    const it = by.get(String(row.target_table || '').toLowerCase())
    if (!it) return row
    return { ...row, schema_has_drift: !!it.has_drift, schema_missing_columns: it.missing_columns || [] }
  })
}

async function loadPublication() {
  if (!task.value.id) return
  const [{ data }, synced] = await Promise.all([
    api.get(`/cdc-sync/tasks/${task.value.id}/publication`).catch(() => ({ data: {} })),
    api.get(`/cdc-sync/tasks/${task.value.id}/synced-tables`).catch(() => ({ data: {} })),
  ])
  publication.value = data.data || {}
  syncedTables.value = synced.data.data || []
}

async function openPub(mode) {
  pubMode.value = mode
  pubTables.value = ''
  pubSelected.value = []
  pubReviewer.value = ''
  pubOpen.value = true
  if (mode === 'add') {
    const { data } = await api.get(`/cdc-sync/tasks/${task.value.id}/add-table/available-tables`).catch(() => ({ data: {} }))
    availablePubTables.value = data.data || []
  } else {
    availablePubTables.value = syncedTables.value.length ? syncedTables.value : (extra.value.tables || [])
  }
}

async function submitPub() {
  const tables = [...new Set([
    ...pubSelected.value,
    ...pubTables.value.split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean),
  ])]
  if (!tables.length) {
    ElMessage.warning('请填写表名')
    return
  }
  if (!pubReviewer.value) {
    ElMessage.warning('请填写审核人')
    return
  }
  pubSubmitting.value = true
  try {
    const body = { tables, reviewer: pubReviewer.value, approver: pubReviewer.value }
    const { data } = pubMode.value === 'add'
      ? await api.post(`/cdc-sync/tasks/${task.value.id}/publication/tables`, body)
      : await api.delete(`/cdc-sync/tasks/${task.value.id}/publication/tables`, { data: body })
    ElMessage.success('已提交审批')
    pubOpen.value = false
    if (data.data?.issue_id) router.push('/ops/tickets/' + data.data.issue_id)
    else load()
  } finally {
    pubSubmitting.value = false
  }
}

function tableQuery() {
  return {
    include_deleted: showDeletedMaps.value ? '1' : undefined,
    include_schema_drift: isInit.value ? '1' : undefined,
  }
}

async function reloadTables() {
  if (!route.params.id) return
  const { data } = await api.get(`/cdc-sync/tasks/${route.params.id}/tables`, { params: tableQuery() }).catch(() => ({ data: {} }))
  const raw = data.data || []
  tableRows.value = raw.map((x) => (typeof x === 'string'
    ? { name: x, target_table: x, source_tables: [x] }
    : { name: (x.source_tables || [])[0], ...x }))
  mergeRLFromPipeline()
}

function mergeRLFromPipeline() {
  const rls = pipe.value.routine_loads || pipeline.value.routine_loads || []
  if (!rls.length) return
  const by = new Map(rls.map((j) => [String(j.table || '').toLowerCase(), j]))
  tableRows.value = tableRows.value.map((row) => {
    const hit = by.get(String(row.target_table || '').toLowerCase())
    if (!hit) return row
    return {
      ...row,
      routine_load_name: row.routine_load_name || hit.name,
      routine_load_status: hit.state || row.routine_load_status,
    }
  })
}

async function showMappingDDL(row) {
  ddlTarget.value = row.target_table || '映射'
  ddlSQL.value = '加载中…'
  ddlOpen.value = true
  const id = row.id || row.target_table
  const { data } = await api.get(`/cdc-sync/tasks/${task.value.id}/tables/${encodeURIComponent(id)}/ddl`).catch(() => ({ data: {} }))
  const d = data.data || {}
  const create = d.create_table_sql || ''
  const load = d.routine_load_sql || ''
  ddlSQL.value = [create, load ? `-- Routine Load\n${load}` : ''].filter(Boolean).join('\n\n') || '暂无 DDL'
}

async function openConnectorRuntime() {
  runtimeOpen.value = true
  const { data } = await api.get(`/cdc-sync/tasks/${task.value.id}/connector/status`).catch(() => ({ data: {} }))
  const d = data.data || {}
  const conn = d.connector || {}
  runtime.value = {
    name: conn.name || extra.value.connector_name,
    state: conn.state || d.worst_state,
    worker_id: conn.worker_id,
    tasks: conn.tasks || d.tasks || [],
  }
}

function openAddCols() {
  if (tableRows.value[0]) void diffOne(tableRows.value[0])
  else ElMessage.warning('暂无映射')
}

async function diffOne(row) {
  diffTarget.value = row
  diffCols.value = []
  migrateSQL.value = ''
  diffReviewer.value = ''
  diffOpen.value = true
  try {
    const { data } = await api.post(`/cdc-sync/tasks/${task.value.id}/schema-diff`, {
      target_table: row.target_table,
      source_tables: row.source_tables || [row.target_table],
    })
    const item = (data.data?.items || [])[0]
    diffCols.value = (item?.columns_to_add || []).map((c) => ({ ...c, selected: c.selected !== false }))
    if (item?.warnings?.length) ElMessage.warning(item.warnings.join('；'))
  } catch (e) {
    ElMessage.warning(e?.response?.data?.message || '列对比失败')
  }
}

async function loadMigrateSQL() {
  const cols = diffCols.value.filter((c) => c.selected)
  const { data } = await api.post(`/cdc-sync/tasks/${task.value.id}/schema-migrate-sql`, {
    target_table: diffTarget.value?.target_table,
    columns: cols,
  })
  migrateSQL.value = data.data?.sql || ''
}

async function submitAlter() {
  if (!diffReviewer.value) {
    ElMessage.warning('请填写审核人')
    return
  }
  const cols = diffCols.value.filter((c) => c.selected)
  diffSubmitting.value = true
  try {
    const { data } = await api.post('/cdc-sync/tasks', {
      name: `${task.value.name}_加列`,
      task_type: 'alter_schema',
      parent_id: task.value.id,
      source_instance_id: task.value.source_instance_id,
      target_instance_id: task.value.target_instance_id,
      source_db: task.value.source_db,
      target_db: task.value.target_db,
      reviewer: diffReviewer.value,
      approver: diffReviewer.value,
      description: '源表加列',
      tables: [diffTarget.value.target_table],
      alter_schema_exec_mode: 'auto',
      kafka_default_offsets: diffOffset.value,
      alter_schema_tables: [{
        target_table: diffTarget.value.target_table,
        source_tables: diffTarget.value.source_tables || [diffTarget.value.target_table],
        columns: cols,
      }],
    })
    ElMessage.success('已提交加列审批')
    diffOpen.value = false
    if (data.data?.issue_id) router.push('/ops/tickets/' + data.data.issue_id)
  } finally {
    diffSubmitting.value = false
  }
}

async function act(kind) {
  const { data } = await api.post(`/cdc-sync/tasks/${task.value.id}/${kind}`)
  ElMessage.info(data.message || '已更新')
  await load()
}

function openApprove() {
  approveComment.value = ''
  approveOpen.value = true
}
function openReject() {
  rejectComment.value = ''
  rejectOpen.value = true
}

async function approveTicket() {
  if (!task.value.issue_id) return
  approving.value = true
  try {
    await api.put(`/tickets/${task.value.issue_id}/approve`, { comment: approveComment.value || undefined })
    ElMessage.success(isDeleteApproval.value ? '删除审批已通过，任务已清理' : '已通过审批')
    approveOpen.value = false
    if (isDeleteApproval.value) {
      router.push('/ops/cdc')
      return
    }
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '审批失败')
  } finally {
    approving.value = false
  }
}

async function rejectTicket() {
  if (!task.value.issue_id) return
  if (!String(rejectComment.value || '').trim()) {
    ElMessage.warning(isCreatorOnly.value ? '请填写撤回原因' : '请填写驳回意见')
    return
  }
  approving.value = true
  try {
    await api.put(`/tickets/${task.value.issue_id}/reject`, { reason: rejectComment.value.trim() })
    ElMessage.success('操作成功')
    rejectOpen.value = false
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '驳回失败')
  } finally {
    approving.value = false
  }
}

watch(() => route.params.id, (id) => {
  if (!isValidCdcTaskID(id)) {
    clearInterval(pollTimer)
    return
  }
  load()
})
watch(tab, (name) => {
  if (name === 'logs') loadLogs()
})

onMounted(() => {
  syncDescCols()
  window.addEventListener('resize', syncDescCols)
  if (!taskID()) {
    router.replace('/ops/cdc')
    return
  }
  load()
})
onUnmounted(() => {
  window.removeEventListener('resize', syncDescCols)
  clearInterval(pollTimer)
})
</script>

<style scoped>
.cdc-detail { overflow-x: hidden; overflow-y: auto; min-width: 0; }
.cdc-card { border-radius: 8px; min-width: 0; }
.cdc-card.wide { max-width: none; }
.card-head { flex-wrap: wrap; }
.log-filters { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.log-pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.log-detail-meta { margin-bottom: 8px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.log-detail-msg { margin: 8px 0 12px; }
.log-detail-pre { background: #f5f7fa; padding: 12px; border-radius: 6px; font-size: 12px; max-height: 50vh; overflow: auto; white-space: pre-wrap; }
.card-title { font-weight: 500; }
.card-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; }
.cfg-pre {
  margin: 0;
  padding: 12px;
  background: #f8fafc;
  border-radius: 6px;
  font-family: Menlo, Consolas, Monaco, monospace;
  font-size: 12px;
  line-height: 1.5;
  overflow: auto;
  max-height: 480px;
  white-space: pre-wrap;
}
.ops { display: flex; gap: 8px; flex-wrap: wrap; }
.overview-stack { display: flex; flex-direction: column; gap: 12px; }
.map-title, .map-tools { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.key-chip { margin-right: 4px; }
.topic-list { display: flex; flex-direction: column; gap: 2px; font-size: 12px; }
.ml { margin-left: 6px; }
</style>
