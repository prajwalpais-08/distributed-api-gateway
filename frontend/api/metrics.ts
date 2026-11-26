type ApiResponse = {
  status: (code: number) => ApiResponse
  setHeader: (name: string, value: string) => void
  json: (body: unknown) => void
}
type ApiRequest = { method?: string }
type HealthResult = { ok: boolean; name: string; detail: string }

const GATEWAY_URL = (process.env.GATEWAY_BASE_URL || 'https://gatewatch-gateway.onrender.com').replace(/\/$/, '')
const USERS_URL = (process.env.USERS_SERVICE_URL || 'https://gatewatch-users.onrender.com').replace(/\/$/, '')
const PRODUCTS_1_URL = (process.env.PRODUCTS_SERVICE_URL_1 || 'https://gatewatch-products-1.onrender.com').replace(/\/$/, '')
const PRODUCTS_2_URL = (process.env.PRODUCTS_SERVICE_URL_2 || 'https://gatewatch-products-2.onrender.com').replace(/\/$/, '')

async function readHealth(url: string, name: string): Promise<HealthResult> {
  try {
    const response = await fetch(`${url}/health`, { signal: AbortSignal.timeout(6000), cache: 'no-store' })
    if (!response.ok) return { ok: false, name, detail: `HTTP ${response.status}` }
    const body = await response.json().catch(() => ({})) as { status?: string }
    const healthy = body.status === 'healthy'
    return { ok: healthy, name, detail: healthy ? 'Healthy' : 'Unexpected health response' }
  } catch (error) {
    return { ok: false, name, detail: error instanceof Error ? error.message : 'Health check failed' }
  }
}

function sumMetric(text: string, metricName: string, requiredLabel?: string): number {
  let total = 0
  for (const line of text.split(/\r?\n/)) {
    if (!(line === metricName || line.startsWith(`${metricName}{`))) continue
    if (requiredLabel && !line.includes(requiredLabel)) continue
    const value = line.match(/\s([0-9.eE+-]+)\s*$/)
    if (!value) continue
    const numeric = Number(value[1])
    if (Number.isFinite(numeric)) total += numeric
  }
  return total
}

function sumStatus(text: string, statusPattern: RegExp): number {
  let total = 0
  for (const line of text.split(/\r?\n/)) {
    if (!line.startsWith('gateway_http_requests_total{')) continue
    const status = line.match(/status="([0-9]{3})"/)
    const value = line.match(/\s([0-9.eE+-]+)\s*$/)
    if (status && value && statusPattern.test(status[1])) {
      const numeric = Number(value[1])
      if (Number.isFinite(numeric)) total += numeric
    }
  }
  return total
}

function percentile95Ms(text: string): number | null {
  const byBound = new Map<number, number>()
  const count = sumMetric(text, 'gateway_http_request_duration_seconds_count')
  for (const line of text.split(/\r?\n/)) {
    if (!line.startsWith('gateway_http_request_duration_seconds_bucket{')) continue
    const le = line.match(/le="([^"]+)"/)
    const value = line.match(/\s([0-9.eE+-]+)\s*$/)
    if (!le || !value || le[1] === '+Inf') continue
    const bound = Number(le[1])
    const numeric = Number(value[1])
    if (Number.isFinite(bound) && Number.isFinite(numeric)) {
      byBound.set(bound, (byBound.get(bound) ?? 0) + numeric)
    }
  }
  if (count <= 0 || byBound.size === 0) return null
  const target = count * 0.95
  const sorted = [...byBound.entries()].sort((a, b) => a[0] - b[0])
  const bucket = sorted.find(([, cumulative]) => cumulative >= target)
  return bucket ? Math.round(bucket[0] * 1000 * 10) / 10 : null
}

export default async function handler(req: ApiRequest, res: ApiResponse) {
  res.setHeader('Cache-Control', 'no-store, max-age=0')
  res.setHeader('Content-Type', 'application/json; charset=utf-8')
  if (req.method !== 'GET') return res.status(405).json({ error: 'Method not allowed' })

  try {
    const [metricsResponse, users, products1, products2] = await Promise.all([
      fetch(`${GATEWAY_URL}/metrics`, { signal: AbortSignal.timeout(7500), cache: 'no-store' }),
      readHealth(USERS_URL, 'Users backend'),
      readHealth(PRODUCTS_1_URL, 'Products backend 1'),
      readHealth(PRODUCTS_2_URL, 'Products backend 2'),
    ])
    if (!metricsResponse.ok) {
      return res.status(502).json({ error: `Gateway metrics endpoint returned HTTP ${metricsResponse.status}` })
    }

    const text = await metricsResponse.text()
    const totalRequests = sumMetric(text, 'gateway_http_requests_total')
    const cacheHits = sumMetric(text, 'gateway_cache_requests_total', 'result="hit"')
    const cacheMisses = sumMetric(text, 'gateway_cache_requests_total', 'result="miss"')
    const serverErrors = sumStatus(text, /^5/)
    const rateLimited = sumStatus(text, /^429$/)
    const cacheTotal = cacheHits + cacheMisses

    return res.status(200).json({
      timestamp: new Date().toISOString(),
      gateway: { ok: true, name: 'API Gateway', detail: 'Metrics endpoint reachable' },
      backends: [users, products1, products2],
      metrics: {
        totalRequests,
        serverErrors,
        rateLimited,
        cacheHits,
        cacheMisses,
        cacheHitRate: cacheTotal > 0 ? (cacheHits / cacheTotal) * 100 : null,
        p95LatencyMs: percentile95Ms(text),
      },
    })
  } catch (error) {
    return res.status(502).json({ error: error instanceof Error ? error.message : 'Unable to collect gateway metrics' })
  }
}
