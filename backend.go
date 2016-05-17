package smtp

// A SMTP message.
type Message struct {
	// The sender e-mail address.
	From string
	// The recipients e-mail addresses.
	To []string
	// The message data.
	Data []byte
}

// A SMTP server backend.
type Backend interface {
	// Send an e-mail.
	Send(msg *Message) error
}
