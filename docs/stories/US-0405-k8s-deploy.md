# User Story: Deploy MXHook on Kubernetes via Helm

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Engineer at a Series A company, I want to deploy MXHook on our Kubernetes
cluster using a Helm chart so that it fits our standard infra provisioning workflow.

---

## Context

Platform team manages all services via Helm + ArgoCD. MXHook must have a Helm chart with
standard K8s patterns: liveness/readiness probes, HPA, PodDisruptionBudget, RBAC.

---

## Acceptance Criteria

- [ ] Official Helm chart available in MXHook chart repo.
- [ ] Chart supports: replicas, resource limits, env var injection, secrets via K8s Secrets.
- [ ] Liveness probe: `GET /healthz`; readiness probe: `GET /readyz`.
- [ ] HPA configured for SMTP and worker deployments independently.
- [ ] PodDisruptionBudget ensures ≥ 1 pod available during rolling updates.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
helm chart structure:
  Chart.yaml
  values.yaml:
    smtp.replicas: 2
    smtp.resources: { requests: cpu/mem, limits: cpu/mem }
    worker.replicas: 3
    api.replicas: 2
    queue.backend: postgres
    database.secretName: mxhook-db-secret

deployment:
  livenessProbe:  httpGet /healthz :8080
  readinessProbe: httpGet /readyz  :8080
  envFrom: secretRef mxhook-secrets
```

### Key Files/Modules

- `deploy/helm/`: Helm chart (future)
- `api/health_test.go`: probe endpoint coverage

---

## E2E / Verification Checklist

- [ ] `helm install mxhook ./chart`; verify all pods reach Ready state.
- [ ] Kill one SMTP pod; verify PDB prevents full outage; emails still processed.
- [ ] Scale HPA; verify worker replicas increase under load.
- [ ] Rolling update; verify zero message loss during update.

---

## Related Stories

- [[US-0401]](./US-0401-evaluate-oss.md) — Evaluate MXHook OSS on staging
- [[US-0209]](./US-0209-health-check.md) — Monitor service health and metrics
