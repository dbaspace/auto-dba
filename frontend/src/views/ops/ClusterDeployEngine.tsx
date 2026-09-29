import { Navigate, useParams } from 'react-router-dom'
import SrCluster from './SrCluster'
import DbDeployWorkbench from './DbDeployWorkbench'
import ProxyWorkbench from './ProxyWorkbench'

/** 动态引擎工作台：starrocks → SR；haproxy/nginx/lvs → 代理；其余 → DbDeployWorkbench。 */
export default function ClusterDeployEngine() {
  const { engine } = useParams<{ engine: string }>()
  const id = (engine || '').toLowerCase().trim()
  if (!id || id === 'host') {
    return <Navigate to="/ops/cluster-deploy" replace />
  }
  if (id === 'starrocks') {
    return <SrCluster />
  }
  if (id === 'haproxy' || id === 'nginx' || id === 'lvs') {
    return <ProxyWorkbench kind={id} />
  }
  return <DbDeployWorkbench engine={id} />
}
