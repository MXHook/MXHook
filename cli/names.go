package cli

// Runtime roles accepted by --runtime-role.
const (
	roleAll    = "all"
	roleEdge   = "edge"
	roleWorker = "worker"
)

// Queue and state backends accepted by --queue-backend.
const (
	backendSQLite     = "sqlite"
	backendPostgres   = "postgres"
	backendCloudflare = "cloudflare"
)
