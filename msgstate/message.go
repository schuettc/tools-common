package msgstate

import "time"

// Message is one message of a thread as a server sends it to the kit's
// thread: the one wire type, so a tool shows threads whether it keeps them in
// threadstore (sift, cull, casebook, muda) or its own file (diazo, galley).
type Message struct {
	ID int64 `json:"id"`
	// Thread names the thread: its id as text, or a tool's own key (a diazo card).
	Thread string    `json:"thread"`
	Who    string    `json:"who"`
	Text   string    `json:"text"`
	When   time.Time `json:"when"`
	// State is where a message Court sent stands; a message with no lifecycle
	// (the agent's reply, a draft) has none.
	State   State `json:"state,omitempty"`
	ReplyTo int64 `json:"reply_to,omitempty"`
	// Line is the agent's one line on what it is doing, while it is working.
	Line string `json:"line,omitempty"`
}
