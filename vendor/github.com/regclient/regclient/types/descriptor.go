package types

import "github.com/regclient/regclient/types/descriptor"

type (
	// Descriptor is used in manifests to refer to content by media type, size, and digest.
	//
	// Deprecated: replace with [descriptor.Descriptor].
	Descriptor = descriptor.Descriptor
	// MatchOpt defines conditions for a match descriptor.
	//
	// Deprecated: replace with [descriptor.MatchOpt].
	MatchOpt = descriptor.MatchOpt
)

var (
	// EmptyData is the content of the empty JSON descriptor. See [mediatype.OCI1Empty].
	//
	// Deprecated: replace with [descriptor.EmptyData].
	EmptyData = descriptor.EmptyData
	// EmptyDigest is the digest of the empty JSON descriptor. See [mediatype.OCI1Empty].
	//
	// Deprecated: replace with [descriptor.EmptyDigest].
	EmptyDigest          = descriptor.EmptyDigest
	DescriptorListFilter = descriptor.DescriptorListFilter
	DescriptorListSearch = descriptor.DescriptorListSearch
)
