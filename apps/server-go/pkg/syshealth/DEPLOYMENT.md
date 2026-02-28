# Deployment Checklist: Dynamic Worker Scaling

## Phase 1: Preparation
- [ ] Verify `gopsutil` is compatible with production OS (Linux/Docker)
- [ ] Ensure Prometheus is scraping `/metrics` endpoint
- [ ] Run all tests: `nx run server-go:test`

## Phase 2: Deployment
- [ ] Deploy server update (adaptive scaling disabled by default)
- [ ] Verify health monitor is collecting metrics: check logs for "system health metrics collected"
- [ ] Verify Prometheus metrics are appearing in Grafana/Prometheus UI:
  - `system_health_score`
  - `system_io_wait_percent`

## Phase 3: Gradual Rollout
- [ ] Enable adaptive scaling for `ChunkEmbedding` worker via API:
  ```bash
  curl -X PATCH http://production-server/api/embeddings/config 
    -H "Content-Type: application/json" 
    -d '{"enable_adaptive_scaling": true, "min_concurrency": 5, "max_concurrency": 50}'
  ```
- [ ] Monitor for 24 hours:
  - Watch `extraction_worker_current_concurrency` for adjustments
  - Check for any unexpected worker crashes or delays
- [ ] Enable for `GraphEmbedding` worker if stable
- [ ] Enable for all remaining workers

## Phase 4: Verification
- [ ] Import Grafana dashboard (if created)
- [ ] Set up Prometheus alerts for Health Score < 33
- [ ] Verify system stability during high load periods
