package auth

import "errors"

var (
	// ErrEmptyChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	ErrEmptyChallenge = errors.New("empty challenge header")
	// ErrInvalidChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	ErrInvalidChallenge = errors.New("invalid challenge header")
	// ErrNoNewChallenge indicates a challenge update did not result in any change
	ErrNoNewChallenge = errors.New("no new challenge")
	// ErrNotFound indicates no credentials found for basic auth
	ErrNotFound = errors.New("no credentials available for basic auth")
	// ErrNotImplemented returned when method has not been implemented yet
	ErrNotImplemented = errors.New("not implemented")
	// ErrParseFailure indicates the WWW-Authenticate header could not be parsed
	ErrParseFailure = errors.New("parse failure")
	// ErrUnauthorized request was not authorized
	ErrUnauthorized = errors.New("unauthorized")
	// ErrUnsupported indicates the request was unsupported
	ErrUnsupported = errors.New("unsupported")
)
