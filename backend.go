package smtp

import (
	"errors"
	"io"
)

var (
	ErrAuthRequired    = errors.New("Please authenticate first")
	ErrAuthUnsupported = errors.New("Authentication not supported")
)

// A SMTP server backend.
type Backend interface {
	// Authenticate a user. Return smtp.ErrAuthUnsupported if you don't want to
	// support this.
	Login(state *ConnectionState, username, password string) (Session, error)

	// Called if the client attempts to send mail without logging in first.
	// Return smtp.ErrAuthRequired if you don't want to support this.
	AnonymousLogin(state *ConnectionState) (Session, error)
}

type BodyType string

const (
	Body7Bit       BodyType = "7BIT"
	Body8BitMIME   BodyType = "8BITMIME"
	BodyBinaryMIME BodyType = "BINARYMIME"
)

type DSNReturn string

const (
	// Attach the full copy of the message to any DSN that indicates
	// a failure. Non-failure DSNs always contain the header only.
	ReturnFull DSNReturn = "FULL"

	// Attach only header of the message to any DSN that indicates a
	// failure.
	ReturnHeaders DSNReturn = "HDRS"
)

// MailOptions contains custom arguments that were
// passed as an argument to the MAIL command.
type MailOptions struct {
	// Value of BODY= argument, 7BIT, 8BITMIME or BINARYMIME.
	Body BodyType

	// Size of the body. Can be 0 if not specified by client.
	Size int

	// TLS is required for the message transmission.
	//
	// The message should be rejected if it can't be transmitted
	// with TLS.
	RequireTLS bool

	// The message envelope or message header contains UTF-8-encoded strings.
	// This flag is set by SMTPUTF8-aware (RFC 6531) client.
	UTF8 bool

	// The authorization identity asserted by the message sender in decoded
	// form with angle brackets stripped.
	//
	// nil value indicates missing AUTH, non-nil empty string indicates
	// AUTH=<>.
	//
	// Defined in RFC 4954.
	Auth *string

	// Whether the full message or header only should be returned in
	// failure DSNs.
	//
	// Defined in RFC 3461. Ignored if the server does not support DSN
	// extension.
	Return DSNReturn

	// Envelope ID identifier. Returned in any DSN for the message.
	//
	// Not in xtext encoding. go-smtp restricts value to printable US-ASCII
	// as required by specification.
	//
	// Defined in RFC 3461. Ignored if the server does not support DSN
	// extension.
	EnvelopeID string
}

type DSNNotify string

const (
	NotifyNever   DSNNotify = "NEVER"
	NotifySuccess DSNNotify = "SUCCESS"
	NotifyDelayed DSNNotify = "DELAYED"
	NotifyFailure DSNNotify = "FAILURE"
)

type RcptOptions struct {
	// When DSN should be generated for this recipient.
	// As described in RFC 3461.
	Notify []DSNNotify

	// Original message recipient as described in RFC 3461.
	//
	// Value of OriginalRecipient is preserved as is. No xtext
	// encoding/decoding or sanitization is done irregardless of
	// OriginalRecipientType.
	OriginalRecipient     string
	OriginalRecipientType string
}

// Session is used by servers to respond to an SMTP client.
//
// The methods are called when the remote client issues the matching command.
type Session interface {
	// Discard currently processed message.
	Reset()

	// Free all resources associated with session.
	Logout() error

	// Set return path for currently processed message.
	Mail(from string, opts MailOptions) error
	// Add recipient for currently processed message.
	Rcpt(to string, opts RcptOptions) error
	// Set currently processed message contents and send it.
	Data(r io.Reader) error
}

// LMTPSession is an add-on interface for Session. It can be implemented by
// LMTP servers to provide extra functionality.
type LMTPSession interface {
	// LMTPData is the LMTP-specific version of Data method.
	// It can be optionally implemented by the backend to provide
	// per-recipient status information when it is used over LMTP
	// protocol.
	//
	// LMTPData implementation sets status information using passed
	// StatusCollector by calling SetStatus once per each AddRcpt
	// call, even if AddRcpt was called multiple times with
	// the same argument. SetStatus must not be called after
	// LMTPData returns.
	//
	// Return value of LMTPData itself is used as a status for
	// recipients that got no status set before using StatusCollector.
	LMTPData(r io.Reader, status StatusCollector) error
}

// StatusCollector allows a backend to provide per-recipient status
// information.
type StatusCollector interface {
	SetStatus(rcptTo string, err error)
}
