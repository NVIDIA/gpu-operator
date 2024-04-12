package types

import (
	"github.com/regclient/regclient/types/mediatype"
)

const (
	// MediaTypeDocker1Manifest deprecated media type for docker schema1 manifests.
	//
	// Deprecated: replace with [mediatype.Docker1Manifest].
	MediaTypeDocker1Manifest = mediatype.Docker1Manifest
	// MediaTypeDocker1ManifestSigned is a deprecated schema1 manifest with jws signing.
	//
	// Deprecated: replace with [mediatype.Docker1ManifestSigned].
	MediaTypeDocker1ManifestSigned = mediatype.Docker1ManifestSigned
	// MediaTypeDocker2Manifest is the media type when pulling manifests from a v2 registry.
	//
	// Deprecated: replace with [mediatype.Docker2Manifest].
	MediaTypeDocker2Manifest = mediatype.Docker2Manifest
	// MediaTypeDocker2ManifestList is the media type when pulling a manifest list from a v2 registry.
	//
	// Deprecated: replace with [mediatype.Docker2ManifestList].
	MediaTypeDocker2ManifestList = mediatype.Docker2ManifestList
	// MediaTypeDocker2ImageConfig is for the configuration json object media type.
	//
	// Deprecated: replace with [mediatype.Docker2ImageConfig].
	MediaTypeDocker2ImageConfig = mediatype.Docker2ImageConfig
	// MediaTypeOCI1Artifact EXPERIMENTAL OCI v1 artifact media type.
	//
	// Deprecated: replace with [mediatype.OCI1Artifact].
	MediaTypeOCI1Artifact = mediatype.OCI1Artifact
	// MediaTypeOCI1Manifest OCI v1 manifest media type.
	//
	// Deprecated: replace with [mediatype.OCI1Manifest].
	MediaTypeOCI1Manifest = mediatype.OCI1Manifest
	// MediaTypeOCI1ManifestList OCI v1 manifest list media type.
	//
	// Deprecated: replace with [mediatype.OCI1ManifestList].
	MediaTypeOCI1ManifestList = mediatype.OCI1ManifestList
	// MediaTypeOCI1ImageConfig OCI v1 configuration json object media type.
	//
	// Deprecated: replace with [mediatype.OCI1ImageConfig].
	MediaTypeOCI1ImageConfig = mediatype.OCI1ImageConfig
	// MediaTypeDocker2LayerGzip is the default compressed layer for docker schema2.
	//
	// Deprecated: replace with [mediatype.Docker2LayerGzip].
	MediaTypeDocker2LayerGzip = mediatype.Docker2LayerGzip
	// MediaTypeDocker2ForeignLayer is the default compressed layer for foreign layers in docker schema2.
	//
	// Deprecated: replace with [mediatype.Docker2ForeignLayer].
	MediaTypeDocker2ForeignLayer = mediatype.Docker2ForeignLayer
	// MediaTypeOCI1Layer is the uncompressed layer for OCIv1.
	//
	// Deprecated: replace with [mediatype.OCI1Layer].
	MediaTypeOCI1Layer = mediatype.OCI1Layer
	// MediaTypeOCI1LayerGzip is the gzip compressed layer for OCI v1.
	//
	// Deprecated: replace with [mediatype.OCI1LayerGzip].
	MediaTypeOCI1LayerGzip = mediatype.OCI1LayerGzip
	// MediaTypeOCI1LayerZstd is the zstd compressed layer for OCI v1.
	//
	// Deprecated: replace with [mediatype.OCI1LayerZstd].
	MediaTypeOCI1LayerZstd = mediatype.OCI1LayerZstd
	// MediaTypeOCI1ForeignLayer is the foreign layer for OCI v1.
	//
	// Deprecated: replace with [mediatype.OCI1ForeignLayer].
	MediaTypeOCI1ForeignLayer = mediatype.OCI1ForeignLayer
	// MediaTypeOCI1ForeignLayerGzip is the gzip compressed foreign layer for OCI v1.
	//
	// Deprecated: replace with [mediatype.OCI1ForeignLayerGzip].
	MediaTypeOCI1ForeignLayerGzip = mediatype.OCI1ForeignLayerGzip
	// MediaTypeOCI1ForeignLayerZstd is the zstd compressed foreign layer for OCI v1.
	//
	// Deprecated: replace with [mediatype.OCI1ForeignLayerZstd].
	MediaTypeOCI1ForeignLayerZstd = mediatype.OCI1ForeignLayerZstd
	// MediaTypeOCI1Empty is used for blobs containing the empty JSON data `{}`.
	//
	// Deprecated: replace with [mediatype.OCI1Empty].
	MediaTypeOCI1Empty = mediatype.OCI1Empty
	// MediaTypeBuildkitCacheConfig is used by buildkit cache images.
	//
	// Deprecated: replace with [mediatype.BuildkitCacheConfig].
	MediaTypeBuildkitCacheConfig = mediatype.BuildkitCacheConfig
)

var (
	// Base cleans the Content-Type header to return only the lower case base media type.
	//
	// Deprecated: replace with [mediatype.Base].
	MediaTypeBase = mediatype.Base
)
