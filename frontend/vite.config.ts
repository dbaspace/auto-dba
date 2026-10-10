import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { execFileSync } from 'node:child_process'
import { fileURLToPath, URL } from 'node:url'
import path from 'node:path'
import fs from 'node:fs'

const repoRoot = path.resolve(fileURLToPath(new URL('.', import.meta.url)), '..')
const proxyBin = path.join(repoRoot, 'bin', 'print_dev_api_proxy')

type DevEndpoints = {
  api_target: string
  vite_host: string
  vite_port: number
}

const fallback: DevEndpoints = {
  api_target: 'http://127.0.0.1:8800',
  vite_host: '127.0.0.1',
  vite_port: 5174,
}

/** 优先预编译 bin/print_dev_api_proxy；否则 go run（较慢）。可用 AUTO_DBA_DEV_ENDPOINTS_JSON 跳过。 */
function resolveDevEndpoints(): DevEndpoints {
  const fromEnv = process.env.AUTO_DBA_DEV_ENDPOINTS_JSON?.trim()
  if (fromEnv) {
    try {
      const parsed = JSON.parse(fromEnv) as Partial<DevEndpoints>
      return {
        api_target: parsed.api_target || fallback.api_target,
        vite_host: parsed.vite_host || fallback.vite_host,
        vite_port: Number(parsed.vite_port) > 0 ? Number(parsed.vite_port) : fallback.vite_port,
      }
    } catch (err) {
      console.warn('[vite] AUTO_DBA_DEV_ENDPOINTS_JSON 无效', err)
    }
  }

  try {
    let out: string
    if (fs.existsSync(proxyBin)) {
      out = execFileSync(proxyBin, [], {
        cwd: repoRoot,
        encoding: 'utf8',
        timeout: 30_000,
        env: process.env,
      })
    } else {
      console.warn('[vite] 未找到 bin/print_dev_api_proxy，回退 go run（可先执行 scripts/build-dev-api-proxy.sh）')
      out = execFileSync('go', ['run', './scripts/print_dev_api_proxy'], {
        cwd: repoRoot,
        encoding: 'utf8',
        timeout: 90_000,
        env: process.env,
      })
    }
    const line = out
      .split(/\r?\n/)
      .map((s) => s.trim())
      .filter(Boolean)
      .reverse()
      .find((s) => s.startsWith('{') && s.includes('api_target'))
    if (line) {
      const parsed = JSON.parse(line) as Partial<DevEndpoints>
      return {
        api_target: parsed.api_target || fallback.api_target,
        vite_host: parsed.vite_host || fallback.vite_host,
        vite_port: Number(parsed.vite_port) > 0 ? Number(parsed.vite_port) : fallback.vite_port,
      }
    }
    console.warn('[vite] print_dev_api_proxy 无有效 JSON，使用默认端口')
  } catch (err) {
    console.warn('[vite] 解析 Nacos/本地前端配置失败，使用默认端口', err)
  }
  return { ...fallback }
}

export default defineConfig(() => {
  const ep = resolveDevEndpoints()
  console.log(
    `[vite] host=${ep.vite_host} port=${ep.vite_port} proxy→${ep.api_target}（Nacos/本地 config.Load）`,
  )
  return {
    plugins: [react()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      host: ep.vite_host,
      port: ep.vite_port,
      proxy: {
        '/api': { target: ep.api_target, changeOrigin: true, xfwd: true, ws: true },
        '/healthz': { target: ep.api_target, changeOrigin: true, xfwd: true },
        '/starrocks-webroot': { target: ep.api_target, changeOrigin: true, xfwd: true },
      },
    },
    preview: {
      host: ep.vite_host,
      port: ep.vite_port,
    },
  }
})
