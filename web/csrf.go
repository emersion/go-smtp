// Package xsrftoken provides methods for generating and validating secure XSRF tokens.
package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type CSRF struct {
	// Key is a secret key for your application.
	Key string
	// ID is a unique identifier for the user.
	Id string
	// actionID is the action the user is taking (e.g. POSTing to a particular path)
	Action string
	// The duration that XSRF tokens are valid. 20 * time.Minute
	Timeout time.Duration
}

func NewCSRF(action string, id string, timeout time.Duration) *CSRF {
	return &CSRF{Key: "45585b28a652a82025397c44e0addc449c4d451c", Id: id, Action: action, Timeout: timeout * time.Minute}
}

// Generate returns a URL-safe secure XSRF token that expires in 24 hours.
func (c *CSRF) Generate() string {
	return c.generateTokenAtTime(c.Key, c.Id, c.Action, time.Now())
}

// generateTokenAtTime is like Generate, but returns a token that expires 20 minutes from now.
func (c *CSRF) generateTokenAtTime(key, userID, actionID string, now time.Time) string {
	h := hmac.New(sha1.New, []byte(key))
	fmt.Fprintf(h, "%s:%s:%d", clean(userID), clean(actionID), now.UnixNano())
	tok := fmt.Sprintf("%s:%d", h.Sum(nil), now.UnixNano())
	return base64.URLEncoding.EncodeToString([]byte(tok))
}

// Valid returns true if token is a valid, unexpired token returned by Generate.
func (c *CSRF) Valid(token string) bool {
	return c.validTokenAtTime(token, c.Key, c.Id, c.Action, time.Now())
}

// validTokenAtTime is like Valid, but it uses now to check if the token is expired.
func (c *CSRF) validTokenAtTime(token, key, userID, actionID string, now time.Time) bool {
	// Decode the token.
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return false
	}

	// Extract the issue time of the token.
	sep := bytes.LastIndex(data, []byte{':'})
	if sep < 0 {
		return false
	}
	nanos, err := strconv.ParseInt(string(data[sep+1:]), 10, 64)
	if err != nil {
		return false
	}
	issueTime := time.Unix(0, nanos)

	// Check that the token is not expired.
	if now.Sub(issueTime) >= c.Timeout {
		return false
	}

	// Check that the token is not from the future.
	// Allow 1 minute grace period in case the token is being verified on a
	// machine whose clock is behind the machine that issued the token.
	if issueTime.After(now.Add(1 * time.Minute)) {
		return false
	}

	expected := c.generateTokenAtTime(key, userID, actionID, issueTime)

	// Check that the token matches the expected value.
	// Use constant time comparison to avoid timing attacks.
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

// clean sanitizes a string for inclusion in a token by replacing all ":"s.
func clean(s string) string {
	return strings.Replace(s, ":", "_", -1)
}
