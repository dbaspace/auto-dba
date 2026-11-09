import React, { useEffect, useState } from 'react'
import { Tabs } from 'antd'
import { useNavigate } from 'react-router-dom'
import PlatformAgent from './PlatformAgent'
import SqlExecutors from './SqlExecutors'
import NodeAgentPanel from './NodeAgentPanel'
import DbAgentPanel from './DbAgentPanel'

const TABS = ['platform', 'sql', 'node', 'db'] as const
type TabKey = (typeof TABS)[number]

function parseTab(raw: string | null): TabKey {
  if (raw && (TABS as readonly string[]).includes(raw)) return raw as TabKey
  return 'platform'
}

/** 页内页签写在 hash 查询上。导航 Tab 的 location 不跟这条查询走，所以从地址栏读。 */
function tabFromLocation(): TabKey {
  const hash = window.location.hash || ''
  const q = hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : window.location.search.replace(/^\?/, '')
  return parseTab(new URLSearchParams(q).get('tab'))
}

/** 配置中心 · Agent管理：四类已有能力按页签汇总，不改各自接口。 */
export default function AgentManage() {
  const navigate = useNavigate()
  const [tab, setTab] = useState<TabKey>(tabFromLocation)

  useEffect(() => {
    const sync = () => setTab(tabFromLocation())
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])

  const select = (key: string) => {
    const next = parseTab(key)
    setTab(next)
    navigate(`/config/agents?tab=${next}`, { replace: true })
  }

  return (
    <div className="backup-page">
      <div className="backup-toolbar">
        <span className="backup-hint">按类型登记和下发。页签决定制品和接口，业务模块只选择已登记的 Agent。</span>
      </div>
      <Tabs
        activeKey={tab}
        onChange={select}
        items={[
          { key: 'platform', label: '平台 Agent', children: <PlatformAgent part="platform" /> },
          {
            key: 'sql',
            label: 'SQL 执行器',
            children: (
              <>
                <PlatformAgent part="sql" />
                <SqlExecutors />
              </>
            ),
          },
          { key: 'node', label: 'Node Agent', children: <NodeAgentPanel /> },
          { key: 'db', label: 'DB Agent', children: <DbAgentPanel /> },
        ]}
      />
    </div>
  )
}
