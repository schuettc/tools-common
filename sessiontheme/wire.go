package sessiontheme

// Query distinguishes a valid unassigned lookup from a resolved theme.
// Lookup failures are errors at the transport boundary, not unassigned queries.
type Query struct {
	Assigned bool      `json:"assigned"`
	Theme    *Resolved `json:"theme,omitempty"`
}

// Resolved identifies the remembered proj session, not an agent conversation.
type Resolved struct {
	SessionID  string     `json:"session_id"`
	GroupID    string     `json:"group_id"`
	Assignment Assignment `json:"assignment"`
	Palette    Palette    `json:"palette"`
	Header     Header     `json:"header"`
}
