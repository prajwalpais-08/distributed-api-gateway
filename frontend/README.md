# GateWatch frontend — Vercel deployment

This frontend is a Vite + React dashboard. Production metrics are fetched through the Vercel serverless function at `api/metrics.ts`, which reads the gateway's `/metrics` endpoint and calls each backend's `/health` endpoint server-side. The browser does not call Prometheus directly and no Prometheus URL or credential is exposed to the browser.

## Deploy on Vercel

1. Copy the contents of this folder into the existing repository's `frontend/` folder (replace the existing frontend source/config files; do not copy `node_modules`).
2. From the repository root in PowerShell:

   ```powershell
   git add frontend
   git commit -m "Prepare GateWatch dashboard for production deployment"
   git push origin main
   ```

3. In Vercel, choose **Add New → Project**, import `prajwalpais-08/distributed-api-gateway`, and set **Root Directory** to `frontend`.
4. Use these settings:
   - Framework: Vite
   - Build command: `npm run build`
   - Output directory: `dist`
   - Install command: `npm install`
5. Deploy. No environment variables are required for the default Render URLs. Optional overrides are `GATEWAY_BASE_URL`, `USERS_SERVICE_URL`, `PRODUCTS_SERVICE_URL_1`, and `PRODUCTS_SERVICE_URL_2`.

## Notes

- The `/api/metrics` function is a small server-side aggregator. It returns selected gateway counters and backend health states; it does not expose the raw Prometheus query API. Render Free services can sleep during inactivity, so the first dashboard refresh after a long idle period may show a temporary timeout; press Refresh again after the gateway wakes.
- The p95 value is an approximation from the gateway's cumulative histogram buckets, not a PromQL rolling-window percentile.
- The throughput chart estimates requests/second from successive browser refreshes and starts filling after the second successful sample.
- Your existing local Prometheus/Grafana setup remains useful for local development. This deployed dashboard reads the Render gateway metrics endpoint so it does not depend on `host.docker.internal` or a local Prometheus container.
