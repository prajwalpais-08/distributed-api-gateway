
const PROMETHEUS_BASE = '/prometheus/api/v1'

export interface PrometheusSample {
  metric: Record<string, string>
  value: [number, string]
}

interface PrometheusResponse {
  status: string
  data: {
    result: PrometheusSample[]
  }
}

export async function queryPrometheus(
  query: string
): Promise<PrometheusSample[]> {
  const params = new URLSearchParams({ query })

  const response = await fetch(
    `${PROMETHEUS_BASE}/query?${params.toString()}`
  )

  if (!response.ok) {
    throw new Error(`Prometheus request failed: ${response.status}`)
  }

  const result: PrometheusResponse = await response.json()

  if (result.status !== 'success') {
    throw new Error('Prometheus query was unsuccessful')
  }

  return result.data.result
}
