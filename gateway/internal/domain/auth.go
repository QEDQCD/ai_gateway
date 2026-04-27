package domain

// Status captures the lifecycle values shared across auth-related records.
// Until a hand-written repository boundary is introduced, persistence shapes
// come directly from the generated internal/store package.
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

const (
	ConsoleRoleAdmin  = "admin"
	ConsoleRoleMember = "member"
)
