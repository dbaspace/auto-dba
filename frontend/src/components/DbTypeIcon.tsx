import React from 'react'
import postgres from '../assets/db/postgres.png'
import mysql from '../assets/db/mysql.png'
import tidb from '../assets/db/tidb.png'
import starrocks from '../assets/db/starrocks.png'
import clickhouse from '../assets/db/clickhouse.png'
import kafka from '../assets/db/kafka.png'
import mongodb from '../assets/db/mongodb.png'
import redis from '../assets/db/redis.png'
import elasticsearch from '../assets/db/elasticsearch.svg'
import cockroachdb from '../assets/db/cockroachdb.svg'
import oceanbase from '../assets/db/oceanbase.svg'
import cassandra from '../assets/db/cassandra.svg'
import oracle from '../assets/db/oracle.svg'
import sqlserver from '../assets/db/sqlserver.svg'

const ICONS: Record<string, string> = {
  postgres,
  postgresql: postgres,
  pg: postgres,
  pgsql: postgres,
  mysql,
  tidb,
  starrocks,
  clickhouse,
  kafka,
  mongodb,
  mongo: mongodb,
  redis,
  elasticsearch,
  es: elasticsearch,
  elastic: elasticsearch,
  cockroachdb,
  cockroach: cockroachdb,
  oceanbase,
  cassandra,
  oracle,
  sqlserver,
  mssql: sqlserver,
}

interface Props {
  type?: string
  size?: number
}

export default function DbTypeIcon({ type = '', size = 16 }: Props) {
  const name = String(type || '').toLowerCase().trim()
  const src = ICONS[name] || ''
  if (src) {
    return (
      <img
        src={src}
        alt={name}
        title={name}
        width={size}
        height={size}
        style={{ display: 'inline-block', flexShrink: 0, objectFit: 'contain', verticalAlign: 'middle' }}
      />
    )
  }
  const ch = (name[0] || '?').toUpperCase()
  return (
    <span
      title={name}
      style={{ display: 'inline-block', flexShrink: 0, width: size, height: size, lineHeight: 0, verticalAlign: 'middle' }}
    >
      <svg viewBox="0 0 24 24" width={size} height={size}>
        <rect width="24" height="24" rx="5" fill="#6B7280" />
        <text x="12" y="16" textAnchor="middle" fill="#fff" fontSize="12" fontFamily="sans-serif">
          {ch}
        </text>
      </svg>
    </span>
  )
}
