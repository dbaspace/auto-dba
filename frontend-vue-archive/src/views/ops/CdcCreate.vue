<template>
  <div class="cdc-shell cdc-create">
    <div class="cdc-back">
      <el-button text type="primary" @click="goList">← 返回列表</el-button>
    </div>
    <div class="cdc-title-row">
      <div class="cdc-title-left">
        <h2>{{ pageTitle }}</h2>
        <el-tag size="small" :type="typeTag(form.task_type)">{{ typeText(form.task_type) }}</el-tag>
      </div>
      <div class="cdc-hint">{{ pageHint }}</div>
    </div>

    <el-steps :active="step" finish-status="success" class="cdc-steps">
      <el-step v-for="s in steps" :key="s" :title="s" />
    </el-steps>

    <el-form ref="formRef" :model="form" :rules="rules" label-position="top" :class="['cdc-form', isWideStep ? 'cdc-form-wide' : '']">
      <!-- init: 选择源端 -->
      <template v-if="mode === 'init' && step === 0">
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">基本信息</span></template>
          <el-form-item label="任务名称" prop="name">
            <el-input v-model="form.name" maxlength="100" show-word-limit placeholder="例：prod_order_db" />
          </el-form-item>
          <el-form-item label="任务描述">
            <el-input v-model="form.description" type="textarea" :rows="2" maxlength="300" show-word-limit placeholder="可选，描述同步范围、负责人、关联工单等" />
          </el-form-item>
          <el-form-item label="环境" prop="env">
            <el-select v-model="form.env" filterable placeholder="请选择环境" style="width:220px" @change="onEnvChange">
              <el-option v-for="e in envOptions" :key="e.value" :label="e.label" :value="e.value" />
            </el-select>
          </el-form-item>
        </el-card>
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">源端 PostgreSQL</span></template>
          <el-form-item label="实例" prop="source_instance_id">
            <el-select v-model="form.source_instance_id" filterable :disabled="!form.env" :placeholder="form.env ? '搜索 / 选择 PG 实例' : '请先选择环境'" style="width:100%" @change="onSourceInstChange">
              <el-option v-for="i in sourceInstances" :key="i.id" :label="instLabel(i)" :value="String(i.id)" />
            </el-select>
          </el-form-item>
          <el-form-item label="同步范围">
            <el-radio-group v-model="form.source_scope_mode" @change="onScopeModeChange">
              <el-radio-button value="single_db">单库</el-radio-button>
              <el-radio-button value="multi_db">多分库</el-radio-button>
            </el-radio-group>
            <div class="form-extra">多分库：同一实例内同构分库一次 init，各库独立 Publication / Slot / Connector，共享 Topic 前缀。</div>
          </el-form-item>
          <el-form-item :label="form.source_scope_mode === 'multi_db' ? '代表库' : '数据库'" prop="source_db">
            <el-select v-model="form.source_db" filterable allow-create :disabled="!form.source_instance_id" :loading="loadingDbs" :placeholder="form.source_instance_id ? '选择库（可手动输入）' : '请先选择实例'" style="width:320px" @change="onSourceDbChange">
              <el-option v-for="d in sourceDbs" :key="d" :label="d" :value="d" />
            </el-select>
            <div v-if="form.source_scope_mode === 'multi_db'" class="form-extra">代表库用于探测表结构，同构分库字段应一致。</div>
          </el-form-item>
          <template v-if="form.source_scope_mode === 'multi_db'">
            <el-form-item label="分库前缀">
              <el-input v-model="form.source_db_prefix" clearable placeholder="如 db_order_master_" style="width:320px" @change="syncResolvedShardDbs" />
            </el-form-item>
            <el-alert v-if="scopePrefixHint" type="warning" show-icon :closable="false" class="mb" :title="scopePrefixHint" />
            <el-form-item label="匹配到的分库">
              <div v-if="resolvedShardDbs.length" class="shard-db-tags">
                <el-tag v-for="n in resolvedShardDbs" :key="n" size="small" :type="n === form.source_db ? 'primary' : 'info'">{{ n }}</el-tag>
              </div>
              <div v-else class="form-extra warn">填写前缀后将从实例库列表自动匹配；有效多分库需 ≥ 2 个库。</div>
            </el-form-item>
          </template>
          <el-form-item label="Schema" prop="source_schema">
            <el-select v-model="form.source_schema" filterable allow-create placeholder="public" style="width:220px">
              <el-option label="public" value="public" />
            </el-select>
          </el-form-item>
        </el-card>
      </template>

      <!-- init: 选择表 -->
      <template v-else-if="mode === 'init' && step === 1">
        <el-alert type="info" show-icon :closable="false" class="mb" title="多张 PG 分表 → 1 张 StarRocks 目标表" description="点击「分表匹配」批量归并；勾选单表则一对一映射。同一组分表只会写入一张 StarRocks 目标表。" />
        <el-alert type="error" show-icon :closable="false" class="mb" title="请为每张映射配置 KEY 列" description="点击右侧映射卡片展开后，在「KEY 列」中可多选并调整字段顺序；KEY 及其顺序会决定自动建表时的数据分布（分桶/排序键）与后续 SQL 查询性能，请谨慎选择。" />
        <div v-if="resolvedShardDbs.length > 1" class="scope-summary">
          <div>
            <strong>当前范围（多分库）：</strong>
            代表库 <code>{{ form.source_db || '—' }}</code>
            · Schema <code>{{ form.source_schema || 'public' }}</code>
            <template v-if="form.source_db_prefix"> · 前缀 <code>{{ form.source_db_prefix }}</code></template>
            · 已匹配 {{ resolvedShardDbs.length }} 个库
          </div>
          <div class="shard-db-tags mt">
            <el-tag v-for="n in resolvedShardDbs" :key="n" size="small" type="primary">{{ n }}</el-tag>
          </div>
          <div class="form-extra">逻辑表选一次即覆盖全部分库；字段以代表库为准，请确保各分库表结构一致。</div>
        </div>
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="cdc-card map-card">
              <template #header>
                <div class="card-head">
                  <span class="card-title">源端表列表</span>
                  <el-button text type="primary" @click="shardOpen = true">分表匹配</el-button>
                </div>
              </template>
              <div class="tbl-bar">
                <el-input v-model="tableQ" clearable placeholder="搜索表名" style="flex:1">
                  <template #prefix><el-icon><Search /></el-icon></template>
                </el-input>
                <el-checkbox :model-value="allSelectableChecked" :indeterminate="allSelectableIndeterminate" @change="toggleAllSelectable">全选可选（{{ selectableCount }}）</el-checkbox>
                <el-switch v-model="groupShards" inline-prompt active-text="分表归类" inactive-text="平铺" />
                <el-button :loading="loadingTables" @click="loadTables">刷新</el-button>
              </div>
              <el-alert v-if="form.sr_dynamic_partition_enable" type="info" show-icon :closable="false" class="mb" title="已开启动态分区" description="请在右侧映射中为每张表选择 DATE/DATETIME 分区列；留空则使用任务默认分区键。" />
              <el-alert v-if="tableHint" type="info" show-icon :closable="false" class="mb" :title="tableHint" />
              <div class="src-list">
                <div v-for="g in visibleShardGroups" :key="'g-' + g.base" class="src-row">
                  <el-checkbox :model-value="isGroupSelected(g)" @change="(v) => toggleGroup(g, v)">
                    <span class="src-name">{{ g.targetTable || g.base }}</span>
                    <el-tag size="small" type="warning">分表 {{ g.members.length }}</el-tag>
                  </el-checkbox>
                  <el-button text class="expand-btn" @click="toggleExpand(g.base)">{{ expandedGroups.has(g.base) ? '收起' : '展开' }}</el-button>
                  <div v-if="expandedGroups.has(g.base)" class="src-members">
                    <div v-for="n in g.members" :key="n" class="muted">{{ n }}</div>
                  </div>
                </div>
                <div v-for="t in visibleSingleTables" :key="t" class="src-row">
                  <el-checkbox :model-value="isTableSelected(t)" @change="(v) => toggleSingle(t, v)">{{ t }}</el-checkbox>
                </div>
                <div v-if="!visibleShardGroups.length && !visibleSingleTables.length" class="tree-empty">暂无表</div>
              </div>
              <el-form-item label="手动补充表名" class="mt">
                <el-input v-model="manualTables" type="textarea" :rows="2" placeholder="连不上库时可手动填写，逗号或换行分隔" @blur="mergeManualTables" />
              </el-form-item>
            </el-card>
          </el-col>
          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="cdc-card map-card">
              <template #header>
                <div class="card-head">
                  <span class="card-title">已选同步映射</span>
                  <span class="muted">共 {{ mappings.length }} 组</span>
                </div>
              </template>
              <CdcTableMappingPanel
                v-model:mappings="mappings"
                :instance-id="form.source_instance_id"
                :db-name="form.source_db"
                :schema="form.source_schema || 'public'"
                :partition-enabled="form.sr_dynamic_partition_enable"
                :task-partition-column="form.sr_partition_column"
                :shard-db-names="resolvedShardDbs"
                :show-sync-mode="form.task_type === 'add_table'"
              />
            </el-card>
          </el-col>
        </el-row>
      </template>

      <!-- init: Debezium -->
      <template v-else-if="mode === 'init' && step === 2">
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">Debezium 配置</span></template>
          <el-form-item label="Kafka 消息格式" prop="message_schema_mode">
            <el-radio-group v-model="form.message_schema_mode">
              <el-radio value="without_schema">不带 Schema</el-radio>
              <el-radio value="with_schema">带 Schema</el-radio>
            </el-radio-group>
            <div class="form-extra">先确认 Connect 集群是否带 Schema，再选择同类型的 Debezium 地址。</div>
          </el-form-item>
          <el-form-item label="Debezium 集群" prop="connector_id">
            <el-select v-model="form.connector_id" filterable :placeholder="form.env ? '选择当前环境的 Debezium 地址' : '请先选择环境'" style="width:100%">
              <el-option v-for="c in filteredConnectors" :key="c.id" :label="c.dbz_host" :value="c.id">
                <div class="opt-main">{{ c.dbz_host }}</div>
                <div class="opt-sub">Kafka：{{ normalizeKafkaBootstrap(c.kafka_host) || '—' }}</div>
              </el-option>
            </el-select>
            <div v-if="form.env && !filteredConnectors.length" class="form-extra warn">当前环境暂无匹配集群，请先在「配置 → 连接器」维护。</div>
          </el-form-item>
          <el-alert
            v-if="selectedConnector && form.message_schema_mode === 'with_schema' && form.debezium_mode === 'after_only'"
            type="warning"
            show-icon
            :closable="false"
            class="mb"
            title="Debezium 集群消息格式"
            description="所选集群为带 Schema，当前任务模版为 after_only 扁平 JSON，请确认 Connect 集群与 StarRocks 消费端兼容。"
          />
          <el-descriptions v-if="selectedConnector" :column="1" border size="small" class="mb">
            <el-descriptions-item label="Debezium 地址">{{ selectedConnector.dbz_host || '—' }}</el-descriptions-item>
            <el-descriptions-item label="Kafka 消息格式">
              <el-tag size="small" :type="form.message_schema_mode === 'with_schema' ? 'primary' : 'info'">
                {{ form.message_schema_mode === 'with_schema' ? '带 Schema' : '不带 Schema' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="Kafka 地址（连接器配置）">
              <code v-if="connectorKafkaHost">{{ connectorKafkaHost }}</code>
              <span v-else class="muted">未配置（可在高级配置中手动覆盖）</span>
            </el-descriptions-item>
          </el-descriptions>
          <div v-if="selectedConnector" class="form-extra mb">创建任务时按连接器解析 Kafka；上表来自「配置 → 连接器」，无需额外选择。</div>
          <el-form-item label="Debezium 模式" prop="debezium_mode">
            <el-radio-group :model-value="form.debezium_mode" class="mode-radios" @change="onDebeziumModeChange">
              <div class="mode-opt">
                <el-radio value="after_only">
                  仅 After
                  <el-tag size="small" type="primary" class="ml">推荐</el-tag>
                </el-radio>
                <div class="opt-sub">输出变更后的数据，适合大多数同步场景，消息更紧凑。</div>
              </div>
              <div class="mode-opt">
                <el-radio value="before_after">Before + After</el-radio>
                <div class="opt-sub">输出变更前后完整数据，适合审计与对比；<span class="warn-text">消息体积与存储成本会显著上升</span>。</div>
              </div>
            </el-radio-group>
          </el-form-item>
          <el-form-item prop="snapshot_mode">
            <template #label>
              同步模式
              <el-tooltip content="对应 Debezium snapshot.mode：决定首次是否全量同步历史数据" placement="top">
                <span class="qmark">?</span>
              </el-tooltip>
            </template>
            <el-radio-group v-model="form.snapshot_mode">
              <el-radio value="initial">全量 + 增量</el-radio>
              <el-radio value="never">增量</el-radio>
            </el-radio-group>
            <div class="form-extra">{{ form.snapshot_mode === 'never' ? '增量：仅从当前位点开始消费新数据，不回拉历史。' : '全量 + 增量：先同步表的历史全量数据，再进入增量同步。' }}</div>
          </el-form-item>
          <el-form-item>
            <template #label>
              过滤删除事件
              <el-tooltip content="开启后 Debezium 将过滤 DELETE 事件；关闭则保留删除事件同步。后端据此选择 Connector 配置模版。" placement="top">
                <span class="qmark">?</span>
              </el-tooltip>
            </template>
            <el-switch v-model="form.filter_delete_events" active-text="开" inactive-text="关" />
          </el-form-item>
          <el-collapse class="cdc-adv">
            <el-collapse-item title="高级配置" name="advanced">
              <el-form-item>
                <template #label>
                  Kafka Bootstrap 覆盖
                  <el-tooltip content="默认使用连接器表中 Kafka 地址，一般无需填写" placement="top">
                    <span class="qmark">?</span>
                  </el-tooltip>
                </template>
                <el-input v-model="form.kafka_bootstrap_servers" placeholder="留空则使用连接器配置中的 Kafka 地址" />
              </el-form-item>
              <el-form-item label="Routine Load 并发数">
                <el-input-number v-model="form.routine_load_parallelism" :min="1" :max="32" controls-position="right" style="width:100%" />
                <div class="form-extra">当前后端仅接受该字段，实际并发由全局参数 sync_sr_routine_load_desired_concurrent 决定，填写暂不生效。</div>
              </el-form-item>
              <div class="infra-title">Kafka / StarRocks 建表参数</div>
              <div class="form-extra mb">对应全局参数默认值；提交后覆盖任务级配置。未传或 ≤0 时后端仍用全局默认。</div>
              <el-form-item>
                <template #label>
                  Kafka Topic 分区数
                  <el-tooltip content="Debezium topic.creation.default.partitions" placement="top">
                    <span class="qmark">?</span>
                  </el-tooltip>
                </template>
                <el-input-number v-model="form.kafka_topic_partitions" :min="1" :max="128" controls-position="right" style="width:100%" />
              </el-form-item>
              <el-form-item>
                <template #label>
                  Kafka Topic 副本数
                  <el-tooltip content="Debezium topic.creation.default.replication.factor" placement="top">
                    <span class="qmark">?</span>
                  </el-tooltip>
                </template>
                <el-input-number v-model="form.kafka_topic_replication_factor" :min="1" :max="5" controls-position="right" style="width:100%" />
              </el-form-item>
              <el-form-item>
                <template #label>
                  StarRocks 分桶数 BUCKETS
                  <el-tooltip content="自动建表 DDL 中 DISTRIBUTED BY HASH ... BUCKETS N" placement="top">
                    <span class="qmark">?</span>
                  </el-tooltip>
                </template>
                <el-input-number v-model="form.sr_auto_buckets" :min="1" :max="1024" controls-position="right" style="width:100%" />
              </el-form-item>
              <el-form-item>
                <template #label>
                  StarRocks 表副本数
                  <el-tooltip content='建表 PROPERTIES("replication_num" = "N")' placement="top">
                    <span class="qmark">?</span>
                  </el-tooltip>
                </template>
                <el-input-number v-model="form.sr_replication_num" :min="1" :max="5" controls-position="right" style="width:100%" />
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
        </el-card>
      </template>

      <!-- init: 目标端 -->
      <template v-else-if="mode === 'init' && step === 3">
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">目标端 StarRocks</span></template>
          <el-form-item label="自动建表">
            <el-switch v-model="form.auto_create_table" active-text="开" inactive-text="关" />
          </el-form-item>
          <el-form-item :required="!targetOptional" prop="target_instance_id">
            <template #label>
              实例
              <span v-if="targetOptional" class="muted label-opt">（选填）</span>
            </template>
            <el-select v-model="form.target_instance_id" filterable clearable placeholder="搜索 / 选择 StarRocks 实例" style="width:100%">
              <el-option v-for="i in targetInstances" :key="i.id" :label="instLabel(i)" :value="String(i.id)" />
            </el-select>
            <div v-if="targetOptional" class="form-extra">手动 DDL 模式：可不填目标端，后续自行建表</div>
          </el-form-item>
          <el-form-item :required="!targetOptional" prop="target_db">
            <template #label>
              目标数据库
              <span v-if="targetOptional" class="muted label-opt">（选填）</span>
            </template>
            <el-input v-model="form.target_db" placeholder="例：cdc_prod_order" />
          </el-form-item>
          <el-form-item label="表模型" prop="key_model">
            <el-radio-group v-model="form.key_model" class="key-models">
              <div v-for="m in KEY_MODELS" :key="m.value" class="key-opt">
                <div class="key-opt-row">
                  <el-radio :value="m.value">
                    {{ m.label }}
                    <el-tag v-if="m.recommended" size="small" type="primary" class="ml">推荐</el-tag>
                  </el-radio>
                  <el-popover placement="right" :width="360" trigger="click">
                    <template #reference>
                      <el-button text type="primary" size="small">说明</el-button>
                    </template>
                    <div class="key-pop">
                      <div><b>核心机制</b><div>{{ m.mechanism }}</div></div>
                      <div class="mt"><b>适用场景</b><div>{{ m.scenarios }}</div></div>
                      <div class="mt"><b>关键词</b>
                        <div class="key-tags">
                          <el-tag v-for="k in m.keywords" :key="k" size="small" type="info">{{ k }}</el-tag>
                        </div>
                      </div>
                      <div class="mt"><b>KEY 列要求</b><div>{{ m.keyColumnHint }}</div></div>
                    </div>
                  </el-popover>
                </div>
                <div class="opt-sub">{{ m.hint }}</div>
              </div>
            </el-radio-group>
          </el-form-item>
          <div class="dp-block">
            <div class="infra-title">动态分区（StarRocks）</div>
            <el-form-item label="启用动态分区">
              <el-switch v-model="form.sr_dynamic_partition_enable" :disabled="targetOptional" active-text="开" inactive-text="关" />
              <div class="form-extra">关闭时分区键与参数不会写入 DDL；仅创建任务时生效，创建后不可修改。</div>
            </el-form-item>
            <template v-if="form.sr_dynamic_partition_enable">
              <el-form-item>
                <template #label>
                  任务默认分区键
                  <el-tooltip content="多表使用同一日期列时只填此项；单表不同列请在「选择表」步骤按表指定" placement="top">
                    <span class="qmark">?</span>
                  </el-tooltip>
                </template>
                <el-input v-model="form.sr_partition_column" placeholder="例：report_date、created_at" />
              </el-form-item>
              <div class="form-extra mb">以下参数可选，未填则使用平台默认（time_unit={{ dpDefaults['dynamic_partition.time_unit'] }}，start={{ dpDefaults['dynamic_partition.start'] }}，end={{ dpDefaults['dynamic_partition.end'] }}，prefix={{ dpDefaults['dynamic_partition.prefix'] }}）</div>
              <el-form-item label="time_unit">
                <el-select v-model="form.sr_dp_time_unit" clearable :placeholder="`默认 ${dpDefaults['dynamic_partition.time_unit']}`" style="width:100%">
                  <el-option v-for="u in SR_DP_TIME_UNITS" :key="u" :label="u" :value="u" />
                </el-select>
              </el-form-item>
              <el-form-item label="start（保留历史分区数，负整数）">
                <el-input v-model="form.sr_dp_start" :placeholder="dpDefaults['dynamic_partition.start']" />
              </el-form-item>
              <el-form-item label="end（预创建未来分区数）">
                <el-input v-model="form.sr_dp_end" :placeholder="dpDefaults['dynamic_partition.end']" />
              </el-form-item>
              <el-form-item label="prefix">
                <el-input v-model="form.sr_dp_prefix" :placeholder="dpDefaults['dynamic_partition.prefix']" />
              </el-form-item>
              <el-alert type="info" show-icon :closable="false" title="分区列" description="请在「选择表」步骤为每张映射选择分区列；留空则使用上方任务默认分区键。" />
            </template>
          </div>
        </el-card>
      </template>

      <!-- init: 检测 -->
      <template v-else-if="mode === 'init' && step === 4">
        <el-alert type="info" show-icon :closable="false" class="mb" title="创建前检测" description="将按当前向导配置调用预检接口，校验 PG / Debezium / Kafka / StarRocks 等前置条件。全部必选项（level=required）须为 passed 且 ready=true 后，方可进入「SQL 预览」。" />
        <el-card shadow="never" class="cdc-card small">
          <template #header>
            <div class="card-head">
              <span class="card-title">检测结果</span>
              <el-button type="primary" :loading="prechecking" @click="runPrecheck">重新检测</el-button>
            </div>
          </template>
          <div v-if="precheckMsg" class="precheck-box" :class="precheckOk ? 'ok' : 'bad'">{{ precheckMsg }}</div>
          <el-table v-if="precheckChecks.length" :data="precheckChecks" size="small" class="mt" row-class-name="precheck-row">
            <el-table-column prop="name" label="检查项" width="160" />
            <el-table-column label="级别" width="90">
              <template #default="{ row }">{{ row.level || '—' }}</template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="row.status === 'passed' ? 'success' : (row.status === 'failed' ? 'danger' : 'warning')">{{ row.status }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="message" label="说明" min-width="240" show-overflow-tooltip />
          </el-table>
        </el-card>
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">命名（将用于创建）</span></template>
          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="Publication">
                <el-input v-model="form.publication_name" maxlength="128" class="mono" placeholder="例：test_db_dba（环境_库名）" @change="onNamingEdited" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="Slot">
                <el-input v-model="form.slot_name" maxlength="128" class="mono" placeholder="例：slot_test_db_dba_a1b2c3" @change="onNamingEdited" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="Connector">
                <el-input v-model="form.connector_name" maxlength="128" class="mono" placeholder="例：test_db_dba" @change="onNamingEdited" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="Topic 前缀">
                <el-input v-model="form.topic_prefix" maxlength="128" class="mono" placeholder="例：test_db_dba" @change="onNamingEdited" />
              </el-form-item>
            </el-col>
          </el-row>
          <div class="form-extra">未填写时 Publication / Connector / Topic 前缀默认为「环境_库名」；Slot 默认为 slot_环境_库名_6位随机hex。输入框中的值为预检与创建的提交依据；result.naming 仅作首次建议。重新检测按当前输入校验且不会重置为默认名。Topic 前缀不能包含英文句点。</div>
        </el-card>
      </template>

      <!-- init / add_table: SQL 预览 -->
      <template v-else-if="isSqlStep">
        <el-alert type="info" show-icon :closable="false" class="mb" title="目标端 SQL 预览（编排时将执行）" description="可直接改 DDL / Routine Load；提交时会带上 sql_overrides，审批通过后按你改过的 SQL 执行。若 Routine Load 的 kafka_topic 与当前 Topic 前缀不一致，后端会忽略该覆盖。" />
        <el-card v-for="item in sqlPreviews" :key="item.table" shadow="never" class="cdc-card small">
          <template #header><span class="card-title">{{ item.source }} → {{ item.table }}</span></template>
          <el-form-item label="建表 DDL">
            <el-input v-model="item.ddl" type="textarea" :rows="8" class="sql-box" />
          </el-form-item>
          <el-form-item label="Routine Load">
            <el-input v-model="item.load" type="textarea" :rows="6" class="sql-box" />
          </el-form-item>
        </el-card>
        <el-card v-if="debeziumPreview" shadow="never" class="cdc-card small">
          <template #header>
            <span class="card-title">Debezium Connector（{{ debeziumPreview.template || form.debezium_mode }}）</span>
          </template>
          <el-alert
            v-for="(n, i) in (debeziumPreview.notes || [])"
            :key="i"
            type="info"
            :closable="false"
            :title="n"
            class="mb-sm"
          />
          <el-input :model-value="debeziumConfigText" type="textarea" :rows="18" readonly class="sql-box" />
        </el-card>
      </template>

      <!-- init / add_table / alter: 确认 -->
      <template v-else-if="isConfirmStep">
        <el-alert type="success" show-icon :closable="false" class="mb" title="请确认配置后提交审批" description="初始化同步、新增同步表、源表加列、移除同步表创建后都须审批通过才会执行。可在列表「待审核」或工单中心查看。提交后超过 8 小时未审核完成，系统将自动取消。" />
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">任务信息</span></template>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="任务名称">{{ form.name || '—' }}</el-descriptions-item>
            <el-descriptions-item label="环境"><el-tag size="small" :type="envTag(form.env)">{{ form.env || '—' }}</el-tag></el-descriptions-item>
            <el-descriptions-item label="任务描述" :span="2">{{ form.description || '—' }}</el-descriptions-item>
            <el-descriptions-item v-if="needParent && selectedParents.length > 1" label="父任务" :span="2">{{ selectedParents.map((p) => p.name).join('、') }}</el-descriptions-item>
            <el-descriptions-item v-else-if="needParent" label="父任务" :span="2">{{ parentName }}</el-descriptions-item>
            <el-descriptions-item label="审核人" :span="2">
              <el-select v-model="form.reviewer" filterable allow-create placeholder="审批通过后执行" style="width:320px">
                <el-option v-for="a in reviewerOptions" :key="a" :label="a" :value="a" />
              </el-select>
            </el-descriptions-item>
          </el-descriptions>
        </el-card>
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">源端 / 目标端</span></template>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="PG 实例">{{ instLabel(sourceInst) }}</el-descriptions-item>
            <el-descriptions-item label="StarRocks 实例">{{ form.target_instance_id ? instLabel(targetInst) : (form.auto_create_table ? '—' : '未填写（手动 DDL）') }}</el-descriptions-item>
            <el-descriptions-item label="源库 / Schema">{{ sourceScopeLabel }} / {{ form.source_schema || 'public' }}</el-descriptions-item>
            <el-descriptions-item label="目标库">{{ form.target_db || (form.auto_create_table ? '—' : '未填写（手动 DDL）') }}</el-descriptions-item>
            <el-descriptions-item label="自动建表">{{ form.auto_create_table ? '是' : '否（手动 DDL）' }}</el-descriptions-item>
            <el-descriptions-item label="表模型">{{ keyModelLabel(form.key_model) }}</el-descriptions-item>
            <el-descriptions-item label="动态分区">{{ form.sr_dynamic_partition_enable ? '已开启' : '未开启' }}</el-descriptions-item>
            <el-descriptions-item v-if="form.sr_dynamic_partition_enable" label="任务默认分区键">{{ form.sr_partition_column || '—' }}</el-descriptions-item>
            <el-descriptions-item v-if="form.sr_dynamic_partition_enable" label="dynamic_partition 参数" :span="2">{{ formatSrDynamicPartitionDisplay(buildSrDynamicPartitionObject(form)) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
        <el-card v-if="mode === 'init'" shadow="never" class="cdc-card small">
          <template #header><span class="card-title">Debezium 配置</span></template>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="Debezium 集群 ID">{{ form.connector_id || '—' }}</el-descriptions-item>
            <el-descriptions-item label="Debezium 地址">{{ selectedConnector?.dbz_host || '—' }}</el-descriptions-item>
            <el-descriptions-item label="Kafka 消息格式">{{ form.message_schema_mode === 'with_schema' ? '带 Schema' : '不带 Schema' }}</el-descriptions-item>
            <el-descriptions-item label="Kafka 地址" :span="2">{{ form.kafka_bootstrap_servers ? `${normalizeKafkaBootstrap(form.kafka_bootstrap_servers)}（覆盖）` : (connectorKafkaHost || '—（由连接器配置解析）') }}</el-descriptions-item>
            <el-descriptions-item label="Debezium 模式">{{ form.debezium_mode === 'before_after' ? 'Before + After' : '仅 After（推荐）' }}</el-descriptions-item>
            <el-descriptions-item label="同步模式">{{ snapshotLabel(form.snapshot_mode) }}</el-descriptions-item>
            <el-descriptions-item label="过滤删除事件">{{ form.filter_delete_events ? '开启' : '关闭' }}</el-descriptions-item>
            <el-descriptions-item label="Routine Load 并发">{{ form.routine_load_parallelism }} <span class="muted">（当前后端全局参数生效，此处仅作记录）</span></el-descriptions-item>
            <el-descriptions-item label="Kafka Topic 分区数">{{ form.kafka_topic_partitions }}</el-descriptions-item>
            <el-descriptions-item label="Kafka Topic 副本数">{{ form.kafka_topic_replication_factor }}</el-descriptions-item>
            <el-descriptions-item label="StarRocks BUCKETS">{{ form.sr_auto_buckets }}</el-descriptions-item>
            <el-descriptions-item label="StarRocks 表副本数">{{ form.sr_replication_num }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
        <el-card v-if="mode === 'init'" shadow="never" class="cdc-card small">
          <template #header>
            <div class="card-head">
              <span class="card-title">命名预览（将用于创建）</span>
              <span class="muted">与任务名称无关；修改请返回「检测」步骤</span>
            </div>
          </template>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="Publication">{{ form.publication_name || '—' }}</el-descriptions-item>
            <el-descriptions-item label="Slot">{{ form.slot_name || '—' }}</el-descriptions-item>
            <el-descriptions-item label="Connector">{{ form.connector_name || '—' }}</el-descriptions-item>
            <el-descriptions-item label="Topic 前缀">{{ form.topic_prefix || '—' }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
        <el-card v-if="mappings.length" shadow="never" class="cdc-card small">
          <template #header>
            <div class="card-head">
              <span class="card-title">表映射摘要</span>
              <span class="muted">共 {{ mappings.length }} 组（单表 {{ mappings.filter((m) => !m.is_shard_group).length }} / 分表 {{ mappings.filter((m) => m.is_shard_group).length }}）</span>
            </div>
          </template>
          <div class="map-summary">
            <div v-for="m in mappings" :key="m.key" class="map-summary-row">
              <el-tag size="small" :type="m.is_shard_group ? 'warning' : ''">{{ m.is_shard_group ? `分表 ${m.source_tables.length}` : '单表' }}</el-tag>
              <code>{{ m.is_shard_group ? (m.shard_pattern || m.source_tables[0]) : m.source_tables[0] }}</code>
              <span class="map-arrow">→</span>
              <code>{{ m.target_table }}</code>
              <span class="muted">KEY {{ m.key_columns?.length ? m.key_columns.join(', ') : '未配置' }}</span>
              <span v-if="m.shard_keys?.length" class="muted">· 分桶键：{{ m.shard_keys.join(', ') }}</span>
              <span v-if="form.sr_dynamic_partition_enable" class="muted">· 生效分区列：{{ resolveEffectivePartitionColumn(form.sr_partition_column, m.partition_column) || '—' }}</span>
              <span class="muted">· {{ syncTableModeLabel(m.sync_table_mode) }}</span>
            </div>
          </div>
        </el-card>
        <el-card v-if="mode === 'alter_schema' && migrateSQL" shadow="never" class="cdc-card small">
          <template #header><span class="card-title">迁移 SQL（PAUSE / STOP / ALTER / 重建 RL）</span></template>
          <el-input :model-value="migrateSQL" type="textarea" :rows="10" readonly class="sql-box" />
        </el-card>
        <el-card v-if="mode === 'alter_schema'" shadow="never" class="cdc-card small">
          <template #header><span class="card-title">加列摘要</span></template>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="执行">{{ form.alter_schema_exec_mode === 'auto' ? '自动执行' : '仅生成 SQL' }}</el-descriptions-item>
            <el-descriptions-item v-for="it in alterDiffItems" :key="it.target_table" :label="it.target_table">
              {{ (it.columns_to_add || []).filter((c) => c.selected).map((c) => `${c.name} ${c.sr_type}`).join('，') || '无待加列' }}
            </el-descriptions-item>
          </el-descriptions>
        </el-card>
        <el-card v-if="mode === 'remove_table'" shadow="never" class="cdc-card small">
          <template #header><span class="card-title">将移除的表</span></template>
          <div class="shard-db-tags">
            <el-tag v-for="t in form.tables" :key="t" type="warning">{{ t }}</el-tag>
          </div>
        </el-card>
      </template>

      <!-- add_table / alter / remove: 选择已有任务 -->
      <template v-else-if="needParent && step === 0">
        <el-card shadow="never" class="cdc-card small">
          <template #header>
            <span class="card-title">{{ (form.task_type === 'add_table' || form.task_type === 'alter_schema') ? '选择已有初始化任务（可多选同一源端）' : '选择已有初始化任务' }}</span>
          </template>
          <el-form-item label="环境">
            <el-select v-model="form.env" clearable filterable placeholder="全部" style="width:220px">
              <el-option v-for="e in envOptions" :key="e.value" :label="e.label" :value="e.value" />
            </el-select>
          </el-form-item>
          <el-alert v-if="form.task_type === 'add_table' || form.task_type === 'alter_schema'" type="info" show-icon :closable="false" class="mb" title="批量父任务" :description="form.task_type === 'alter_schema' ? '可勾选同一源端实例下的多个 init 任务；提交后共享一张工单，每个父任务各生成一条加列子任务。' : '可勾选同一源端实例下的多个 init 任务；提交后共享一张工单，每个父任务各生成一条加表子任务，已同步的表会自动跳过。'" />
          <el-table :data="parentCandidates" highlight-current-row @current-change="onParentRowClick" empty-text="暂无可用的初始化任务">
            <el-table-column width="48">
              <template #default="{ row }">
                <el-checkbox v-if="form.task_type === 'add_table' || form.task_type === 'alter_schema'" :model-value="form.parent_ids.includes(row.id)" @change="(v) => toggleParent(row, v)" />
                <el-radio v-else :model-value="form.parent_id" :value="row.id" @change="onPickParent(row)">&nbsp;</el-radio>
              </template>
            </el-table-column>
            <el-table-column prop="name" label="任务名称" min-width="180" show-overflow-tooltip />
            <el-table-column label="源端" min-width="160" show-overflow-tooltip>
              <template #default="{ row }">{{ row.source_db || '—' }}</template>
            </el-table-column>
            <el-table-column label="目标端" min-width="160" show-overflow-tooltip>
              <template #default="{ row }">{{ row.target_db || '—' }}</template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="statusTag(row.status)">{{ statusText(row.status) }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </template>

      <!-- add_table: 选择新增表 -->
      <template v-else-if="mode === 'add_table' && step === 1">
        <el-alert type="info" show-icon :closable="false" class="mb" title="多张 PG 分表 → 1 张 StarRocks 目标表" description="点击「分表匹配」批量归并；勾选单表则一对一映射。" />
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="cdc-card map-card">
              <template #header>
                <div class="card-head">
                  <span class="card-title">源端表列表</span>
                  <el-button text type="primary" @click="shardOpen = true">分表匹配</el-button>
                </div>
              </template>
              <div class="tbl-bar">
                <el-input v-model="tableQ" clearable placeholder="搜索表名" style="flex:1">
                  <template #prefix><el-icon><Search /></el-icon></template>
                </el-input>
                <el-checkbox :model-value="allSelectableChecked" :indeterminate="allSelectableIndeterminate" @change="toggleAllSelectable">全选可选（{{ selectableCount }}）</el-checkbox>
                <el-switch v-model="groupShards" inline-prompt active-text="分表归类" inactive-text="平铺" />
                <el-button :loading="loadingTables" @click="loadTables">刷新</el-button>
              </div>
              <div class="src-list">
                <div v-for="g in visibleShardGroups" :key="'ag-' + g.base" class="src-row">
                  <el-checkbox :model-value="isGroupSelected(g)" @change="(v) => toggleGroup(g, v)">
                    <span class="src-name">{{ g.targetTable || g.base }}</span>
                    <el-tag size="small" type="warning">分表 {{ g.members.length }}</el-tag>
                  </el-checkbox>
                </div>
                <div v-for="t in visibleSingleTables" :key="'a-' + t" class="src-row">
                  <el-checkbox :model-value="isTableSelected(t)" :disabled="syncedInAllParents.has(t.toLowerCase())" @change="(v) => toggleSingle(t, v)">
                    {{ t }}
                    <el-tag v-if="syncedInAllParents.has(t.toLowerCase())" size="small" type="info">全部任务已同步</el-tag>
                    <el-tag v-else-if="countTasksSyncedTable(t, selectedParents)" size="small">已在 {{ countTasksSyncedTable(t, selectedParents) }} 个任务同步</el-tag>
                  </el-checkbox>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="cdc-card map-card">
              <template #header>
                <div class="card-head">
                  <span class="card-title">已选同步映射</span>
                  <span class="muted">共 {{ mappings.length }} 组</span>
                </div>
              </template>
              <CdcTableMappingPanel
                v-model:mappings="mappings"
                :instance-id="form.source_instance_id"
                :db-name="form.source_db"
                :schema="form.source_schema || 'public'"
                :partition-enabled="form.sr_dynamic_partition_enable"
                :task-partition-column="form.sr_partition_column"
                :shard-db-names="resolvedShardDbs"
                :show-sync-mode="form.task_type === 'add_table'"
              />
            </el-card>
          </el-col>
        </el-row>
      </template>

      <!-- add_table: 配置目标端 -->
      <template v-else-if="mode === 'add_table' && step === 2">
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">配置目标端</span></template>
          <el-form-item label="自动建表">
            <el-switch v-model="form.auto_create_table" active-text="开" inactive-text="关" />
          </el-form-item>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="继承目标实例">{{ instLabel(targetInst) }}</el-descriptions-item>
            <el-descriptions-item label="目标库">{{ form.target_db || '—' }}</el-descriptions-item>
            <el-descriptions-item label="表模型">{{ keyModelLabel(form.key_model) }}</el-descriptions-item>
            <el-descriptions-item label="动态分区">{{ form.sr_dynamic_partition_enable ? '已开启（继承）' : '未开启' }}</el-descriptions-item>
            <el-descriptions-item v-if="form.sr_dynamic_partition_enable" label="分区键">{{ form.sr_partition_column || '—' }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
      </template>

      <!-- alter: 列级 diff -->
      <template v-else-if="mode === 'alter_schema' && step === 1">
        <el-alert type="info" show-icon :closable="false" class="mb" title="列级差异" description="左侧选择目标映射，右侧对比「PG 有而 StarRocks 无」的非主键列。连不上目标端时仍会列出 PG 候选列。" />
        <el-row :gutter="16">
          <el-col :xs="24" :md="8">
            <el-card shadow="never" class="cdc-card map-card">
              <template #header><span class="card-title">目标映射</span></template>
              <div class="src-list">
                <div
                  v-for="m in parentMappings"
                  :key="m.target_table"
                  class="src-row alter-map"
                  :class="{ active: form.tables.includes(m.target_table) }"
                  @click="toggleAlterMapping(m)"
                >
                  <el-checkbox :model-value="form.tables.includes(m.target_table)" @change="() => toggleAlterMapping(m)" @click.stop>
                    <span class="src-name">{{ m.target_table }}</span>
                    <div class="muted">{{ (m.source_tables || []).join(', ') }}</div>
                  </el-checkbox>
                </div>
                <div v-if="!parentMappings.length" class="tree-empty">父任务暂无映射</div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="24" :md="16">
            <el-card shadow="never" class="cdc-card map-card">
              <template #header>
                <div class="card-head">
                  <span class="card-title">待加列</span>
                  <el-button text type="primary" :loading="diffLoading" @click="loadSchemaDiff">重新对比</el-button>
                </div>
              </template>
              <el-alert v-if="diffWarning" type="warning" show-icon :closable="false" class="mb" :title="diffWarning" />
              <el-table :data="alterDiffRows" size="small" empty-text="请选择映射后自动对比">
                <el-table-column width="48">
                  <template #default="{ row }">
                    <el-checkbox v-model="row.selected" />
                  </template>
                </el-table-column>
                <el-table-column prop="target_table" label="目标表" min-width="120" />
                <el-table-column prop="name" label="列名" min-width="120" />
                <el-table-column prop="pg_type" label="PG 类型" width="120" />
                <el-table-column prop="sr_type" label="SR 类型" width="140" />
              </el-table>
            </el-card>
          </el-col>
        </el-row>
      </template>

      <!-- remove: 选择移除表 -->
      <template v-else-if="mode === 'remove_table' && step === 1">
        <el-alert type="warning" show-icon :closable="false" class="mb" title="将从 Publication 移除所选表" description="审批通过后会 ALTER PUBLICATION DROP TABLE，并收缩 Connector table.include.list。不会删除 StarRocks 目标表。" />
        <el-card shadow="never" class="cdc-card small">
          <template #header><span class="card-title">选择要移除的同步表</span></template>
          <el-checkbox-group v-model="form.tables" class="tbl-list">
            <el-checkbox v-for="t in parentTables" :key="t" :value="t" :label="t">{{ t }}</el-checkbox>
          </el-checkbox-group>
        </el-card>
      </template>

      <!-- alter: 执行方式 -->
      <template v-else-if="mode === 'alter_schema' && step === 2">
        <el-alert type="info" show-icon :closable="false" class="mb" title="PostgreSQL 前置" description="请确认已在 PostgreSQL 对所有相关源表执行 ADD COLUMN。本系统不会在源库加列。" />
        <el-card shadow="never" class="cdc-card small">
          <el-form-item>
            <el-checkbox v-model="form.pg_column_ack">我已在 PostgreSQL 完成 ADD COLUMN</el-checkbox>
          </el-form-item>
          <el-form-item label="执行方式">
            <el-radio-group v-model="form.alter_schema_exec_mode" class="col-radios">
              <el-radio value="sql_only">仅生成 SQL（下一步预览后手动执行）</el-radio>
              <el-radio value="auto" :disabled="!canAutoAlter">自动执行 alter_schema</el-radio>
            </el-radio-group>
            <div v-if="!canAutoAlter" class="form-extra">父任务未开启自动建表或未配置目标端，只能生成 SQL。</div>
          </el-form-item>
          <el-form-item v-if="form.alter_schema_exec_mode === 'auto'" label="重建 Routine Load 位点">
            <el-radio-group v-model="form.kafka_default_offsets">
              <el-radio value="OFFSET_BEGINNING">OFFSET_BEGINNING（从头消费）</el-radio>
              <el-radio value="OFFSET_END">OFFSET_END（从最新位点）</el-radio>
            </el-radio-group>
          </el-form-item>
        </el-card>
      </template>
    </el-form>

    <el-dialog v-model="shardOpen" title="分表匹配" width="560px" append-to-body destroy-on-close>
      <el-alert type="info" show-icon :closable="false" class="mb" title="多张 PG 分表 → 1 张 StarRocks 目标表" description="匹配规则将归并为同一映射组写入一张目标表。" />
      <el-form label-position="top">
        <el-form-item label="匹配类型">
          <el-radio-group v-model="shardPatternType">
            <el-radio value="glob">glob（默认，尾部 * 按数字分表收窄）</el-radio>
            <el-radio value="regex">regex</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="shardPatternType === 'regex' ? '匹配规则（regex）' : '匹配规则（glob）'">
          <el-input v-model="shardPattern" :placeholder="shardPatternType === 'regex' ? '如 ^tv_app_market_[0-9]+$' : '如 tv_app_market_*'" @input="scheduleDiscover" />
        </el-form-item>
        <el-form-item v-if="resolvedShardDbs.length > 1" label="匹配范围">
          <el-radio-group v-model="shardDbScope">
            <el-radio value="all">全部分库</el-radio>
            <el-radio value="representative">仅代表库 {{ form.source_db }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="目标表名">
          <el-input v-model="shardTarget" placeholder="默认同规则去掉 *" />
        </el-form-item>
        <div class="muted">{{ discoverLoading ? '正在匹配…' : `匹配到 ${shardPreview.length} 张：${shardPreview.slice(0, 8).join(', ')}${shardPreview.length > 8 ? '…' : ''}` }}</div>
      </el-form>
      <template #footer>
        <el-button @click="shardOpen = false">取消</el-button>
        <el-button type="primary" :disabled="!shardPreview.length" @click="confirmShard">加入映射</el-button>
      </template>
    </el-dialog>

    <div :class="['cdc-nav', isWideStep ? 'cdc-nav-wide' : '']">
      <el-button @click="goList">取消</el-button>
      <div class="cdc-nav-right">
        <el-button v-if="mode === 'init'" @click="saveDraft">保存草稿</el-button>
        <el-button v-if="step > 0" @click="prev">上一步</el-button>
        <el-button v-if="step < steps.length - 1" type="primary" :disabled="nextDisabled" @click="next">下一步</el-button>
        <el-button v-else-if="mode === 'alter_schema' && form.alter_schema_exec_mode === 'sql_only'" @click="goList">返回列表</el-button>
        <el-button v-else type="primary" :loading="submitting" :disabled="!form.reviewer" @click="submit">{{ mode === 'init' ? '确认创建' : '确认提交' }}</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import api from '../../api/client'
import CdcTableMappingPanel from './CdcTableMappingPanel.vue'
import { useWorkOrderDraft } from '../../composables/useWorkOrderDraft'
import './cdc.css'
import {
  CDC_DYNAMIC_PARTITION_DEFAULTS,
  CDC_INFRA_DEFAULTS,
  KEY_MODELS,
  PAGE_HINTS,
  PAGE_TITLES,
  SR_DP_TIME_UNITS,
  buildSqlPreview,
  buildSrDynamicPartitionObject,
  formatDebeziumConfig,
  formatSourceScopeLabel,
  formatSrDynamicPartitionDisplay,
  decodeCdcExtra,
  envTag,
  filterShardDatabases,
  groupShardTables,
  instEnv,
  instLabel,
  normalizeKafkaBootstrap,
  isMultiDbScope,
  keyModelLabel,
  matchGlob, // fallback when discover API unavailable
  parentTablesOf,
  countTasksSyncedTable,
  positiveInt,
  resolveEffectivePartitionColumn,
  resolveSourceScope,
  shardDbPrefixHint,
  snapshotLabel,
  syncTableModeLabel,
  normalizeSyncTableMode,
  statusTag,
  statusText,
  syncTablesFromMappings,
  tablesSyncedInAllParents,
  typeTag,
  typeText,
  validateAddTableParents,
  validatePartitionColumnsForMappings,
  collectSqlOverrides,
} from './cdc'

const INIT_STEPS = ['选择源端', '选择表', '配置 Debezium', '配置目标端', '检测', 'SQL 预览', '确认并提交审批']
const ADD_STEPS_AUTO = ['选择已有任务', '选择新增表', '配置目标端', 'SQL 预览', '确认并提交审批']
const ADD_STEPS = ['选择已有任务', '选择新增表', '配置目标端', '确认并提交审批']
const ALTER_STEPS = ['选择已有任务', '选择目标表', '执行方式', '确认并提交审批']
const REMOVE_STEPS = ['选择已有任务', '选择移除表', '确认并提交审批']

const route = useRoute()
const router = useRouter()
const formRef = ref()
const step = ref(0)
const submitting = ref(false)
const prechecking = ref(false)
const precheckOk = ref(false)
const precheckMsg = ref('')
const precheckChecks = ref([])
const dpDefaults = CDC_DYNAMIC_PARTITION_DEFAULTS
const loadingDbs = ref(false)
const loadingTables = ref(false)
const tableHint = ref('')
const tableQ = ref('')
const manualTables = ref('')
const sqlPreviews = ref([])
const debeziumPreview = ref(null)
const instances = ref([])
const envOptions = ref([])
const connectors = ref([])
const parentTasks = ref([])
const sourceDbs = ref([])
const tables = ref([])
const registeredDbs = ref([])
const defaultReviewers = ref([])
const mappings = ref([])
const groupShards = ref(true)
const expandedGroups = ref(new Set())
const shardOpen = ref(false)
const shardPattern = ref('')
const shardPatternType = ref('glob')
const shardDbScope = ref('all')
const parentMappingRows = ref([])
const syncedByParent = ref({})
const discoverLoading = ref(false)
const discoveredTables = ref([])
let discoverTimer = 0
const shardTarget = ref('')
const diffLoading = ref(false)
const diffWarning = ref('')
const alterDiffItems = ref([])
const migrateSQL = ref('')

const infraDefaults = reactive({ ...CDC_INFRA_DEFAULTS })
const form = reactive(emptyForm())
const targetOptional = computed(() => form.auto_create_table === false)

const mode = computed(() => form.task_type)
const pageTitle = computed(() => PAGE_TITLES[form.task_type] || '创建 CDC 任务')
const pageHint = computed(() => PAGE_HINTS[form.task_type] || '')
const needParent = computed(() => form.task_type === 'add_table' || form.task_type === 'alter_schema' || form.task_type === 'remove_table')
const steps = computed(() => {
  if (form.task_type === 'alter_schema') return ALTER_STEPS
  if (form.task_type === 'remove_table') return REMOVE_STEPS
  if (form.task_type === 'add_table') return form.auto_create_table ? ADD_STEPS_AUTO : ADD_STEPS
  return INIT_STEPS
})
const isSqlStep = computed(() => {
  if (form.task_type === 'init') return step.value === 5
  if (form.task_type === 'add_table') return form.auto_create_table && step.value === 3
  return false
})
const isConfirmStep = computed(() => step.value === steps.value.length - 1)
const debeziumConfigText = computed(() => formatDebeziumConfig(debeziumPreview.value?.config))
const isWideStep = computed(() => (form.task_type === 'init' && step.value === 1) || (form.task_type === 'add_table' && step.value === 1) || (form.task_type === 'alter_schema' && step.value === 1))
const nextDisabled = computed(() => form.task_type === 'init' && step.value === 4 && !precheckOk.value)
const shardBundles = computed(() => groupShardTables(tables.value))
const visibleShardGroups = computed(() => {
  if (!groupShards.value) return []
  const q = tableQ.value.trim().toLowerCase()
  return shardBundles.value.groups.filter((g) => !q || g.base.toLowerCase().includes(q) || g.members.some((n) => n.toLowerCase().includes(q)))
})
const visibleSingleTables = computed(() => {
  const q = tableQ.value.trim().toLowerCase()
  const names = groupShards.value ? shardBundles.value.singles : tables.value
  return names.filter((t) => !q || t.toLowerCase().includes(q))
})
const shardPreview = computed(() => {
  if (discoveredTables.value.length || discoverLoading.value) return discoveredTables.value
  if (shardPatternType.value === 'regex') return []
  return tables.value.filter((t) => matchGlob(shardPattern.value, t))
})

const sourceInst = computed(() => instances.value.find((i) => String(i.id) === instanceID(form.source_instance_id)))
const targetInst = computed(() => instances.value.find((i) => String(i.id) === instanceID(form.target_instance_id)))
const parentName = computed(() => parentTasks.value.find((t) => t.id === form.parent_id)?.name || form.parent_id || '—')
const selectedParents = computed(() => {
  const multi = form.task_type === 'add_table' || form.task_type === 'alter_schema'
  const ids = multi && form.parent_ids.length ? form.parent_ids : (form.parent_id ? [form.parent_id] : [])
  return parentTasks.value.filter((t) => ids.includes(t.id))
})
const parentTables = computed(() => {
  const p = selectedParents.value[0] || parentTasks.value.find((t) => t.id === form.parent_id)
  return parentTablesOf(p)
})
const parentMappings = computed(() => {
  const live = (parentMappingRows.value || []).filter((m) => m.status !== 'deleted')
  if (live.length) return live
  const p = selectedParents.value[0] || parentTasks.value.find((t) => t.id === form.parent_id)
  const extra = decodeCdcExtra(p?.description).extra
  const maps = extra.mappings || []
  if (maps.length) return maps
  return parentTables.value.map((t) => ({ target_table: extra.table_map?.[t] || t, source_tables: [t] }))
})
const canAutoAlter = computed(() => {
  const p = selectedParents.value[0] || parentTasks.value.find((t) => t.id === form.parent_id)
  if (!p) return false
  const extra = decodeCdcExtra(p.description).extra
  return extra.auto_create_table !== false && !!(p.target_instance_id && p.target_db)
})
const syncedInAllParents = computed(() => {
  const ids = selectedParents.value.map((p) => p.id)
  if (!ids.length) return new Set(tablesSyncedInAllParents(selectedParents.value))
  const maps = ids.map((id) => new Set((syncedByParent.value[id] || []).map((n) => String(n).toLowerCase())))
  if (maps.some((s) => !s.size) && !Object.keys(syncedByParent.value).length) {
    return new Set(tablesSyncedInAllParents(selectedParents.value))
  }
  const first = maps[0]
  const out = []
  for (const n of first) {
    if (maps.every((s) => s.has(n))) out.push(n)
  }
  return new Set(out)
})
const resolvedShardDbs = computed(() => {
  if (form.source_scope_mode !== 'multi_db') return form.source_db ? [form.source_db] : []
  return filterShardDatabases(sourceDbs.value, form.source_db_prefix)
})
const scopePrefixHint = computed(() => (form.source_scope_mode === 'multi_db' ? shardDbPrefixHint(form.source_db, form.source_db_prefix) : ''))
const sourceScopeLabel = computed(() => formatSourceScopeLabel(form.source_db, form.source_db_prefix, resolvedShardDbs.value))
const alterDiffRows = computed(() => {
  const rows = []
  for (const it of alterDiffItems.value) {
    for (const c of it.columns_to_add || []) {
      rows.push({ ...c, target_table: it.target_table })
    }
  }
  return rows
})
const parentCandidates = computed(() => {
  return parentTasks.value.filter((t) => {
    if (t.task_type !== 'init') return false
    if (form.task_type === 'add_table' && t.status !== 'running') return false
    if (!['running', 'paused', 'init', 'creating', 'provisioning'].includes(t.status)) return false
    if (!form.env) return true
    const extra = decodeCdcExtra(t.description).extra
    const env = extra.env || instEnv(instances.value.find((i) => i.id === t.source_instance_id))
    return env === form.env
  })
})
const sourceInstances = computed(() => instances.value.filter((i) => matchType(i.db_type, 'postgres') && (!form.env || instEnv(i) === form.env || !instEnv(i))))
const targetInstances = computed(() => {
  const list = instances.value.filter((i) => matchType(i.db_type, 'starrocks') && (!form.env || instEnv(i) === form.env || !instEnv(i)))
  return list.length ? list : instances.value.filter((i) => !matchType(i.db_type, 'postgres') && !matchType(i.db_type, 'kafka'))
})
const filteredConnectors = computed(() => connectors.value.filter((c) => (!form.env || c.env === form.env) && (c.message_schema_mode || 'without_schema') === form.message_schema_mode))
const selectedConnector = computed(() => connectors.value.find((c) => c.id === form.connector_id))
const connectorKafkaHost = computed(() => normalizeKafkaBootstrap(selectedConnector.value?.kafka_host))
const mappingSummary = computed(() => mappings.value.map((m) => `${m.source_tables.join(',')} → ${m.target_table || '?'}`).join('；'))
const reviewerOptions = computed(() => {
  const set = new Set(defaultReviewers.value)
  const inst = sourceInst.value
  for (const a of inst?.approvers || []) if (a) set.add(a)
  const db = registeredDbs.value.find((d) => d.db_name === form.source_db)
  for (const a of db?.approvers || []) if (a) set.add(a)
  return [...set]
})
const filteredTables = computed(() => {
  const q = tableQ.value.trim().toLowerCase()
  if (!q) return tables.value
  return tables.value.filter((t) => t.toLowerCase().includes(q))
})
const selectableCount = computed(() => visibleShardGroups.value.length + visibleSingleTables.value.length)
const selectedSelectableCount = computed(() => (
  visibleShardGroups.value.filter((g) => isGroupSelected(g)).length
  + visibleSingleTables.value.filter((t) => isTableSelected(t)).length
))
const allSelectableChecked = computed(() => selectableCount.value > 0 && selectedSelectableCount.value === selectableCount.value)
const allSelectableIndeterminate = computed(() => selectedSelectableCount.value > 0 && selectedSelectableCount.value < selectableCount.value)

const rules = {
  name: [{ required: true, message: '请输入任务名称', trigger: 'blur' }],
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  source_instance_id: [{ required: true, message: '请选择源端实例', trigger: 'change' }],
  source_db: [{ required: true, message: '请选择数据库', trigger: 'change' }],
  source_schema: [{ required: true, message: '请输入 Schema', trigger: 'change' }],
  connector_id: [{ required: true, message: '请选择 Debezium 集群', trigger: 'change' }],
  debezium_mode: [{ required: true, message: '请选择 Debezium 模式', trigger: 'change' }],
  snapshot_mode: [{ required: true, message: '请选择同步模式', trigger: 'change' }],
  message_schema_mode: [{ required: true, message: '请选择消息格式', trigger: 'change' }],
  key_model: [{ required: true, message: '请选择表模型', trigger: 'change' }],
}

function emptyForm() {
  return {
    name: '',
    description: '',
    task_type: 'init',
    env: '',
    parent_id: '',
    parent_ids: [],
    source_scope_mode: 'single_db',
    source_db_prefix: '',
    source_db_names: [],
    source_instance_id: '',
    target_instance_id: '',
    source_db: '',
    target_db: '',
    source_schema: 'public',
    tables: [],
    table_map: {},
    connector_id: '',
    message_schema_mode: 'without_schema',
    debezium_mode: 'after_only',
    snapshot_mode: 'initial',
    filter_delete_events: false,
    kafka_bootstrap_servers: '',
    routine_load_parallelism: infraDefaults.routine_load_parallelism,
    kafka_topic_partitions: infraDefaults.kafka_topic_partitions,
    kafka_topic_replication_factor: infraDefaults.kafka_topic_replication_factor,
    sr_auto_buckets: infraDefaults.sr_auto_buckets,
    sr_replication_num: infraDefaults.sr_replication_num,
    sr_dynamic_partition_enable: false,
    sr_partition_column: '',
    sr_dp_time_unit: '',
    sr_dp_start: '',
    sr_dp_end: '',
    sr_dp_prefix: '',
    publication_name: '',
    slot_name: '',
    connector_name: '',
    topic_prefix: '',
    auto_create_table: true,
    key_model: 'unique_key',
    alter_schema_exec_mode: 'sql_only',
    kafka_default_offsets: 'OFFSET_BEGINNING',
    alter_columns: '',
    alter_schema_tables: [],
    pg_column_ack: false,
    reviewer: '',
    mappings: [],
  }
}

function applyInfraDefaults(args) {
  const n = (key, fallback) => positiveInt(args.find((a) => a.key === key)?.value, fallback)
  infraDefaults.routine_load_parallelism = n('sync_sr_routine_load_desired_concurrent', CDC_INFRA_DEFAULTS.routine_load_parallelism)
  infraDefaults.kafka_topic_partitions = n('sync_kafka_topic_partitions', CDC_INFRA_DEFAULTS.kafka_topic_partitions)
  infraDefaults.kafka_topic_replication_factor = n('sync_kafka_topic_replication_factor', CDC_INFRA_DEFAULTS.kafka_topic_replication_factor)
  infraDefaults.sr_auto_buckets = n('sync_sr_auto_buckets', CDC_INFRA_DEFAULTS.sr_auto_buckets)
  infraDefaults.sr_replication_num = n('sync_sr_replication_num', CDC_INFRA_DEFAULTS.sr_replication_num)
}

function matchType(a, b) {
  const n = (t) => {
    t = String(t || '').toLowerCase()
    if (['postgresql', 'pg', 'pgsql'].includes(t)) return 'postgres'
    if (t === 'sr') return 'starrocks'
    return t
  }
  return n(a) === n(b)
}

function resetMode(type) {
  const keep = emptyForm()
  keep.task_type = PAGE_TITLES[type] ? type : 'init'
  if (keep.task_type === 'add_table') keep.auto_create_table = false
  Object.assign(form, keep)
  step.value = 0
  precheckOk.value = false
  precheckMsg.value = ''
  precheckChecks.value = []
  tables.value = []
  sourceDbs.value = []
  sqlPreviews.value = []
  debeziumPreview.value = null
  mappings.value = []
  alterDiffItems.value = []
  diffWarning.value = ''
  migrateSQL.value = ''
}

function onNamingEdited() {
  precheckOk.value = false
  precheckMsg.value = '命名已修改，请重新检测'
}

function goList(query) {
  router.push(typeof query === 'object' && query?.tab ? { path: '/ops/cdc', query } : '/ops/cdc')
}

function onEnvChange() {
  form.source_instance_id = ''
  form.target_instance_id = ''
  form.source_db = ''
  form.connector_id = ''
  sourceDbs.value = []
  tables.value = []
  form.tables = []
  mappings.value = []
}

async function onSourceInstChange() {
  form.source_db = ''
  form.tables = []
  tables.value = []
  mappings.value = []
  await loadSourceDbs()
}

function onSourceDbChange() {
  form.tables = []
  tables.value = []
  mappings.value = []
  pickDefaultReviewer()
  syncResolvedShardDbs()
}

function onScopeModeChange() {
  if (form.source_scope_mode !== 'multi_db') {
    form.source_db_prefix = ''
    form.source_db_names = []
  }
  syncResolvedShardDbs()
}

function syncResolvedShardDbs() {
  const scope = resolveSourceScope({
    mode: form.source_scope_mode,
    representativeDb: form.source_db,
    dbPrefix: form.source_db_prefix,
    names: sourceDbs.value,
  })
  form.source_db_names = scope.resolved_db_names
}

async function loadSourceDbs() {
  if (!form.source_instance_id) {
    sourceDbs.value = []
    registeredDbs.value = []
    return
  }
  loadingDbs.value = true
  try {
    const [live, reg] = await Promise.all([
      api.get(`/instances/${form.source_instance_id}/databases`).catch(() => ({ data: { data: [] } })),
      api.get('/databases', { params: { instance_id: form.source_instance_id } }).catch(() => ({ data: { data: [] } })),
    ])
    registeredDbs.value = reg.data.data || []
    sourceDbs.value = (live.data.data || []).filter((d) => !d.system).map((d) => d.name || d.db_name).filter(Boolean)
    if (!sourceDbs.value.length) sourceDbs.value = registeredDbs.value.map((d) => d.db_name).filter(Boolean)
    pickDefaultReviewer()
    syncResolvedShardDbs()
  } catch {
    sourceDbs.value = []
    registeredDbs.value = []
  } finally {
    loadingDbs.value = false
  }
}

function pickDefaultReviewer() {
  if (form.reviewer && reviewerOptions.value.includes(form.reviewer)) return
  form.reviewer = reviewerOptions.value[0] || form.reviewer || ''
}

async function loadTables() {
  if (!form.source_instance_id || !form.source_db) {
    tableHint.value = '请先选择源端实例和数据库'
    return
  }
  loadingTables.value = true
  tableHint.value = ''
  try {
    if (form.task_type === 'add_table' && form.parent_id) {
      const { data } = await api.get(`/cdc-sync/tasks/${form.parent_id}/add-table/available-tables`)
      tables.value = data.data || []
    } else {
      const { data } = await api.get(`/migration/instances/${form.source_instance_id}/databases/${encodeURIComponent(form.source_db)}/tables`)
      tables.value = (data.data || []).map((t) => (typeof t === 'string' ? t : (t.table_name || t.name))).filter(Boolean)
    }
    if (!tables.value.length) tableHint.value = '未拉到表，可在下方手动补充表名'
  } catch (e) {
    tables.value = []
    tableHint.value = e?.response?.data?.message || '无法连接实例拉表，可手动补充表名'
  } finally {
    loadingTables.value = false
  }
}

function selectAllTables() {
  form.tables = [...filteredTables.value]
}

function newMapping({ source_tables, target_table, is_shard_group, shard_pattern }) {
  return {
    key: `m_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
    source_tables: [...source_tables],
    target_table: target_table || source_tables[0] || '',
    key_columns: [],
    shard_keys: [],
    partition_column: '',
    is_shard_group: !!is_shard_group,
    shard_pattern: shard_pattern || '',
    source_db_scope: 'all',
    source_db_names: [],
    sync_table_mode: form.task_type === 'add_table' ? 'incremental' : normalizeSyncTableMode(form.snapshot_mode === 'never' ? 'incremental' : 'initialization'),
    open: false,
  }
}

function syncMappingTables() {
  const synced = syncTablesFromMappings(mappings.value)
  form.tables = synced.tables
  form.table_map = synced.table_map
}

function isTableSelected(name) {
  return mappings.value.some((m) => !m.is_shard_group && m.source_tables.includes(name))
}

function isGroupSelected(g) {
  const set = new Set(g.members)
  return mappings.value.some((m) => m.is_shard_group && m.source_tables.length === g.members.length && m.source_tables.every((t) => set.has(t)))
}

function toggleSingle(name, checked) {
  if (checked) {
    if (isTableSelected(name)) return
    mappings.value = mappings.value.filter((m) => !(m.is_shard_group && m.source_tables.includes(name)))
    mappings.value.push(newMapping({ source_tables: [name], target_table: name, is_shard_group: false }))
  } else {
    mappings.value = mappings.value.filter((m) => !(m.source_tables.length === 1 && m.source_tables[0] === name))
  }
  syncMappingTables()
}

function toggleGroup(g, checked) {
  if (checked) {
    mappings.value = mappings.value.filter((m) => !m.source_tables.some((t) => g.members.includes(t)))
    mappings.value.push(newMapping({
      source_tables: g.members,
      target_table: g.targetTable,
      is_shard_group: true,
      shard_pattern: g.pattern,
    }))
  } else {
    mappings.value = mappings.value.filter((m) => !(m.is_shard_group && m.shard_pattern === g.pattern))
  }
  syncMappingTables()
}

function toggleAllSelectable(checked) {
  if (checked) {
    for (const g of visibleShardGroups.value) {
      if (!isGroupSelected(g)) toggleGroup(g, true)
    }
    for (const t of visibleSingleTables.value) {
      if (!isTableSelected(t)) toggleSingle(t, true)
    }
    return
  }
  const groups = new Set(visibleShardGroups.value.map((g) => g.pattern))
  const singles = new Set(visibleSingleTables.value)
  mappings.value = mappings.value.filter((m) => {
    if (m.is_shard_group && groups.has(m.shard_pattern)) return false
    if (!m.is_shard_group && m.source_tables.length === 1 && singles.has(m.source_tables[0])) return false
    return true
  })
  syncMappingTables()
}

function toggleExpand(base) {
  const next = new Set(expandedGroups.value)
  if (next.has(base)) next.delete(base)
  else next.add(base)
  expandedGroups.value = next
}

function removeMapping(key) {
  mappings.value = mappings.value.filter((m) => m.key !== key)
  syncMappingTables()
}

function scheduleDiscover() {
  clearTimeout(discoverTimer)
  discoverTimer = setTimeout(runDiscover, 280)
}

async function runDiscover() {
  const pattern = shardPattern.value.trim()
  if (!pattern || !instanceID(form.source_instance_id)) {
    discoveredTables.value = []
    return
  }
  const dbs = shardDbScope.value === 'representative' || !resolvedShardDbs.value.length
    ? (form.source_db ? [form.source_db] : [])
    : resolvedShardDbs.value
  if (!dbs.length) {
    discoveredTables.value = tables.value.filter((t) => shardPatternType.value === 'regex' ? false : matchGlob(pattern, t))
    return
  }
  discoverLoading.value = true
  try {
    const { data } = await api.post('/cdc-sync/discover/tables', {
      instance_id: instanceID(form.source_instance_id),
      db_name: form.source_db,
      db_names: dbs,
      schema: form.source_schema || 'public',
      pattern,
      pattern_type: shardPatternType.value,
    })
    discoveredTables.value = data.data?.tables || []
  } catch {
    discoveredTables.value = shardPatternType.value === 'regex' ? [] : tables.value.filter((t) => matchGlob(pattern, t))
  } finally {
    discoverLoading.value = false
  }
}

function confirmShard() {
  const members = shardPreview.value
  if (!members.length) return
  const target = shardTarget.value.trim() || String(shardPattern.value).replace(/\*+$/, '').replace(/_+$/, '') || members[0]
  mappings.value = mappings.value.filter((m) => !m.source_tables.some((t) => members.includes(t)))
  mappings.value.push(newMapping({
    source_tables: members,
    target_table: target,
    is_shard_group: true,
    shard_pattern: shardPattern.value.trim(),
  }))
  syncMappingTables()
  shardOpen.value = false
  shardPattern.value = ''
  shardTarget.value = ''
}

function restoreMappings(raw) {
  const list = Array.isArray(raw) ? raw : []
  mappings.value = list.map((m) => ({
    key: m.key || `m_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
    source_tables: [...(m.source_tables || [])],
    target_table: m.target_table || m.source_tables?.[0] || '',
    key_columns: [...(m.key_columns || [])],
    shard_keys: [...(m.shard_keys || [])],
    partition_column: m.partition_column || '',
    is_shard_group: !!m.is_shard_group,
    shard_pattern: m.shard_pattern || '',
    source_db_scope: m.source_db_scope || 'all',
    source_db_names: [...(m.source_db_names || [])],
    sync_table_mode: normalizeSyncTableMode(m.sync_table_mode || m.sync_mode),
    open: m.open !== false,
  }))
  if (!mappings.value.length && form.tables.length) {
    mappings.value = form.tables.map((t) => newMapping({
      source_tables: [t],
      target_table: form.table_map?.[t] || t,
      is_shard_group: false,
    }))
  }
  syncMappingTables()
}

function onDebeziumModeChange(v) {
  const next = typeof v === 'string' ? v : form.debezium_mode
  const needWarn = next === 'before_after' && form.debezium_mode !== 'before_after'
  form.debezium_mode = next
  if (!needWarn) return
  ElMessageBox.alert(
    'Before+After 模式会增加 Kafka 消息体积和 StarRocks 存储，请仅在审计/对比等明确需求下使用。',
    '请确认 Before + After 模式',
    { confirmButtonText: '我知道了', type: 'info', appendTo: document.body },
  )
}

function mergeManualTables() {
  const extra = String(manualTables.value || '').split(/[\s,，]+/).map((s) => s.trim()).filter(Boolean)
  if (!extra.length) return
  const set = new Set([...tables.value, ...form.tables, ...extra])
  tables.value = [...set]
  for (const name of extra) {
    if (!isTableSelected(name)) toggleSingle(name, true)
  }
}

function onParentRowClick(row) {
  if (!row) return
  if (form.task_type === 'add_table' || form.task_type === 'alter_schema') toggleParent(row, !form.parent_ids.includes(row.id))
  else onPickParent(row)
}

function toggleParent(row, checked) {
  if (!row) return
  const next = new Set(form.parent_ids)
  if (checked) next.add(row.id)
  else next.delete(row.id)
  const selected = parentTasks.value.filter((t) => next.has(t.id))
  const err = validateAddTableParents(selected, (id) => instances.value.find((i) => i.id === id))
  if (checked && err) {
    ElMessage.warning(err)
    return
  }
  form.parent_ids = [...next]
  form.parent_id = form.parent_ids[0] || ''
  const ref = selected[0] || (checked ? row : null)
  if (ref) onPickParent(ref)
}

function onPickParent(row) {
  if (!row) return
  form.parent_id = row.id
  if ((form.task_type === 'add_table' || form.task_type === 'alter_schema') && !form.parent_ids.includes(row.id)) form.parent_ids = [row.id]
  const suffix = form.task_type === 'alter_schema' ? '加列' : (form.task_type === 'remove_table' ? '移除表' : '加表')
  form.name = form.name || `${row.name}_${suffix}`
  form.source_instance_id = row.source_instance_id
  form.target_instance_id = row.target_instance_id
  form.source_db = row.source_db
  form.target_db = row.target_db
  const extra = decodeCdcExtra(row.description).extra
  form.env = extra.env || form.env
  form.source_schema = extra.source_schema || 'public'
  form.connector_id = extra.connector_id || ''
  form.debezium_mode = extra.debezium_mode || 'after_only'
  form.message_schema_mode = extra.message_schema_mode || 'without_schema'
  form.key_model = extra.key_model || 'unique_key'
  form.auto_create_table = form.task_type === 'add_table' ? false : extra.auto_create_table !== false
  form.sr_dynamic_partition_enable = extra.sr_dynamic_partition_enable === true
  form.sr_partition_column = extra.sr_partition_column || ''
  form.kafka_bootstrap_servers = extra.kafka_bootstrap_servers || ''
  form.routine_load_parallelism = positiveInt(extra.routine_load_parallelism, infraDefaults.routine_load_parallelism)
  form.kafka_topic_partitions = positiveInt(extra.kafka_topic_partitions, infraDefaults.kafka_topic_partitions)
  form.kafka_topic_replication_factor = positiveInt(extra.kafka_topic_replication_factor, infraDefaults.kafka_topic_replication_factor)
  form.sr_auto_buckets = positiveInt(extra.sr_auto_buckets, infraDefaults.sr_auto_buckets)
  form.sr_replication_num = positiveInt(extra.sr_replication_num, infraDefaults.sr_replication_num)
  form.publication_name = extra.publication_name || ''
  form.slot_name = extra.slot_name || ''
  form.connector_name = extra.connector_name || ''
  form.topic_prefix = extra.topic_prefix || ''
  if (!canAutoAlter.value && form.task_type === 'alter_schema') form.alter_schema_exec_mode = 'sql_only'
  void loadSourceDbs()
  void loadParentExtras()
}

async function loadParentExtras() {
  const ids = form.parent_ids.length ? form.parent_ids : (form.parent_id ? [form.parent_id] : [])
  parentMappingRows.value = []
  syncedByParent.value = {}
  if (!ids.length) return
  try {
    const [maps, synced] = await Promise.all([
      api.get(`/cdc-sync/tasks/${ids[0]}/tables`).catch(() => ({ data: {} })),
      api.post('/cdc-sync/tasks/synced-tables/batch', { parent_ids: ids }).catch(() => ({ data: {} })),
    ])
    parentMappingRows.value = maps.data.data || []
    syncedByParent.value = synced.data.data || {}
  } catch {
    parentMappingRows.value = []
    syncedByParent.value = {}
  }
}

function toggleAlterMapping(m) {
  const name = m.target_table
  const set = new Set(form.tables)
  if (set.has(name)) set.delete(name)
  else set.add(name)
  form.tables = [...set]
  void loadSchemaDiff()
}

async function loadSchemaDiff() {
  if (!form.parent_id || !form.tables.length) {
    alterDiffItems.value = []
    diffWarning.value = ''
    return
  }
  diffLoading.value = true
  try {
    const items = []
    const warns = []
    for (const m of parentMappings.value.filter((x) => form.tables.includes(x.target_table))) {
      const { data } = await api.post(`/cdc-sync/tasks/${form.parent_id}/schema-diff`, {
        target_table: m.target_table,
        source_tables: m.source_tables || [m.target_table],
      })
      const row = (data.data?.items || [])[0] || { target_table: m.target_table, columns_to_add: [], warnings: [] }
      items.push(row)
      for (const w of row.warnings || []) warns.push(w)
      if (row.error) warns.push(row.error)
    }
    alterDiffItems.value = items
    diffWarning.value = [...new Set(warns)].join('；')
  } catch (e) {
    diffWarning.value = e?.response?.data?.message || '列对比失败'
  } finally {
    diffLoading.value = false
  }
}

async function refreshSql() {
  sqlPreviews.value = buildSqlPreview(form, mappings.value, form.kafka_bootstrap_servers || connectorKafkaHost.value)
  try {
    const { data } = await api.post('/cdc-sync/tasks/preview-target-sql', {
      ...submitBody(),
      ...extraPayload(),
    })
    const items = data.data?.items || []
    if (items.length) {
      sqlPreviews.value = items.map((it) => ({
        table: it.table,
        source: it.source,
        ddl: it.ddl || '',
        load: it.load || '',
        skipped: it.skipped,
      }))
    } else {
      const sql = data.data?.sql || ''
      if (sql) sqlPreviews.value = [{ table: form.target_db || 'target', source: form.tables.join(','), ddl: sql, load: '' }]
    }
  } catch { /* keep local preview */ }
  return loadDebeziumPreview()
}

async function loadDebeziumPreview() {
  try {
    const { data } = await api.post('/cdc-sync/tasks/debezium-preview', {
      ...submitBody(),
      ...extraPayload(),
    })
    debeziumPreview.value = data.data || null
  } catch (e) {
    debeziumPreview.value = null
    ElMessage.warning(e?.response?.data?.message || 'Debezium 配置预览失败')
  }
}

const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess } = useWorkOrderDraft({
  type: 'cdc',
  label: 'CDC同步任务',
  path: '/ops/cdc/create',
  form,
  enabled: () => form.task_type === 'init',
  extraWatch: [mappings, step],
  getValues: () => ({ form: { ...form, mappings: mappings.value }, step: step.value }),
  applyValues: (v) => {
    const d = v.form || v
    Object.assign(form, emptyForm(), d)
    restoreMappings(d.mappings || form.mappings)
    if (Number.isFinite(v.step)) step.value = v.step
    else if (Number.isFinite(d.step)) step.value = d.step
  },
})

function instanceID(v) {
  if (v && typeof v === 'object') return String(v.id || v.instance_id || v.value || '').trim()
  return String(v || '').trim()
}

function extraPayload() {
  const multi = isMultiDbScope(form.source_scope_mode, resolvedShardDbs.value)
  const selectedCols = alterDiffRows.value.filter((c) => c.selected)
  const alterTables = form.tables.map((tgt) => ({
    target_table: tgt,
    source_tables: parentMappings.value.find((m) => m.target_table === tgt)?.source_tables || [tgt],
    columns: selectedCols.filter((c) => c.target_table === tgt).map((c) => ({
      name: c.name, pg_type: c.pg_type, sr_type: c.sr_type, selected: true,
    })),
  }))
  return {
    env: form.env,
    source_instance_id: instanceID(form.source_instance_id),
    target_instance_id: instanceID(form.target_instance_id),
    source_schema: form.source_schema,
    source_scope_mode: form.source_scope_mode,
    source_db_prefix: form.source_db_prefix,
    source_db_names: multi ? resolvedShardDbs.value : (form.source_db ? [form.source_db] : []),
    shard_db: multi,
    representative_db: form.source_db,
    tables: form.tables,
    table_map: form.table_map,
    mappings: mappings.value.map((m) => ({
      source_tables: m.source_tables,
      target_table: m.target_table,
      key_columns: m.key_columns,
      shard_keys: m.shard_keys || [],
      partition_column: m.partition_column || '',
      sr_dynamic_partition: m.sr_dynamic_partition,
      is_shard_group: m.is_shard_group,
      shard_pattern: m.shard_pattern,
      source_db_scope: m.source_db_scope || 'all',
      source_db_names: (m.source_db_scope === 'explicit' ? m.source_db_names : []) || [],
      sync_table_mode: normalizeSyncTableMode(m.sync_table_mode),
    })),
    connector_id: form.connector_id,
    message_schema_mode: form.message_schema_mode,
    debezium_mode: form.debezium_mode,
    snapshot_mode: form.snapshot_mode,
    filter_delete_events: form.filter_delete_events,
    kafka_bootstrap_servers: normalizeKafkaBootstrap(form.kafka_bootstrap_servers),
    routine_load_parallelism: positiveInt(form.routine_load_parallelism, infraDefaults.routine_load_parallelism),
    kafka_topic_partitions: positiveInt(form.kafka_topic_partitions, infraDefaults.kafka_topic_partitions),
    kafka_topic_replication_factor: positiveInt(form.kafka_topic_replication_factor, infraDefaults.kafka_topic_replication_factor),
    sr_auto_buckets: positiveInt(form.sr_auto_buckets, infraDefaults.sr_auto_buckets),
    sr_replication_num: positiveInt(form.sr_replication_num, infraDefaults.sr_replication_num),
    sr_dynamic_partition_enable: !!form.sr_dynamic_partition_enable,
    sr_partition_column: form.sr_partition_column,
    sr_dynamic_partition: buildSrDynamicPartitionObject(form),
    auto_create_table: form.auto_create_table,
    key_model: form.key_model,
    publication_name: form.publication_name || '',
    slot_name: form.slot_name || '',
    connector_name: form.connector_name || '',
    topic_prefix: form.topic_prefix || debeziumPreview.value?.topic_prefix || '',
    debezium_template: debeziumPreview.value?.template || '',
    debezium_config: debeziumPreview.value?.config || undefined,
    alter_columns: selectedCols.map((c) => c.name).join(',') || form.alter_columns,
    alter_schema_tables: form.task_type === 'alter_schema' ? alterTables : undefined,
    alter_schema_exec_mode: form.alter_schema_exec_mode,
    kafka_default_offsets: form.kafka_default_offsets,
    sql_overrides: Object.keys(collectSqlOverrides(sqlPreviews.value)).length ? collectSqlOverrides(sqlPreviews.value) : undefined,
    parent_ids: form.parent_ids,
  }
}

function submitBody() {
  const extra = extraPayload()
  return {
    name: form.name,
    task_name: form.name,
    task_type: form.task_type,
    parent_id: form.parent_id,
    parent_task_id: form.parent_id,
    parent_ids: form.parent_ids,
    source_instance_id: instanceID(form.source_instance_id),
    target_instance_id: instanceID(form.target_instance_id),
    source_db: form.source_db,
    source_db_name: form.source_db,
    target_db: form.target_db,
    target_db_name: form.target_db,
    description: form.description,
    reviewer: form.reviewer,
    approver: form.reviewer,
    assigned_to: form.reviewer,
    ...extra,
  }
}

async function runPrecheck() {
  if (!instanceID(form.source_instance_id)) {
    ElMessage.warning('请先选择源端实例')
    return
  }
  if (form.auto_create_table && !instanceID(form.target_instance_id)) {
    ElMessage.warning('自动建表开启时必须选择目标端实例')
    return
  }
  prechecking.value = true
  try {
    const { data } = await api.post('/cdc-sync/tasks/precheck', { ...submitBody(), ...extraPayload() })
    const payload = data.data || {}
    if (payload.sql_previews?.length) {
      sqlPreviews.value = payload.sql_previews.map((it) => ({
        table: it.table, source: it.source, ddl: it.ddl || '', load: it.load || '', skipped: it.skipped,
      }))
    }
    precheckChecks.value = payload.checks || []
    precheckOk.value = !!(data.ready ?? data.ok)
    precheckMsg.value = data.message || (precheckOk.value ? '预检通过' : '预检未通过')
    const naming = payload.naming || {}
    if (naming.publication_name && !form.publication_name) form.publication_name = naming.publication_name
    if (naming.slot_name && !form.slot_name) form.slot_name = naming.slot_name
    if (naming.connector_name && !form.connector_name) form.connector_name = naming.connector_name
    if (naming.topic_prefix && !form.topic_prefix) form.topic_prefix = naming.topic_prefix
    ElMessage[precheckOk.value ? 'success' : 'warning'](precheckMsg.value)
  } catch (e) {
    precheckOk.value = false
    precheckChecks.value = []
    precheckMsg.value = e?.response?.data?.message || '预检失败'
    ElMessage.warning(precheckMsg.value)
  } finally {
    prechecking.value = false
  }
}

async function next() {
  if (form.task_type === 'init' && step.value === 0) {
    try {
      await formRef.value?.validateField(['name', 'env', 'source_instance_id', 'source_db', 'source_schema'])
    } catch {
      return
    }
    if (form.source_scope_mode === 'multi_db' && resolvedShardDbs.value.length < 2) {
      ElMessage.warning('多分库至少匹配 2 个库，请检查分库前缀')
      return
    }
    await loadTables()
  }
  if (form.task_type === 'init' && step.value === 1) {
    syncMappingTables()
    if (!form.tables.length) {
      ElMessage.warning('请至少选择一张表')
      return
    }
    if (form.key_model !== 'duplicate_key' && mappings.value.some((m) => !m.key_columns.length)) {
      ElMessage.warning('存在映射未配置 KEY 列，请展开映射或点击「加载全部字段」后勾选 KEY 列')
      return
    }
  }
  if (form.task_type === 'init' && step.value === 2) {
    try {
      await formRef.value?.validateField(['message_schema_mode', 'connector_id', 'debezium_mode', 'snapshot_mode'])
    } catch {
      ElMessage.warning('请完善 Debezium 配置')
      return
    }
  }
  if (form.task_type === 'init' && step.value === 3) {
    if (!form.auto_create_table) {
      formRef.value?.clearValidate?.(['target_instance_id', 'target_db'])
    } else {
      if (!instanceID(form.target_instance_id)) {
        ElMessage.warning('请选择目标端 StarRocks 实例')
        return
      }
      if (!form.target_db) {
        ElMessage.warning('自动建表开启时，目标库必填')
        return
      }
    }
    const partErr = validatePartitionColumnsForMappings(form.sr_dynamic_partition_enable, form.auto_create_table, form.sr_partition_column, mappings.value)
    if (partErr) {
      ElMessage.warning(partErr)
      return
    }
  }
  if (form.task_type === 'init' && step.value === 4) {
    if (!precheckOk.value) {
      ElMessage.warning('检测未通过，请先点击「重新检测」')
      return
    }
  }
  if (needParent.value && step.value === 0) {
    if (form.task_type === 'add_table' && !form.parent_ids.length && !form.parent_id) {
      ElMessage.warning('请选择已有初始化任务')
      return
    }
    if (form.task_type !== 'add_table' && !form.parent_id) {
      ElMessage.warning('请选择已有初始化任务')
      return
    }
  }
  if (form.task_type === 'add_table' && step.value === 0) await loadTables()
  if (form.task_type === 'add_table' && step.value === 1) {
    syncMappingTables()
    if (!form.tables.length) {
      ElMessage.warning('请至少选择一张表')
      return
    }
    if (form.key_model !== 'duplicate_key' && mappings.value.some((m) => !m.key_columns.length)) {
      ElMessage.warning('存在映射未配置 KEY 列，请展开映射或点击「加载全部字段」后勾选 KEY 列')
      return
    }
  }
  if (form.task_type === 'alter_schema' && step.value === 1) {
    if (!form.tables.length) {
      ElMessage.warning('请至少选择一张目标表')
      return
    }
    if (!alterDiffRows.value.some((c) => c.selected)) {
      ElMessage.warning('请至少勾选一列待加列')
      return
    }
  }
  if (form.task_type === 'remove_table' && step.value === 1 && !form.tables.length) {
    ElMessage.warning('请至少选择一张要移除的表')
    return
  }
  if (form.task_type === 'alter_schema' && step.value === 2 && !form.pg_column_ack) {
    ElMessage.warning('请确认已在 PG 加列')
    return
  }
  step.value = Math.min(step.value + 1, steps.value.length - 1)
  if (form.task_type === 'init' && step.value === 4) void runPrecheck()
  if (isSqlStep.value) await refreshSql()
  else if (isConfirmStep.value) {
    await loadDebeziumPreview()
    if (form.task_type === 'alter_schema') await loadAlterMigrateSQL()
  }
}

async function loadAlterMigrateSQL() {
  if (!form.parent_id || !form.tables.length) {
    migrateSQL.value = ''
    return
  }
  try {
    const { data } = await api.post(`/cdc-sync/tasks/${form.parent_id}/schema-migrate-sql`, {
      kafka_default_offsets: form.kafka_default_offsets,
      items: form.tables.map((tgt) => ({
        target_table: tgt,
        columns: alterDiffRows.value.filter((c) => c.selected && c.target_table === tgt),
      })),
    })
    migrateSQL.value = data.data?.sql || ''
  } catch {
    migrateSQL.value = ''
  }
}

function prev() {
  if (form.task_type === 'init' && (step.value === 4 || step.value === 5)) {
    precheckOk.value = false
    precheckMsg.value = ''
  }
  step.value = Math.max(step.value - 1, 0)
}

async function submit() {
  if (!form.name) {
    ElMessage.warning('请填写任务名称')
    return
  }
  if (!form.source_instance_id) {
    ElMessage.warning('源端实例不能为空')
    return
  }
  if (form.task_type === 'init' && form.auto_create_table && !form.target_instance_id) {
    ElMessage.warning('自动建表开启时必须选择目标端实例')
    return
  }
  if (needParent.value && !form.parent_id && !form.parent_ids.length) {
    ElMessage.warning('请选择父任务')
    return
  }
  if (!form.reviewer) {
    ElMessage.warning('请选择审核人')
    return
  }
  submitting.value = true
  try {
    const body = submitBody()
    if (form.task_type === 'add_table' && form.parent_ids.length > 1) {
      const { data: pre } = await api.post('/cdc-sync/tasks/batch-add-table/precheck', { ...body, parent_ids: form.parent_ids }).catch(() => ({ data: {} }))
      if (pre.data?.failed) ElMessage.warning(`批量预检：${pre.data.failed} 个父任务未通过，仍将提交可通过的任务`)
    }
    if ((form.task_type === 'add_table' || form.task_type === 'alter_schema') && form.parent_ids.length > 1) {
      const path = form.task_type === 'alter_schema' ? '/cdc-sync/tasks/batch-alter-schema' : '/cdc-sync/tasks/batch-add-table'
      const { data } = await api.post(path, { ...body, parent_ids: form.parent_ids })
      clearOnSubmitSuccess()
      const created = data.data?.created || 0
      const skipped = data.data?.skipped || 0
      const label = form.task_type === 'alter_schema' ? '加列' : '加表'
      ElMessage.success(`已提交 ${created} 条${label}任务${skipped ? `，跳过 ${skipped} 个` : ''}`)
      const issueId = data.data?.issue_id || (data.data?.items || []).find((it) => it.task?.issue_id)?.task?.issue_id
      if (issueId) router.push('/ops/tickets/' + issueId)
      else goList({ tab: 'pending' })
      return
    }
    const { data } = await api.post('/cdc-sync/tasks', body)
    clearOnSubmitSuccess()
    const row = data.data || {}
    ElMessage.success('已提交审批，通过后自动执行')
    if (row.issue_id) router.push('/ops/tickets/' + row.issue_id)
    else goList({ tab: 'pending' })
  } finally {
    submitting.value = false
  }
}

watch(() => route.query.type, (t) => {
  resetMode(String(t || 'init'))
  pickDefaultReviewer()
})

watch(mappings, () => { syncMappingTables() }, { deep: true })
watch(shardOpen, (open) => { if (open) scheduleDiscover() })
watch(shardPatternType, () => { if (shardOpen.value) scheduleDiscover() })

watch(() => form.auto_create_table, () => {
  formRef.value?.clearValidate?.(['target_instance_id', 'target_db'])
  if (form.task_type === 'add_table' && step.value > steps.value.length - 1) {
    step.value = steps.value.length - 1
  }
})

watch(() => form.message_schema_mode, () => {
  if (form.connector_id && !filteredConnectors.value.some((c) => c.id === form.connector_id)) {
    form.connector_id = ''
  }
})

onMounted(async () => {
  resetMode(String(route.query.type || 'init'))
  const [instRes, envRes, connRes, taskRes, argRes] = await Promise.all([
    api.get('/cdc-sync/instances').catch(() => api.get('/instances')),
    api.get('/environments').catch(() => ({ data: { data: [] } })),
    api.get('/connectors').catch(() => ({ data: { data: [] } })),
    api.get('/cdc-sync/tasks/init-list').catch(() => api.get('/cdc-sync/tasks', { params: { task_type: 'init' } })),
    api.get('/global-args').catch(() => ({ data: { data: [] } })),
  ])
  instances.value = instRes.data.data || []
  const envs = envRes.data.data || []
  envOptions.value = envs.map((e) => ({ value: e.code || e.name, label: e.name ? `${e.name} (${e.code})` : e.code })).filter((e) => e.value)
  if (!envOptions.value.length) envOptions.value = [{ value: 'test', label: '测试 (test)' }, { value: 'prod', label: '生产 (prod)' }]
  connectors.value = connRes.data.data || []
  parentTasks.value = (taskRes.data.data || []).filter((t) => t.task_type === 'init')
  const preParent = String(route.query.parent_id || '').trim()
  if (preParent && parentTasks.value.some((t) => t.id === preParent)) {
    const row = parentTasks.value.find((t) => t.id === preParent)
    if (row) onPickParent(row)
  }
  const args = argRes.data.data || []
  applyInfraDefaults(args)
  Object.assign(form, {
    routine_load_parallelism: infraDefaults.routine_load_parallelism,
    kafka_topic_partitions: infraDefaults.kafka_topic_partitions,
    kafka_topic_replication_factor: infraDefaults.kafka_topic_replication_factor,
    sr_auto_buckets: infraDefaults.sr_auto_buckets,
    sr_replication_num: infraDefaults.sr_replication_num,
  })
  const set = new Set()
  String(args.find((a) => a.key === 'sync_default_approver')?.value || '').split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean).forEach((u) => set.add(u))
  defaultReviewers.value = [...set]
  pickDefaultReviewer()
  if (form.task_type !== 'init') return
  if (!restoreDraft()) return
  if (form.source_instance_id) await loadSourceDbs()
  if (form.source_instance_id && form.source_db) await loadTables()
  if (step.value >= 5) refreshSql()
})
</script>

<style scoped>
.cdc-create { overflow: auto; }
.cdc-card { margin-bottom: 16px; border-radius: 8px; }
.map-card { min-height: 420px; }
.card-title { font-weight: 500; color: #111827; }
.card-head { display: flex; justify-content: space-between; align-items: center; }
.form-extra { margin-top: 6px; color: #6b7280; font-size: 12px; }
.form-extra.warn { color: #b45309; }
.qmark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  margin-left: 4px;
  border-radius: 50%;
  border: 1px solid #d1d5db;
  color: #9ca3af;
  font-size: 10px;
  line-height: 1;
  cursor: help;
  vertical-align: middle;
}
.label-opt { margin-left: 4px; font-weight: 400; }
.key-opt-row { display: flex; align-items: center; justify-content: space-between; width: 100%; gap: 8px; }
.key-pop { font-size: 13px; line-height: 1.6; color: #374151; }
.key-tags { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 4px; }
.dp-block { border-top: 1px dashed #e5e7eb; padding-top: 12px; margin-top: 8px; }
.mono :deep(.el-input__inner) { font-family: Menlo, Consolas, Monaco, monospace; font-size: 12px; }
.cdc-adv { border: none; }
.cdc-adv :deep(.el-collapse-item__header) { font-size: 14px; color: #374151; height: 40px; }
.cdc-adv :deep(.el-collapse-item__wrap) { border-bottom: none; }
.infra-title { display: block; font-weight: 600; font-size: 12px; margin: 12px 0 4px; color: #111827; }
.opt-main { font-size: 13px; }
.opt-sub { display: block; color: #9ca3af; font-size: 12px; line-height: 1.5; margin-top: 2px; padding-left: 22px; }
.mode-radios, .key-models { display: flex; flex-direction: column; gap: 12px; align-items: flex-start; width: 100%; }
.mode-opt, .key-opt { width: 100%; }
.mode-opt :deep(.el-radio), .key-opt :deep(.el-radio) { align-items: flex-start; height: auto; white-space: normal; }
.warn-text { color: #d97706; }
.ml { margin-left: 8px; }
.src-list {
  max-height: 420px;
  overflow: auto;
  padding: 4px 0;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
}
.src-row { padding: 6px 12px; border-bottom: 1px solid #f3f4f6; }
.src-row:last-child { border-bottom: 0; }
.src-name { margin-right: 8px; }
.src-members { margin: 4px 0 0 24px; }
.expand-btn { padding: 0 4px; }
.map-item { border: 1px solid #e5e7eb; border-radius: 8px; margin-bottom: 10px; background: #fff; }
.map-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 10px 12px; cursor: pointer; }
.map-title { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; min-width: 0; }
.map-arrow { color: #9ca3af; }
.map-body { padding: 0 12px 12px; }
.tree-empty { padding: 28px 12px; text-align: center; color: #9ca3af; font-size: 13px; }
.mb-sm { margin-bottom: 6px; }
.tbl-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
.tbl-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 4px 12px;
  max-height: 320px;
  overflow: auto;
  padding: 8px;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
}
.key-models { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
.col-radios { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
.precheck-box { margin-top: 12px; padding: 10px 12px; border-radius: 6px; font-size: 13px; }
.precheck-box.ok { background: #f0fdf4; color: #166534; }
.precheck-box.bad { background: #fef2f2; color: #991b1b; }
.sql-box :deep(.el-textarea__inner) { font-family: Menlo, Consolas, Monaco, monospace; font-size: 12px; }
.map-summary { max-height: 260px; overflow: auto; display: flex; flex-direction: column; gap: 8px; }
.map-summary-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding-bottom: 8px; border-bottom: 1px dashed #f3f4f6; }
.map-summary-row code { font-size: 12px; }
.mb { margin-bottom: 16px; }
.mt { margin-top: 12px; }
.muted { color: #9ca3af; font-size: 12px; }
.shard-db-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.alter-map { cursor: pointer; }
.alter-map.active { background: #eff6ff; }
</style>
