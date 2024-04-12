package types

import "github.com/regclient/regclient/types/errs"

var (
	// ErrAllRequestsFailed when there are no mirrors left to try
	//
	// Deprecated: replace with [errs.ErrAllRequestsFailed].
	ErrAllRequestsFailed = errs.ErrAllRequestsFailed
	// ErrAPINotFound if an api is not available for the host
	//
	// Deprecated: replace with [errs.ErrAPINotFound].
	ErrAPINotFound = errs.ErrAPINotFound
	// ErrBackoffLimit maximum backoff attempts reached
	//
	// Deprecated: replace with [errs.ErrBackoffLimit].
	ErrBackoffLimit = errs.ErrBackoffLimit
	// ErrCanceled if the context was canceled
	//
	// Deprecated: replace with [errs.ErrCanceled].
	ErrCanceled = errs.ErrCanceled
	// ErrDigestMismatch if the expected digest wasn't received
	//
	// Deprecated: replace with [errs.ErrDigestMismatch].
	ErrDigestMismatch = errs.ErrDigestMismatch
	// ErrEmptyChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	//
	// Deprecated: replace with [errs.ErrEmptyChallenge].
	ErrEmptyChallenge = errs.ErrEmptyChallenge
	// ErrFileDeleted indicates a requested file has been deleted
	//
	// Deprecated: replace with [errs.ErrFileDeleted].
	ErrFileDeleted = errs.ErrFileDeleted
	// ErrFileNotFound indicates a requested file is not found
	//
	// Deprecated: replace with [errs.ErrFileNotFound].
	ErrFileNotFound = errs.ErrFileNotFound
	// ErrHTTPStatus if the http status code was unexpected
	//
	// Deprecated: replace with [errs.ErrHTTPStatus].
	ErrHTTPStatus = errs.ErrHTTPStatus
	// ErrInvalidChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	//
	// Deprecated: replace with [errs.ErrInvalidChallenge].
	ErrInvalidChallenge = errs.ErrInvalidChallenge
	// ErrInvalidReference indicates the reference to an image is has an invalid syntax
	//
	// Deprecated: replace with [errs.ErrInvalidReference].
	ErrInvalidReference = errs.ErrInvalidReference
	// ErrLoopDetected indicates a child node points back to the parent
	//
	// Deprecated: replace with [errs.ErrLoopDetected].
	ErrLoopDetected = errs.ErrLoopDetected
	// ErrManifestNotSet indicates the manifest is not set, it must be pulled with a ManifestGet first
	//
	// Deprecated: replace with [errs.ErrManifestNotSet].
	ErrManifestNotSet = errs.ErrManifestNotSet
	// ErrMissingAnnotation returned when a needed annotation is not found
	//
	// Deprecated: replace with [errs.ErrMissingAnnotation].
	ErrMissingAnnotation = errs.ErrMissingAnnotation
	// ErrMissingDigest returned when image reference does not include a digest
	//
	// Deprecated: replace with [errs.ErrMissingDigest].
	ErrMissingDigest = errs.ErrMissingDigest
	// ErrMissingLocation returned when the location header is missing
	//
	// Deprecated: replace with [errs.ErrMissingLocation].
	ErrMissingLocation = errs.ErrMissingLocation
	// ErrMissingName returned when name missing for host
	//
	// Deprecated: replace with [errs.ErrMissingName].
	ErrMissingName = errs.ErrMissingName
	// ErrMissingTag returned when image reference does not include a tag
	//
	// Deprecated: replace with [errs.ErrMissingTag].
	ErrMissingTag = errs.ErrMissingTag
	// ErrMissingTagOrDigest returned when image reference does not include a tag or digest
	//
	// Deprecated: replace with [errs.ErrMissingTagOrDigest].
	ErrMissingTagOrDigest = errs.ErrMissingTagOrDigest
	// ErrMismatch returned when a comparison detects a difference
	//
	// Deprecated: replace with [errs.ErrMismatch].
	ErrMismatch = errs.ErrMismatch
	// ErrMountReturnedLocation when a blob mount fails but a location header is received
	//
	// Deprecated: replace with [errs.ErrMountReturnedLocation].
	ErrMountReturnedLocation = errs.ErrMountReturnedLocation
	// ErrNoNewChallenge indicates a challenge update did not result in any change
	//
	// Deprecated: replace with [errs.ErrNoNewChallenge].
	ErrNoNewChallenge = errs.ErrNoNewChallenge
	// ErrNotFound isn't there, search for your value elsewhere
	//
	// Deprecated: replace with [errs.ErrNotFound].
	ErrNotFound = errs.ErrNotFound
	// ErrNotImplemented returned when method has not been implemented yet
	//
	// Deprecated: replace with [errs.ErrNotImplemented].
	ErrNotImplemented = errs.ErrNotImplemented
	// ErrNotRetryable indicates the process cannot be retried
	//
	// Deprecated: replace with [errs.ErrNotRetryable].
	ErrNotRetryable = errs.ErrNotRetryable
	// ErrParsingFailed when a string cannot be parsed
	//
	// Deprecated: replace with [errs.ErrParsingFailed].
	ErrParsingFailed = errs.ErrParsingFailed
	// ErrRetryNeeded indicates a request needs to be retried
	//
	// Deprecated: replace with [errs.ErrRetryNeeded].
	ErrRetryNeeded = errs.ErrRetryNeeded
	// ErrShortRead if contents are less than expected the size
	//
	// Deprecated: replace with [errs.ErrShortRead].
	ErrShortRead = errs.ErrShortRead
	// ErrSizeLimitExceeded if contents exceed the size limit
	//
	// Deprecated: replace with [errs.ErrSizeLimitExceeded].
	ErrSizeLimitExceeded = errs.ErrSizeLimitExceeded
	// ErrUnavailable when a requested value is not available
	//
	// Deprecated: replace with [errs.ErrUnavailable].
	ErrUnavailable = errs.ErrUnavailable
	// ErrUnsupported indicates the request was unsupported
	//
	// Deprecated: replace with [errs.ErrUnsupported].
	ErrUnsupported = errs.ErrUnsupported
	// ErrUnsupportedAPI happens when an API is not supported on a registry
	//
	// Deprecated: replace with [errs.ErrUnsupportedAPI].
	ErrUnsupportedAPI = errs.ErrUnsupportedAPI
	// ErrUnsupportedConfigVersion happens when config file version is greater than this command supports
	//
	// Deprecated: replace with [errs.ErrUnsupportedConfigVersion].
	ErrUnsupportedConfigVersion = errs.ErrUnsupportedConfigVersion
	// ErrUnsupportedMediaType returned when media type is unknown or unsupported
	//
	// Deprecated: replace with [errs.ErrUnsupportedMediaType].
	ErrUnsupportedMediaType = errs.ErrUnsupportedMediaType
	// ErrHTTPRateLimit when requests exceed server rate limit
	//
	// Deprecated: replace with [errs.ErrHTTPRateLimit].
	ErrHTTPRateLimit = errs.ErrHTTPRateLimit
	// ErrHTTPUnauthorized when authentication fails
	//
	// Deprecated: replace with [errs.ErrHTTPUnauthorized].
	ErrHTTPUnauthorized = errs.ErrHTTPUnauthorized
)
