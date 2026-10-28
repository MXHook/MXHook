# User Story: Configure RBAC Roles for Team Access

**System:** MXHook
**Personas:** SaaS Series A

---

## User Goal

As a Platform Lead, I want to assign role-based access (admin/operator/developer) to
team members so that product engineers can view logs without modifying routes.

---

## Context

EE feature. Platform team has 3 members with different needs: platform lead (admin),
on-call (operator), product eng (developer). Developer should view logs + deliveries but
not create/delete routes or domains.

---

## Acceptance Criteria

- [ ] Roles: `admin` (full access), `operator` (read/write ops; no domain/route CRUD),
  `developer` (read-only: logs, deliveries, messages).
- [ ] `POST /api/users { email, role }` invites team member with role (admin only).
- [ ] Role enforcement on all API endpoints; 403 for unauthorized operations.
- [ ] Role visible in `GET /api/users/:id`.
- [ ] Admin can change a user's role; cannot demote last admin.

---

## Implementation Notes

### Pseudocode / Architecture

```pseudocode
roles:
  admin:    all endpoints
  operator: GET/POST/PATCH /routes, /deliveries, /dlq, /replay, /logs; no /domains DELETE
  developer: GET /messages, /deliveries, /logs only

middleware:
  require_role(min_role):
    if user.role < min_role: return 403

POST /api/users:
  require_role(admin)
  create user + assign role
  send invite email
```

### Key Files/Modules

- `api/auth_test.go`: role enforcement tests
- `api/server.go`: role middleware
- `api/types.go`: User, Role types

---

## E2E / Verification Checklist

- [ ] Invite developer-role user; verify they can GET /deliveries but not DELETE /domains.
- [ ] Invite operator; verify they can POST /replay but not POST /domains.
- [ ] Admin attempts to demote last admin; verify rejected with clear error.
- [ ] Verify 403 response includes which role is required.

---

## Related Stories

- [[US-0402]](./US-0402-ee-license.md) — Evaluate EE license
- [[US-0404]](./US-0404-analytics.md) — View email volume analytics
