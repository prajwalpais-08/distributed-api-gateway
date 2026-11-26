import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Activity, AlertTriangle, CheckCircle2, Database, Gauge,
  RefreshCw, Server, ShieldCheck, Zap,
} from 'lucide-react'
import {
  Area, AreaChart, CartesianGrid, ResponsiveContainer,
  Tooltip, XAxis, YAxis,
} from 'recharts'

type Health = { ok: boolean; name: string; detail: string }
type MetricsPayload = {
  timestamp: string
  gateway: Health
  backends: Health[]
  metrics: {
    totalRequests: number
    serverErrors: number
    rateLimited: number
    cacheHits: number
    cacheMisses: number
    cacheHitRate: number | null
    p95LatencyMs: number | null
  }
}
type HistoryPoint = { time: string; rps: number }

function MetricCard({ title, value, unit, note, icon: Icon, tone }: {
  title: string; value: string; unit?: string; note: string
  icon: typeof Activity; tone: string
}) {
  return <article className="metric-card">
    <div className="metric-heading"><span>{title}</span><span className={`metric-icon ${tone}`}><Icon size={18} /></span></div>
    <div className="metric-value">{value}<small>{unit}</small></div>
    <p>{note}</p>
  </article>
}

function HealthRow({ item }: { item: Health }) {
  return <div className="health-row">
    <span className={`health-indicator ${item.ok ? 'healthy' : 'unhealthy'}`} />
    <div className="health-copy"><strong>{item.name}</strong><span>{item.detail}</span></div>
    <span className={`health-pill ${item.ok ? 'healthy' : 'unhealthy'}`}>
      {item.ok ? <CheckCircle2 size={14} /> : <AlertTriangle size={14} />}
      {item.ok ? 'Healthy' : 'Check'}
    </span>
  </div>
}

const fmt = (value: number | null | undefined, digits = 0) =>
  value === null || value === undefined || !Number.isFinite(value) ? '—' : value.toLocaleString(undefined, { maximumFractionDigits: digits })

