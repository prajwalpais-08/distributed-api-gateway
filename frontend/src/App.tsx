import { useCallback, useEffect, useState } from 'react'
import {
  Activity, Server, Gauge, Database, ShieldCheck,
  AlertTriangle, CheckCircle2, Zap, RefreshCw
} from 'lucide-react'
import {
  ResponsiveContainer, AreaChart, Area, XAxis, YAxis,
  CartesianGrid, Tooltip, BarChart, Bar
} from 'recharts'
import { queryPrometheus, type PrometheusSample } from './prometheus'
import './App.css'

type HistoryPoint = {
  time: string
  requests: number
  latency: number
}

function MetricCard({ title, value, unit, icon: Icon, note, color }: {
  title: string
  value: string
  unit?: string
  icon: typeof Activity
  note: string
  color: string
}) {
  return (
    <div className="metric-card">
      <div className="metric-top">
        <span>{title}</span>
        <span className={`metric-icon ${color}`}><Icon size={19} /></span>
      </div>
      <div className="metric-value">{value}<small>{unit}</small></div>
      <div className="metric-note">{note}</div>
    </div>
  )
}

function App() {
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const [requestsPerSecond, setRequestsPerSecond] = useState<number | null>(null)
  const [p95Latency, setP95Latency] = useState<number | null>(null)
  const [errorRate, setErrorRate] = useState<number | null>(null)
  const [cacheHitRate, setCacheHitRate] = useState<number | null>(null)
  const [gatewayStatus, setGatewayStatus] = useState<PrometheusSample[]>([])
  const [history, setHistory] = useState<HistoryPoint[]>([])
  const [loading, setLoading] = useState(true)
  const [metricsError, setMetricsError] = useState('')

  const loadMetrics = useCallback(async () => {
    try {
      setMetricsError('')

      const [
        requestResult,
        latencyResult,
        errorResult,
        cacheHitResult,
        cacheMissResult,
        gatewayResult,
      ] = await Promise.all([
        queryPrometheus('sum(rate(gateway_http_requests_total[1m]))'),
        queryPrometheus('histogram_quantile(0.95, sum by (le) (rate(gateway_http_request_duration_seconds_bucket[5m])))'),
        queryPrometheus('100 * sum(rate(gateway_http_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(gateway_http_requests_total[5m])), 1)'),
        queryPrometheus('sum(rate(gateway_cache_requests_total{result="hit"}[5m]))'),
        queryPrometheus('sum(rate(gateway_cache_requests_total{result="miss"}[5m]))'),
        queryPrometheus('up{job=~"gateway-1|gateway-2"}'),
      ])

      const readValue = (samples: PrometheusSample[]) => {
        if (!samples.length) return null
        const value = Number(samples[0].value[1])
        return Number.isFinite(value) ? value : null
      }

      const requests = readValue(requestResult)
      const latencySeconds = readValue(latencyResult)
      const errors = readValue(errorResult)
      const hits = readValue(cacheHitResult)
      const misses = readValue(cacheMissResult)
      const cacheTotal = (hits ?? 0) + (misses ?? 0)
      const hitPercent = cacheTotal > 0 && hits !== null
        ? (hits / cacheTotal) * 100
        : null

      setRequestsPerSecond(requests)
      setP95Latency(latencySeconds === null ? null : latencySeconds * 1000)
      setErrorRate(errors)
      setCacheHitRate(hitPercent)
      setGatewayStatus(gatewayResult)
      setLastUpdated(new Date())

      if (requests !== null || latencySeconds !== null) {
        const point: HistoryPoint = {
          time: new Date().toLocaleTimeString(),
          requests: requests ?? 0,
          latency: latencySeconds === null ? 0 : latencySeconds * 1000,
        }
        setHistory(previous => [...previous, point].slice(-12))
      }
    } catch (error) {
      setMetricsError(
        error instanceof Error ? error.message : 'Unable to load Prometheus metrics'
      )
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadMetrics()
    const timer = window.setInterval(() => void loadMetrics(), 10000)
    return () => window.clearInterval(timer)
  }, [loadMetrics])

  const gatewayIsUp = (job: string) =>
    gatewayStatus.some(sample => sample.metric.job === job && Number(sample.value[1]) === 1)

  const gatewayHasResult = (job: string) =>
    gatewayStatus.some(sample => sample.metric.job === job)

  const format = (value: number | null, digits = 1) =>
    value === null ? '—' : value.toFixed(digits)

  const cacheChartData = [
    { name: 'Cache hits', value: cacheHitRate ?? 0 },
    { name: 'Cache misses', value: cacheHitRate === null ? 0 : 100 - cacheHitRate },
  ]

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-icon"><Activity size={23} /></div>
          <div>
            <strong>GateWatch</strong>
            <span>API GATEWAY</span>
          </div>
        </div>

        <div className="side-label">MONITORING</div>
        <nav>
          <a className="nav-link active" href="#overview"><Activity size={18} /> Overview</a>
          <a className="nav-link" href="#traffic"><Gauge size={18} /> Traffic analytics</a>
          <a className="nav-link" href="#backends"><Server size={18} /> Backend services</a>
          <a className="nav-link" href="#cache"><Database size={18} /> Redis cache</a>
          <a className="nav-link" href="#protection"><ShieldCheck size={18} /> Protection</a>
        </nav>

        <div className="sidebar-bottom">
          <div className="system-status">
            <span className="pulse-dot" />
            {metricsError ? 'Metrics connection issue' : loading ? 'Connecting to Prometheus' : 'Prometheus connected'}
          </div>
          <span className="version">Distributed API Gateway · v1.0</span>
        </div>
      </aside>

      <main className="main-content" id="overview">
        <header className="topbar">
          <div>
            <div className="eyebrow">MONITORING / OVERVIEW</div>
            <h1>Gateway Dashboard</h1>
            <p className="subtitle">Live traffic, performance and infrastructure metrics from Prometheus.</p>
          </div>
          <button className="refresh-button" onClick={() => void loadMetrics()} disabled={loading}>
            <RefreshCw size={16} /> {loading ? 'Loading…' : 'Refresh'}
          </button>
        </header>

        {metricsError && (
          <div className="demo-notice">
            <AlertTriangle size={17} />
            <span>Could not load live metrics: {metricsError}. Check that Prometheus and both gateway instances are running.</span>
          </div>
        )}

        <section className="metric-grid">
          <MetricCard title="Requests / sec" value={format(requestsPerSecond)} unit=" req/s" icon={Activity} note="Combined gateway traffic · last 1 min" color="blue" />
          <MetricCard title="P95 latency" value={format(p95Latency)} unit=" ms" icon={Gauge} note="95th percentile · last 5 min" color="purple" />
          <MetricCard title="HTTP error rate" value={format(errorRate, 2)} unit="%" icon={AlertTriangle} note="5xx responses · last 5 min" color="orange" />
          <MetricCard title="Cache hit rate" value={format(cacheHitRate)} unit="%" icon={Database} note="Redis cache · last 5 min" color="green" />
        </section>

        <section className="chart-grid" id="traffic">
          <div className="panel traffic-panel">
            <div className="panel-heading">
              <div><h2>Request throughput</h2><p>Combined requests per second · live samples every 10 seconds</p></div>
              <span className="live-label"><span className="pulse-dot" /> LIVE DATA</span>
            </div>
            <div className="chart-area">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={history}>
                  <defs>
                    <linearGradient id="blueFill" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#5795ff" stopOpacity={0.28} />
                      <stop offset="100%" stopColor="#5795ff" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid stroke="#283244" strokeDasharray="3 3" />
                  <XAxis dataKey="time" stroke="#8490a5" tickLine={false} axisLine={false} />
                  <YAxis stroke="#8490a5" tickLine={false} axisLine={false} />
                  <Tooltip contentStyle={{ background: '#151d2b', border: '1px solid #303b50', borderRadius: 10 }} />
                  <Area type="monotone" dataKey="requests" name="Requests/sec" stroke="#5795ff" fill="url(#blueFill)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </div>

          <div className="panel latency-panel">
            <div className="panel-heading">
              <div><h2>Request latency</h2><p>P95 response time in milliseconds</p></div>
              <span className="small-tag">LIVE SAMPLES</span>
            </div>
            <div className="chart-area">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={history}>
                  <defs>
                    <linearGradient id="latencyFill" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#34d399" stopOpacity={0.25} />
                      <stop offset="100%" stopColor="#34d399" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid stroke="#283244" strokeDasharray="3 3" />
                  <XAxis dataKey="time" hide />
                  <YAxis stroke="#8490a5" tickLine={false} axisLine={false} />
                  <Tooltip contentStyle={{ background: '#151d2b', border: '1px solid #303b50', borderRadius: 10 }} />
                  <Area type="monotone" dataKey="latency" name="P95 latency (ms)" stroke="#34d399" fill="url(#latencyFill)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </div>
        </section>

        <section className="lower-grid">
          <div className="panel" id="backends">
            <div className="panel-heading">
              <div><h2>Gateway instances</h2><p>Prometheus scrape availability</p></div>
              <span className="small-tag">2 TARGETS</span>
            </div>
            {[
              { job: 'gateway-1', name: 'Gateway 1', address: 'localhost:8080' },
              { job: 'gateway-2', name: 'Gateway 2', address: 'localhost:8081' },
            ].map(gateway => {
              const known = gatewayHasResult(gateway.job)
              const online = gatewayIsUp(gateway.job)
              return (
                <div className="instance-row" key={gateway.job}>
                  <div className="instance-icon"><Server size={20} /></div>
                  <div className="instance-info"><strong>{gateway.name}</strong><span>{gateway.address}</span></div>
                  <span className={`status-pill ${online ? 'online' : ''}`}>
                    {online ? <CheckCircle2 size={14} /> : <AlertTriangle size={14} />}
                    {known ? (online ? 'Online' : 'Down') : 'Unknown'}
                  </span>
                </div>
              )
            })}
            <div className="backend-divider" />
            <div className="panel-heading backend-title"><div><h2>Backend services</h2><p>Configured endpoints · health metrics not yet exposed</p></div></div>
            {['Users service · :9001', 'Products service · :9002', 'Products service 2 · :9003'].map(service => (
              <div className="service-row" key={service}>
                <span className="service-dot" />{service}<span className="service-state">Configured</span>
              </div>
            ))}
          </div>

          <div className="panel" id="cache">
            <div className="panel-heading">
              <div><h2>Redis cache</h2><p>Live hit and miss rate distribution</p></div>
              <Database size={19} className="heading-icon" />
            </div>
            <div className="cache-summary">
              <div><strong>{format(cacheHitRate)}%</strong><span>Hit rate</span></div>
              <div><strong>{cacheHitRate === null ? '—' : `${(100 - cacheHitRate).toFixed(1)}%`}</strong><span>Miss rate</span></div>
            </div>
            <div className="cache-bar"><span style={{ width: `${cacheHitRate ?? 0}%` }} /></div>
            <div className="chart-area cache-chart">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={cacheChartData} layout="vertical">
                  <CartesianGrid stroke="#283244" strokeDasharray="3 3" />
                  <XAxis type="number" domain={[0, 100]} stroke="#8490a5" />
                  <YAxis type="category" dataKey="name" width={90} stroke="#8490a5" />
                  <Tooltip contentStyle={{ background: '#151d2b', border: '1px solid #303b50', borderRadius: 10 }} />
                  <Bar dataKey="value" fill="#34d399" radius={[0, 5, 5, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        </section>

        <section className="panel protection-panel" id="protection">
          <div className="panel-heading">
            <div><h2>Gateway protection</h2><p>Configured protection components</p></div>
            <ShieldCheck size={20} className="heading-icon" />
          </div>
          <div className="protection-grid">
            <div className="protection-item">
              <div className="protection-icon blue"><Zap size={20} /></div>
              <div><strong>Redis rate limiter</strong><span>Distributed token bucket</span></div>
              <span className="status-pill configured"><CheckCircle2 size={14} /> Configured</span>
            </div>
            <div className="protection-item">
              <div className="protection-icon purple"><ShieldCheck size={20} /></div>
              <div><strong>Circuit breakers</strong><span>Individual products backends</span></div>
              <span className="status-pill configured"><CheckCircle2 size={14} /> Configured</span>
            </div>
          </div>
        </section>

        <footer className="footer">
          <span>GateWatch · Distributed API Gateway Monitoring</span>
          <span>Last refreshed: {lastUpdated ? lastUpdated.toLocaleTimeString() : 'Not yet refreshed'}</span>
        </footer>
      </main>
    </div>
  )
}

export default App
