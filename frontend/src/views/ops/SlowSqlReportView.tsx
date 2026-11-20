import React, { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import api from '../../api/request'

export default function SlowSqlReportView() {
  const { id } = useParams()
  const [html, setHtml] = useState('')
  const [err, setErr] = useState('')
  const [missing, setMissing] = useState('')

  useEffect(() => {
    if (!id) return
    api.get('/sql-insight/slow-reports/mails/' + id).then(({ data }) => {
      const row = data.data || {}
      setHtml(row.html || '')
      setErr(row.error || '')
      if (!row.html) setMissing('这份周报没有可查看的内容')
    }).catch((e) => {
      setMissing(e?.response?.data?.message || '打开周报失败')
    })
  }, [id])

  return (
    <div style={{ height: '100vh', display: 'flex', flexDirection: 'column', background: '#fff' }}>
      {err ? <div style={{ padding: '8px 16px', color: '#cf1322' }}>{err}</div> : null}
      {missing ? <div style={{ padding: 16 }}>{missing}</div> : null}
      {html ? <iframe title="周报" srcDoc={html} style={{ flex: 1, width: '100%', border: 0 }} /> : null}
    </div>
  )
}
