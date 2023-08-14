// Package smtp implements the Simple Mail Transfer Protocol as defined in RFC 5321.
//
// It also implements the following extensions:
//
//   - 8BITMIME (RFC 1652)
//   - AUTH (RFC 2554)
//   - STARTTLS (RFC 3207)
//   - ENHANCEDSTATUSCODES (RFC 2034)
//   - SMTPUTF8 (RFC 6531)
//   - REQUIRETLS (RFC 8689)
//   - CHUNKING (RFC 3030)
//   - BINARYMIME (RFC 3030)
//
// LMTP (RFC 2033) is also supported.
//
// Additional extensions may be handled by other packages.
package smtp
