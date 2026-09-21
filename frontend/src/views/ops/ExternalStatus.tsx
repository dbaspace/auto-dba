import React, { useCallback, useEffect, useState } from 'react'
import { Button, Result, Space } from 'antd'
import api from '../../api/request'
import './ExternalStatus.css'

type Props = {
  kind?: 'dolphin' | 'sr'
}

export default function ExternalStatus({ kind = 'dolphin' }: Props) {
  const [ok, setOk] = useState(false)
  const [message, setMessage] = useState('')
  const [url, setUrl] = useState('')

  const load = useCallback(async () => {
    const path = kind === 'dolphin' ? '/dsadmin/status' : '/feadmin/status'
    const { data } = await api.get(path)
    setOk(!!data.ok)
    setMessage(data.message || '')
    setUrl(data.url || '')
  }, [kind])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="ops-page">
      <div className="ops-title">
        <div className="toolbar" style={{ margin: 0 }}>
          <Space>
            <Button onClick={load}>重新检查</Button>
            {url ? (
              <Button type="primary" onClick={() => window.open(url, '_blank')}>
                打开控制台
              </Button>
            ) : null}
          </Space>
        </div>
      </div>
      {url ? (
        <div className="ops-table">
          <iframe src={url} className="embed" title="external-console" />
        </div>
      ) : (
        <Result status={ok ? 'success' : 'info'} title={ok ? '已配置' : '未配置'} subTitle={message} />
      )}
    </div>
  )
}