export default function App() {
  const [data, setData] = useState<MetricsPayload | null>(null)
  const [history, setHistory] = useState<HistoryPoint[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const previous = useRef<{ total: number; time: number } | null>(null)

  const refresh = useCallback(async () => {
    try {
      const response = await fetch('/api/metrics', { cache: 'no-store' })
      const body = await response.json() as MetricsPayload | { error?: string }
      if (!response.ok) throw new Error('error' in body && body.error ? body.error : `Metrics request failed (${response.status})`)
      const next = body as MetricsPayload
      const now = Date.now()
      if (previous.current) {
        const seconds = Math.max((now - previous.current.time) / 1000, 1)
        const difference = next.metrics.totalRequests - previous.current.total
        const rps = difference >= 0 ? difference / seconds : 0
        setHistory(old => [...old, { time: new Date(now).toLocaleTimeString(), rps }].slice(-18))
      }
      previous.current = { total: next.metrics.totalRequests, time: now }
      setData(next)
      setLastUpdated(new Date(now))
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to load production metrics')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 15000)
    return () => window.clearInterval(timer)
  }, [refresh])

  const backends = data?.backends ?? []
  const services = data ? [data.gateway, ...backends] : []
  const healthyCount = services.filter(service => service.ok).length
  const allHealthy = services.length === 4 && healthyCount === 4
  const hitRate = data?.metrics.cacheHitRate ?? null

  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><div className="brand-mark"><Activity size={22} /></div><div><strong>GateWatch</strong><span>DISTRIBUTED API GATEWAY</span></div></div>
      <div className="nav-label">MONITORING</div>
      <a className="nav-item active" href="#overview"><Activity size={17} /> Overview</a>
      <a className="nav-item" href="#traffic"><Gauge size={17} /> Traffic analytics</a>
      <a className="nav-item" href="#services"><Server size={17} /> Services</a>
      <a className="nav-item" href="#cache"><Database size={17} /> Redis cache</a>
      <a className="nav-item" href="#protection"><ShieldCheck size={17} /> Protection</a>
      <div className="sidebar-bottom">
        <span className={`live-dot ${error ? 'off' : ''}`} />
        <span>{error ? 'Metrics connection issue' : loading ? 'Connecting to production' : 'Production monitoring'}</span>
        <small>GateWatch · v1.0</small>
      </div>
    </aside>

    <main className="main-content" id="overview">
      <header className="topbar">
        <div><div className="eyebrow">MONITORING / PRODUCTION</div><h1>Gateway dashboard</h1><p>Live metrics collected from the deployed GateWatch API Gateway.</p></div>
        <button className="refresh-button" onClick={() => void refresh()} disabled={loading}><RefreshCw size={16} /> {loading ? 'Loading…' : 'Refresh'}</button>
      </header>

      {error && <div className="notice error"><AlertTriangle size={18} /><span>Could not load production metrics: {error}</span></div>}
      {!error && data && <div className={`notice ${allHealthy ? 'success' : 'warning'}`}><Activity size={18} /><span>{allHealthy ? 'Gateway and all three backend health checks are passing.' : `${healthyCount} of 4 services report healthy. Review the service panel below.`}</span></div>}

      <section className="metric-grid">
        <MetricCard title="Total HTTP requests" value={fmt(data?.metrics.totalRequests)} note="Cumulative gateway counter" icon={Activity} tone="blue" />
        <MetricCard title="P95 latency" value={fmt(data?.metrics.p95LatencyMs, 1)} unit=" ms" note="From gateway histogram buckets" icon={Gauge} tone="purple" />
        <MetricCard title="5xx responses" value={fmt(data?.metrics.serverErrors)} note="Cumulative server errors" icon={AlertTriangle} tone="orange" />
        <MetricCard title="Cache hit rate" value={fmt(hitRate, 1)} unit="%" note="Redis cache hits / total cache lookups" icon={Database} tone="green" />
      </section>

      <section className="panel traffic-panel" id="traffic">
        <div className="panel-title"><div><h2>Request throughput</h2><p>Requests per second, estimated from successive gateway counter samples</p></div><span className="live-tag"><i /> LIVE</span></div>
        <div className="chart-wrap">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={history} margin={{ top: 10, right: 12, left: 0, bottom: 0 }}>
              <defs><linearGradient id="trafficFill" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#5b9cff" stopOpacity={0.35} /><stop offset="100%" stopColor="#5b9cff" stopOpacity={0.02} /></linearGradient></defs>
              <CartesianGrid stroke="#243148" strokeDasharray="3 3" />
              <XAxis dataKey="time" stroke="#8290a8" tickLine={false} axisLine={false} minTickGap={24} />
              <YAxis stroke="#8290a8" tickLine={false} axisLine={false} width={42} />
              <Tooltip contentStyle={{ background: '#111b2c', border: '1px solid #293950', borderRadius: 10, color: '#edf3ff' }} />
              <Area type="monotone" dataKey="rps" name="Requests/sec" stroke="#5b9cff" fill="url(#trafficFill)" strokeWidth={2} connectNulls />
            </AreaChart>
          </ResponsiveContainer>
          {history.length === 0 && <div className="chart-empty">Throughput samples will appear after the next refresh.</div>}
        </div>
      </section>

      <section className="content-grid">
        <article className="panel" id="services">
          <div className="panel-title"><div><h2>Service health</h2><p>Live HTTP health checks</p></div><span className="small-count">{healthyCount}/4 healthy</span></div>
          {services.length ? services.map(item => <HealthRow key={item.name} item={item} />) : <p className="muted">Waiting for health checks…</p>}
        </article>

        <article className="panel" id="cache">
          <div className="panel-title"><div><h2>Redis cache</h2><p>Gateway cache counters</p></div><Database size={19} className="title-icon" /></div>
          <div className="cache-values"><div><strong>{fmt(data?.metrics.cacheHits)}</strong><span>Cache hits</span></div><div><strong>{fmt(data?.metrics.cacheMisses)}</strong><span>Cache misses</span></div></div>
          <div className="cache-track"><span style={{ width: `${hitRate ?? 0}%` }} /></div>
          <div className="cache-foot"><span>Hit rate</span><strong>{fmt(hitRate, 1)}%</strong></div>
          <div className="cache-foot"><span>429 rate-limited requests</span><strong>{fmt(data?.metrics.rateLimited)}</strong></div>
        </article>
      </section>

      <section className="panel protection-panel" id="protection">
        <div className="panel-title"><div><h2>Gateway protection</h2><p>Protection features included in the deployed gateway</p></div><ShieldCheck size={20} className="title-icon" /></div>
        <div className="protection-grid">
          <div className="protection-item"><span className="protection-icon blue"><Zap size={19} /></span><div><strong>Distributed rate limiter</strong><span>Redis-backed token bucket · {fmt(data?.metrics.rateLimited)} requests limited</span></div><span className="configured"><CheckCircle2 size={14} /> Active</span></div>
          <div className="protection-item"><span className="protection-icon purple"><ShieldCheck size={19} /></span><div><strong>Circuit breakers</strong><span>Individual protection for products backends</span></div><span className="configured"><CheckCircle2 size={14} /> Initialized</span></div>
        </div>
      </section>

      <footer className="footer"><span>GateWatch · Production monitoring</span><span>Last updated: {lastUpdated ? lastUpdated.toLocaleTimeString() : 'Not yet refreshed'} · Auto-refresh every 15 seconds</span></footer>
    </main>
  </div>
}
