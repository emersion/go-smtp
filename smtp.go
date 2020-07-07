// Package smtp implements the Simple Mail Transfer Protocol as defined in RFC 5321.
//
// It also implements the following extensions:
//
//  8BITMIME: RFC 1652
//  AUTH: RFC 2554
//  STARTTLS: RFC 3207
//  ENHANCEDSTATUSCODES: RFC 2034
//  SMTPUTF8: RFC 6531
//  REQUIRETLS: RFC 8689
//  CHUNKING: RFC 3030
//  BINARYMIME: RFC 3030
//
// LMTP (RFC 2033) is also supported.
//
// Additional extensions may be handled by other packages.
package smtp

import (
	"errors"
	"strings"
)

// validateLine checks to see if a line has CR or LF as per RFC 5321
func validateLine(line string) error {
	if strings.ContainsAny(line, "\n\r") {
		return errors.New("smtp: A line must not contain CR or LF")
	}
	return nil
}
