package ocidir

import (
	"context"
	"errors"
	"fmt"

	"github.com/regclient/regclient/scheme"
	"github.com/regclient/regclient/types/errs"
	"github.com/regclient/regclient/types/manifest"
	"github.com/regclient/regclient/types/mediatype"
	v1 "github.com/regclient/regclient/types/oci/v1"
	"github.com/regclient/regclient/types/platform"
	"github.com/regclient/regclient/types/ref"
	"github.com/regclient/regclient/types/referrer"
)

// ReferrerList returns a list of referrers to a given reference
func (o *OCIDir) ReferrerList(ctx context.Context, r ref.Ref, opts ...scheme.ReferrerOpts) (referrer.ReferrerList, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.referrerList(ctx, r, opts...)
}

func (o *OCIDir) referrerList(ctx context.Context, r ref.Ref, opts ...scheme.ReferrerOpts) (referrer.ReferrerList, error) {
	config := scheme.ReferrerConfig{}
	for _, opt := range opts {
		opt(&config)
	}
	rl := referrer.ReferrerList{
		Tags: []string{},
	}
	// select a platform from a manifest list
	if config.Platform != "" {
		p, err := platform.Parse(config.Platform)
		if err != nil {
			return rl, err
		}
		m, err := o.manifestGet(ctx, r)
		if err != nil {
			return rl, err
		}
		for m.IsList() {
			d, err := manifest.GetPlatformDesc(m, &p)
			if err != nil {
				return rl, err
			}
			m, err = o.manifestGet(ctx, r.SetDigest(d.Digest.String()))
			if err != nil {
				return rl, err
			}
		}
		r = r.SetDigest(m.GetDescriptor().Digest.String())
	}
	// if ref is a tag, run a head request for the digest
	if r.Digest == "" {
		m, err := o.manifestGet(ctx, r)
		if err != nil {
			return rl, err
		}
		r = r.SetDigest(m.GetDescriptor().Digest.String())
	}
	rl.Subject = r

	// pull referrer list by tag
	rlTag, err := referrer.FallbackTag(r)
	if err != nil {
		return rl, err
	}
	m, err := o.manifestGet(ctx, rlTag)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			// empty list, initialize a new manifest
			rl.Manifest, err = manifest.New(manifest.WithOrig(v1.Index{
				Versioned: v1.IndexSchemaVersion,
				MediaType: mediatype.OCI1ManifestList,
			}))
			if err != nil {
				return rl, err
			}
			return rl, nil
		}
		return rl, err
	}
	ociML, ok := m.GetOrig().(v1.Index)
	if !ok {
		return rl, fmt.Errorf("manifest is not an OCI index: %s", rlTag.CommonName())
	}
	// update referrer list
	rl.Manifest = m
	rl.Descriptors = ociML.Manifests
	rl.Annotations = ociML.Annotations
	rl.Tags = append(rl.Tags, rlTag.Tag)
	rl = scheme.ReferrerFilter(config, rl)

	return rl, nil
}

// referrerDelete deletes a referrer associated with a manifest
func (o *OCIDir) referrerDelete(ctx context.Context, r ref.Ref, m manifest.Manifest) error {
	// get refers field
	mSubject, ok := m.(manifest.Subjecter)
	if !ok {
		return fmt.Errorf("manifest does not support subject: %w", errs.ErrUnsupportedMediaType)
	}
	subject, err := mSubject.GetSubject()
	if err != nil {
		return err
	}
	// validate/set subject descriptor
	if subject == nil || subject.MediaType == "" || subject.Digest == "" || subject.Size <= 0 {
		return fmt.Errorf("subject is not set%.0w", errs.ErrNotFound)
	}

	// get descriptor for subject
	rSubject := r.SetDigest(subject.Digest.String())

	// pull existing referrer list
	rl, err := o.referrerList(ctx, rSubject)
	if err != nil {
		return err
	}
	err = rl.Delete(m)
	if err != nil {
		return err
	}

	// push updated referrer list by tag
	rlTag, err := referrer.FallbackTag(rSubject)
	if err != nil {
		return err
	}
	if rl.IsEmpty() {
		err = o.tagDelete(ctx, rlTag)
		if err == nil {
			return nil
		}
		// if delete is not supported, fall back to pushing empty list
	}
	return o.manifestPut(ctx, rlTag, rl.Manifest)
}

// referrerPut pushes a new referrer associated with a given reference
func (o *OCIDir) referrerPut(ctx context.Context, r ref.Ref, m manifest.Manifest) error {
	// get subject field
	mSubject, ok := m.(manifest.Subjecter)
	if !ok {
		return fmt.Errorf("manifest does not support subject: %w", errs.ErrUnsupportedMediaType)
	}
	subject, err := mSubject.GetSubject()
	if err != nil {
		return err
	}
	// validate/set subject descriptor
	if subject == nil || subject.MediaType == "" || subject.Digest == "" || subject.Size <= 0 {
		return fmt.Errorf("subject is not set%.0w", errs.ErrNotFound)
	}

	// get descriptor for subject
	rSubject := r.SetDigest(subject.Digest.String())

	// pull existing referrer list
	rl, err := o.referrerList(ctx, rSubject)
	if err != nil {
		return err
	}
	err = rl.Add(m)
	if err != nil {
		return err
	}

	// push updated referrer list by tag
	rlTag, err := referrer.FallbackTag(rSubject)
	if err != nil {
		return err
	}
	return o.manifestPut(ctx, rlTag, rl.Manifest)
}
