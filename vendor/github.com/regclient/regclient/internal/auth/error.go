package auth

import (
	"github.com/regclient/regclient/types/errs"
)

var (
	// ErrEmptyChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	//
	// Deprecated: replace with [errs.ErrEmptyChallenge].
	ErrEmptyChallenge = errs.ErrEmptyChallenge
	// ErrInvalidChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	//
	// Deprecated: replace with [errs.ErrInvalidChallenge].
	ErrInvalidChallenge = errs.ErrInvalidChallenge
	// ErrNoNewChallenge indicates a challenge update did not result in any change
	//
	// Deprecated: replace with [errs.ErrNoNewChallenge].
	ErrNoNewChallenge = errs.ErrNoNewChallenge
	// ErrNotFound indicates no credentials found for basic auth
	//
	// Deprecated: replace with [errs.ErrNotFound].
	ErrNotFound = errs.ErrNotFound
	// ErrNotImplemented returned when method has not been implemented yet
	//
	// Deprecated: replace with [errs.ErrNotImplemented].
	ErrNotImplemented = errs.ErrNotImplemented
	// ErrParseFailure indicates the WWW-Authenticate header could not be parsed
	//
	// Deprecated: replace with [errs.ErrParseFailure].
	ErrParseFailure = errs.ErrParsingFailed
	// ErrUnauthorized request was not authorized
	//
	// Deprecated: replace with [errs.ErrUnauthorized].
	ErrUnauthorized = errs.ErrHTTPUnauthorized
	// ErrUnsupported indicates the request was unsupported
	//
	// Deprecated: replace with [errs.ErrUnsupported].
	ErrUnsupported = errs.ErrUnsupported
)
