// Package regclient is used to access OCI registries.
package regclient

import (
	"io"
	"time"

	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/regclient/regclient/config"
	"github.com/regclient/regclient/internal/version"
	"github.com/regclient/regclient/scheme"
	"github.com/regclient/regclient/scheme/ocidir"
	"github.com/regclient/regclient/scheme/reg"
)

const (
	// DefaultUserAgent sets the header on http requests.
	DefaultUserAgent = "regclient/regclient"
	// DockerCertDir default location for docker certs.
	DockerCertDir = "/etc/docker/certs.d"
	// DockerRegistry is the well known name of Docker Hub, "docker.io".
	DockerRegistry = config.DockerRegistry
	// DockerRegistryAuth is the name of Docker Hub seen in docker's config.json.
	DockerRegistryAuth = config.DockerRegistryAuth
	// DockerRegistryDNS is the actual registry DNS name for Docker Hub.
	DockerRegistryDNS = config.DockerRegistryDNS
)

// RegClient is used to access OCI distribution-spec registries.
type RegClient struct {
	hosts       map[string]*config.Host
	hostDefault *config.Host
	log         *logrus.Logger
	regOpts     []reg.Opts
	schemes     map[string]scheme.API
	userAgent   string
}

// Opt functions are used by [New] to create a [*RegClient].
type Opt func(*RegClient)

// New returns a registry client.
func New(opts ...Opt) *RegClient {
	var rc = RegClient{
		hosts:     map[string]*config.Host{},
		userAgent: DefaultUserAgent,
		log:       &logrus.Logger{Out: io.Discard},
		regOpts:   []reg.Opts{},
		schemes:   map[string]scheme.API{},
	}

	info := version.GetInfo()
	if info.VCSTag != "" {
		rc.userAgent = fmt.Sprintf("%s (%s)", rc.userAgent, info.VCSTag)
	} else {
		rc.userAgent = fmt.Sprintf("%s (%s)", rc.userAgent, info.VCSRef)
	}

	// inject Docker Hub settings
	_ = rc.hostSet(*config.HostNewName(config.DockerRegistryAuth))

	for _, opt := range opts {
		opt(&rc)
	}

	// configure regOpts
	hostList := []*config.Host{}
	for _, h := range rc.hosts {
		hostList = append(hostList, h)
	}
	rc.regOpts = append(rc.regOpts,
		reg.WithConfigHosts(hostList),
		reg.WithConfigHostDefault(rc.hostDefault),
		reg.WithLog(rc.log),
		reg.WithUserAgent(rc.userAgent),
	)

	// setup scheme's
	rc.schemes["reg"] = reg.New(rc.regOpts...)
	rc.schemes["ocidir"] = ocidir.New(
		ocidir.WithLog(rc.log),
	)

	rc.log.WithFields(logrus.Fields{
		"VCSRef": info.VCSRef,
		"VCSTag": info.VCSTag,
	}).Debug("regclient initialized")

	return &rc
}

// WithBlobLimit sets the max size for chunked blob uploads which get stored in memory.
//
// Deprecated: replace with WithRegOpts(reg.WithBlobLimit(limit)), see [WithRegOpts] and [reg.WithBlobLimit].
func WithBlobLimit(limit int64) Opt {
	return func(rc *RegClient) {
		rc.regOpts = append(rc.regOpts, reg.WithBlobLimit(limit))
	}
}

// WithBlobSize overrides default blob sizes.
//
// Deprecated: replace with WithRegOpts(reg.WithBlobSize(chunk, max)), see [WithRegOpts] and [reg.WithBlobSize].
func WithBlobSize(chunk, max int64) Opt {
	return func(rc *RegClient) {
		rc.regOpts = append(rc.regOpts, reg.WithBlobSize(chunk, max))
	}
}

// WithCertDir adds a path of certificates to trust similar to Docker's /etc/docker/certs.d.
//
// Deprecated: replace with WithRegOpts(reg.WithCertDirs(path)), see [WithRegOpts] and [reg.WithCertDirs].
func WithCertDir(path ...string) Opt {
	return func(rc *RegClient) {
		rc.regOpts = append(rc.regOpts, reg.WithCertDirs(path))
	}
}

// WithConfigHost adds a list of config host settings.
func WithConfigHost(configHost ...config.Host) Opt {
	return func(rc *RegClient) {
		rc.hostLoad("host", configHost)
	}
}

// WithConfigHostDefault adds default settings for new hosts.
func WithConfigHostDefault(configHost config.Host) Opt {
	return func(rc *RegClient) {
		rc.hostDefault = &configHost
	}
}

// WithConfigHosts adds a list of config host settings.
//
// Deprecated: replace with [WithConfigHost].
func WithConfigHosts(configHosts []config.Host) Opt {
	return WithConfigHost(configHosts...)
}

// WithDockerCerts adds certificates trusted by docker in /etc/docker/certs.d.
func WithDockerCerts() Opt {
	return WithCertDir(DockerCertDir)
}

// WithDockerCreds adds configuration from users docker config with registry logins.
// This changes the default value from the config file, and should be added after the config file is loaded.
func WithDockerCreds() Opt {
	return func(rc *RegClient) {
		configHosts, err := config.DockerLoad()
		if err != nil {
			rc.log.WithFields(logrus.Fields{
				"err": err,
			}).Warn("Failed to load docker creds")
			return
		}
		rc.hostLoad("docker", configHosts)
	}
}

// WithDockerCredsFile adds configuration from a named docker config file with registry logins.
// This changes the default value from the config file, and should be added after the config file is loaded.
func WithDockerCredsFile(fname string) Opt {
	return func(rc *RegClient) {
		configHosts, err := config.DockerLoadFile(fname)
		if err != nil {
			rc.log.WithFields(logrus.Fields{
				"err": err,
			}).Warn("Failed to load docker creds")
			return
		}
		rc.hostLoad("docker-file", configHosts)
	}
}

// WithLog overrides default logrus Logger.
func WithLog(log *logrus.Logger) Opt {
	return func(rc *RegClient) {
		rc.log = log
	}
}

// WithRegOpts passes through opts to the reg scheme.
func WithRegOpts(opts ...reg.Opts) Opt {
	return func(rc *RegClient) {
		if len(opts) == 0 {
			return
		}
		rc.regOpts = append(rc.regOpts, opts...)
	}
}

// WithRetryDelay specifies the time permitted for retry delays.
//
// Deprecated: replace with WithRegOpts(reg.WithDelay(delayInit, delayMax)), see [WithRegOpts] and [reg.WithDelay].
func WithRetryDelay(delayInit, delayMax time.Duration) Opt {
	return func(rc *RegClient) {
		rc.regOpts = append(rc.regOpts, reg.WithDelay(delayInit, delayMax))
	}
}

// WithRetryLimit specifies the number of retries for non-fatal errors.
//
// Deprecated: replace with WithRegOpts(reg.WithRetryLimit(retryLimit)), see [WithRegOpts] and [reg.WithRetryLimit].
func WithRetryLimit(retryLimit int) Opt {
	return func(rc *RegClient) {
		rc.regOpts = append(rc.regOpts, reg.WithRetryLimit(retryLimit))
	}
}

// WithUserAgent specifies the User-Agent http header.
func WithUserAgent(ua string) Opt {
	return func(rc *RegClient) {
		rc.userAgent = ua
	}
}

func (rc *RegClient) hostLoad(src string, hosts []config.Host) {
	for _, configHost := range hosts {
		if configHost.Name == "" {
			// TODO: should this error, warn, or fall back to hostname?
			continue
		}
		if configHost.Name == DockerRegistry || configHost.Name == DockerRegistryDNS || configHost.Name == DockerRegistryAuth {
			configHost.Name = DockerRegistry
			if configHost.Hostname == "" || configHost.Hostname == DockerRegistry || configHost.Hostname == DockerRegistryAuth {
				configHost.Hostname = DockerRegistryDNS
			}
		}
		tls, _ := configHost.TLS.MarshalText()
		rc.log.WithFields(logrus.Fields{
			"name":       configHost.Name,
			"user":       configHost.User,
			"hostname":   configHost.Hostname,
			"helper":     configHost.CredHelper,
			"repoAuth":   configHost.RepoAuth,
			"tls":        string(tls),
			"pathPrefix": configHost.PathPrefix,
			"mirrors":    configHost.Mirrors,
			"blobMax":    configHost.BlobMax,
			"blobChunk":  configHost.BlobChunk,
		}).Debugf("Loading %s config", src)
		err := rc.hostSet(configHost)
		if err != nil {
			rc.log.WithFields(logrus.Fields{
				"host":  configHost.Name,
				"user":  configHost.User,
				"error": err,
			}).Warn("Failed to update host config")
		}
	}
}

func (rc *RegClient) hostSet(newHost config.Host) error {
	name := newHost.Name
	var err error
	if _, ok := rc.hosts[name]; !ok {
		// merge newHost with default host settings
		rc.hosts[name] = config.HostNewDefName(rc.hostDefault, name)
		err = rc.hosts[name].Merge(newHost, nil)
	} else {
		// merge newHost with existing settings
		err = rc.hosts[name].Merge(newHost, rc.log)
	}
	if err != nil {
		return err
	}
	return nil
}
