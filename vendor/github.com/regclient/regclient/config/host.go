// Package config is used for all regclient configuration settings.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/regclient/regclient/internal/timejson"
)

// TLSConf specifies whether TLS is enabled and verified for a host.
type TLSConf int

const (
	// TLSUndefined indicates TLS is not passed, defaults to Enabled.
	TLSUndefined TLSConf = iota
	// TLSEnabled uses TLS (https) for the connection.
	TLSEnabled
	// TLSInsecure uses TLS but does not verify CA.
	TLSInsecure
	// TLSDisabled does not use TLS (http).
	TLSDisabled
)

const (
	// DockerRegistry is the name resolved in docker images on Hub.
	DockerRegistry = "docker.io"
	// DockerRegistryAuth is the name provided in docker's config for Hub.
	DockerRegistryAuth = "https://index.docker.io/v1/"
	// DockerRegistryDNS is the host to connect to for Hub.
	DockerRegistryDNS = "registry-1.docker.io"
	// defaultExpire is the default time to expire a credential and force re-authentication.
	defaultExpire = time.Hour * 1
	// defaultCredHelperRetry is the time to refresh a credential from a failed credential helper command.
	defaultCredHelperRetry = time.Second * 5
	// defaultConcurrent is the default number of concurrent registry connections.
	defaultConcurrent = 3
	// defaultReqPerSec is the default maximum frequency to send requests to a registry.
	defaultReqPerSec = 0
	// tokenUser is the username returned by credential helpers that indicates the password is an identity token.
	tokenUser = "<token>"
)

// MarshalJSON converts TLSConf to a json string using MarshalText.
func (t TLSConf) MarshalJSON() ([]byte, error) {
	s, err := t.MarshalText()
	if err != nil {
		return []byte(""), err
	}
	return json.Marshal(string(s))
}

// MarshalText converts TLSConf to a string.
func (t TLSConf) MarshalText() ([]byte, error) {
	var s string
	switch t {
	default:
		s = ""
	case TLSEnabled:
		s = "enabled"
	case TLSInsecure:
		s = "insecure"
	case TLSDisabled:
		s = "disabled"
	}
	return []byte(s), nil
}

// UnmarshalJSON converts TLSConf from a json string.
func (t *TLSConf) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	return t.UnmarshalText([]byte(s))
}

// UnmarshalText converts TLSConf from a string.
func (t *TLSConf) UnmarshalText(b []byte) error {
	switch strings.ToLower(string(b)) {
	default:
		return fmt.Errorf("unknown TLS value \"%s\"", b)
	case "":
		*t = TLSUndefined
	case "enabled":
		*t = TLSEnabled
	case "insecure":
		*t = TLSInsecure
	case "disabled":
		*t = TLSDisabled
	}
	return nil
}

// Host defines settings for connecting to a registry.
type Host struct {
	Name          string            `json:"-" yaml:"registry,omitempty"`                  // Name of the registry (required) (yaml configs pass this as a field, json provides this from the object key)
	TLS           TLSConf           `json:"tls,omitempty" yaml:"tls"`                     // TLS setting: enabled (default), disabled, insecure
	RegCert       string            `json:"regcert,omitempty" yaml:"regcert"`             // public pem cert of registry
	ClientCert    string            `json:"clientCert,omitempty" yaml:"clientCert"`       // public pem cert for client (mTLS)
	ClientKey     string            `json:"clientKey,omitempty" yaml:"clientKey"`         // private pem cert for client (mTLS)
	Hostname      string            `json:"hostname,omitempty" yaml:"hostname"`           // hostname of registry, default is the registry name
	User          string            `json:"user,omitempty" yaml:"user"`                   // username, not used with credHelper
	Pass          string            `json:"pass,omitempty" yaml:"pass"`                   // password, not used with credHelper
	Token         string            `json:"token,omitempty" yaml:"token"`                 // token, experimental for specific APIs
	CredHelper    string            `json:"credHelper,omitempty" yaml:"credHelper"`       // credential helper command for requesting logins
	CredExpire    timejson.Duration `json:"credExpire,omitempty" yaml:"credExpire"`       // time until credential expires
	CredHost      string            `json:"credHost,omitempty" yaml:"credHost"`           // used when a helper hostname doesn't match Hostname
	PathPrefix    string            `json:"pathPrefix,omitempty" yaml:"pathPrefix"`       // used for mirrors defined within a repository namespace
	Mirrors       []string          `json:"mirrors,omitempty" yaml:"mirrors"`             // list of other Host Names to use as mirrors
	Priority      uint              `json:"priority,omitempty" yaml:"priority"`           // priority when sorting mirrors, higher priority attempted first
	RepoAuth      bool              `json:"repoAuth,omitempty" yaml:"repoAuth"`           // tracks a separate auth per repo
	API           string            `json:"api,omitempty" yaml:"api"`                     // Deprecated: registry API to use
	APIOpts       map[string]string `json:"apiOpts,omitempty" yaml:"apiOpts"`             // options for APIs
	BlobChunk     int64             `json:"blobChunk,omitempty" yaml:"blobChunk"`         // size of each blob chunk
	BlobMax       int64             `json:"blobMax,omitempty" yaml:"blobMax"`             // threshold to switch to chunked upload, -1 to disable, 0 for regclient.blobMaxPut
	ReqPerSec     float64           `json:"reqPerSec,omitempty" yaml:"reqPerSec"`         // requests per second
	ReqConcurrent int64             `json:"reqConcurrent,omitempty" yaml:"reqConcurrent"` // concurrent requests, default is defaultConcurrent(3)
	Scheme        string            `json:"scheme,omitempty" yaml:"scheme"`               // Deprecated: use TLS instead
	credRefresh   time.Time         `json:"-" yaml:"-"`                                   // internal use, when to refresh credentials
}

// Cred defines a user credential for accessing a registry.
type Cred struct {
	User, Password, Token string
}

// HostNew creates a default Host entry.
func HostNew() *Host {
	h := Host{
		TLS:           TLSEnabled,
		APIOpts:       map[string]string{},
		ReqConcurrent: int64(defaultConcurrent),
		ReqPerSec:     float64(defaultReqPerSec),
	}
	return &h
}

// HostNewDefName creates a host using provided defaults and hostname.
func HostNewDefName(def *Host, name string) *Host {
	var h Host
	if def == nil {
		h = *HostNew()
	} else {
		h = *def
		// configure required defaults
		if h.TLS == TLSUndefined {
			h.TLS = TLSEnabled
		}
		if h.APIOpts == nil {
			h.APIOpts = map[string]string{}
		}
		if h.ReqConcurrent == 0 {
			h.ReqConcurrent = int64(defaultConcurrent)
		}
		if h.ReqPerSec == 0 {
			h.ReqPerSec = float64(defaultReqPerSec)
		}
		// copy any fields that are not passed by value
		if len(h.APIOpts) > 0 {
			orig := h.APIOpts
			h.APIOpts = map[string]string{}
			for k, v := range orig {
				h.APIOpts[k] = v
			}
		}
		if h.Mirrors != nil {
			orig := h.Mirrors
			h.Mirrors = make([]string, len(orig))
			copy(h.Mirrors, orig)
		}
	}
	// configure host
	origName := name
	// Docker Hub is a special case
	if name == DockerRegistryAuth || name == DockerRegistryDNS || name == DockerRegistry {
		h.Name = DockerRegistry
		h.Hostname = DockerRegistryDNS
		h.CredHost = DockerRegistryAuth
		return &h
	}
	// handle http/https prefix
	i := strings.Index(name, "://")
	if i > 0 {
		scheme := name[:i]
		name = name[i+3:]
		if scheme == "http" {
			h.TLS = TLSDisabled
		}
	}
	// trim any repository path
	i = strings.Index(name, "/")
	if i > 0 {
		name = name[:i]
	}
	h.Name = name
	h.Hostname = name
	if origName != name {
		h.CredHost = origName
	}
	return &h
}

// HostNewName creates a default Host with a hostname.
func HostNewName(name string) *Host {
	return HostNewDefName(nil, name)
}

// GetCred returns the credential, fetching from a credential helper if needed.
func (host *Host) GetCred() Cred {
	// refresh from credHelper if needed
	if host.CredHelper != "" && (host.credRefresh.IsZero() || time.Now().After(host.credRefresh)) {
		host.refreshHelper()
	}
	return Cred{User: host.User, Password: host.Pass, Token: host.Token}
}

func (host *Host) refreshHelper() {
	if host.CredHelper == "" {
		return
	}
	if host.CredExpire <= 0 {
		host.CredExpire = timejson.Duration(defaultExpire)
	}
	// run a cred helper, calling get method
	ch := newCredHelper(host.CredHelper, map[string]string{})
	err := ch.get(host)
	if err != nil {
		host.credRefresh = time.Now().Add(defaultCredHelperRetry)
	} else {
		host.credRefresh = time.Now().Add(time.Duration(host.CredExpire))
	}
}

// IsZero returns true if the struct is set to the zero value or the result of [HostNew].
func (host Host) IsZero() bool {
	if host.Name != "" ||
		(host.TLS != TLSUndefined && host.TLS != TLSEnabled) ||
		host.RegCert != "" ||
		host.ClientCert != "" ||
		host.ClientKey != "" ||
		host.Hostname != "" ||
		host.User != "" ||
		host.Pass != "" ||
		host.Token != "" ||
		host.CredHelper != "" ||
		host.CredExpire != 0 ||
		host.CredHost != "" ||
		host.PathPrefix != "" ||
		len(host.Mirrors) != 0 ||
		host.Priority != 0 ||
		host.RepoAuth ||
		len(host.APIOpts) != 0 ||
		host.BlobChunk != 0 ||
		host.BlobMax != 0 ||
		(host.ReqPerSec != 0 && host.ReqPerSec != float64(defaultReqPerSec)) ||
		(host.ReqConcurrent != 0 && host.ReqConcurrent != int64(defaultConcurrent)) ||
		!host.credRefresh.IsZero() {
		return false
	}
	return true
}

// Merge adds fields from a new config host entry.
func (host *Host) Merge(newHost Host, log *logrus.Logger) error {
	name := newHost.Name
	if name == "" {
		name = host.Name
	}
	if log == nil {
		log = &logrus.Logger{Out: io.Discard}
	}

	// merge the existing and new config host
	if host.Name == "" {
		// only set the name if it's not initialized, this shouldn't normally change
		host.Name = newHost.Name
	}

	if newHost.CredHelper == "" && (newHost.Pass != "" || host.Token != "") {
		// unset existing cred helper for user/pass or token
		host.CredHelper = ""
		host.CredExpire = 0
	}
	if newHost.CredHelper != "" && newHost.User == "" && newHost.Pass == "" && newHost.Token == "" {
		// unset existing user/pass/token for cred helper
		host.User = ""
		host.Pass = ""
		host.Token = ""
	}

	if newHost.User != "" {
		if host.User != "" && host.User != newHost.User {
			log.WithFields(logrus.Fields{
				"orig": host.User,
				"new":  newHost.User,
				"host": name,
			}).Warn("Changing login user for registry")
		}
		host.User = newHost.User
	}

	if newHost.Pass != "" {
		if host.Pass != "" && host.Pass != newHost.Pass {
			log.WithFields(logrus.Fields{
				"host": name,
			}).Warn("Changing login password for registry")
		}
		host.Pass = newHost.Pass
	}

	if newHost.Token != "" {
		if host.Token != "" && host.Token != newHost.Token {
			log.WithFields(logrus.Fields{
				"host": name,
			}).Warn("Changing login token for registry")
		}
		host.Token = newHost.Token
	}

	if newHost.CredHelper != "" {
		if host.CredHelper != "" && host.CredHelper != newHost.CredHelper {
			log.WithFields(logrus.Fields{
				"host": name,
				"orig": host.CredHelper,
				"new":  newHost.CredHelper,
			}).Warn("Changing credential helper for registry")
		}
		host.CredHelper = newHost.CredHelper
	}

	if newHost.CredExpire != 0 {
		if host.CredExpire != 0 && host.CredExpire != newHost.CredExpire {
			log.WithFields(logrus.Fields{
				"host": name,
				"orig": host.CredExpire,
				"new":  newHost.CredExpire,
			}).Warn("Changing credential expire for registry")
		}
		host.CredExpire = newHost.CredExpire
	}

	if newHost.CredHost != "" {
		if host.CredHost != "" && host.CredHost != newHost.CredHost {
			log.WithFields(logrus.Fields{
				"host": name,
				"orig": host.CredHost,
				"new":  newHost.CredHost,
			}).Warn("Changing credential host for registry")
		}
		host.CredHost = newHost.CredHost
	}

	if newHost.TLS != TLSUndefined {
		if host.TLS != TLSUndefined && host.TLS != newHost.TLS {
			tlsOrig, _ := host.TLS.MarshalText()
			tlsNew, _ := newHost.TLS.MarshalText()
			log.WithFields(logrus.Fields{
				"orig": string(tlsOrig),
				"new":  string(tlsNew),
				"host": name,
			}).Warn("Changing TLS settings for registry")
		}
		host.TLS = newHost.TLS
	}

	if newHost.RegCert != "" {
		if host.RegCert != "" && host.RegCert != newHost.RegCert {
			log.WithFields(logrus.Fields{
				"orig": host.RegCert,
				"new":  newHost.RegCert,
				"host": name,
			}).Warn("Changing certificate settings for registry")
		}
		host.RegCert = newHost.RegCert
	}

	if newHost.ClientCert != "" {
		if host.ClientCert != "" && host.ClientCert != newHost.ClientCert {
			log.WithFields(logrus.Fields{
				"orig": host.ClientCert,
				"new":  newHost.ClientCert,
				"host": name,
			}).Warn("Changing client certificate settings for registry")
		}
		host.ClientCert = newHost.ClientCert
	}

	if newHost.ClientKey != "" {
		if host.ClientKey != "" && host.ClientKey != newHost.ClientKey {
			log.WithFields(logrus.Fields{
				"host": name,
			}).Warn("Changing client certificate key settings for registry")
		}
		host.ClientKey = newHost.ClientKey
	}

	if newHost.Hostname != "" {
		if host.Hostname != "" && host.Hostname != newHost.Hostname {
			log.WithFields(logrus.Fields{
				"orig": host.Hostname,
				"new":  newHost.Hostname,
				"host": name,
			}).Warn("Changing hostname settings for registry")
		}
		host.Hostname = newHost.Hostname
	}

	if newHost.PathPrefix != "" {
		newHost.PathPrefix = strings.Trim(newHost.PathPrefix, "/") // leading and trailing / are not needed
		if host.PathPrefix != "" && host.PathPrefix != newHost.PathPrefix {
			log.WithFields(logrus.Fields{
				"orig": host.PathPrefix,
				"new":  newHost.PathPrefix,
				"host": name,
			}).Warn("Changing path prefix settings for registry")
		}
		host.PathPrefix = newHost.PathPrefix
	}

	if len(newHost.Mirrors) > 0 {
		if len(host.Mirrors) > 0 && !stringSliceEq(host.Mirrors, newHost.Mirrors) {
			log.WithFields(logrus.Fields{
				"orig": host.Mirrors,
				"new":  newHost.Mirrors,
				"host": name,
			}).Warn("Changing mirror settings for registry")
		}
		host.Mirrors = newHost.Mirrors
	}

	if newHost.Priority != 0 {
		if host.Priority != 0 && host.Priority != newHost.Priority {
			log.WithFields(logrus.Fields{
				"orig": host.Priority,
				"new":  newHost.Priority,
				"host": name,
			}).Warn("Changing priority settings for registry")
		}
		host.Priority = newHost.Priority
	}

	if newHost.RepoAuth {
		host.RepoAuth = newHost.RepoAuth
	}

	// TODO: eventually delete
	if newHost.API != "" {
		log.WithFields(logrus.Fields{
			"api":  newHost.API,
			"host": name,
		}).Warn("API field has been deprecated")
	}

	if len(newHost.APIOpts) > 0 {
		if len(host.APIOpts) > 0 {
			merged := copyMapString(host.APIOpts)
			for k, v := range newHost.APIOpts {
				if host.APIOpts[k] != "" && host.APIOpts[k] != v {
					log.WithFields(logrus.Fields{
						"orig": host.APIOpts[k],
						"new":  newHost.APIOpts[k],
						"opt":  k,
						"host": name,
					}).Warn("Changing APIOpts setting for registry")
				}
				merged[k] = v
			}
			host.APIOpts = merged
		} else {
			host.APIOpts = newHost.APIOpts
		}
	}

	if newHost.BlobChunk > 0 {
		if host.BlobChunk != 0 && host.BlobChunk != newHost.BlobChunk {
			log.WithFields(logrus.Fields{
				"orig": host.BlobChunk,
				"new":  newHost.BlobChunk,
				"host": name,
			}).Warn("Changing blobChunk settings for registry")
		}
		host.BlobChunk = newHost.BlobChunk
	}

	if newHost.BlobMax != 0 {
		if host.BlobMax != 0 && host.BlobMax != newHost.BlobMax {
			log.WithFields(logrus.Fields{
				"orig": host.BlobMax,
				"new":  newHost.BlobMax,
				"host": name,
			}).Warn("Changing blobMax settings for registry")
		}
		host.BlobMax = newHost.BlobMax
	}

	if newHost.ReqPerSec != 0 {
		if host.ReqPerSec != 0 && host.ReqPerSec != newHost.ReqPerSec {
			log.WithFields(logrus.Fields{
				"orig": host.ReqPerSec,
				"new":  newHost.ReqPerSec,
				"host": name,
			}).Warn("Changing reqPerSec settings for registry")
		}
		host.ReqPerSec = newHost.ReqPerSec
	}

	if newHost.ReqConcurrent > 0 {
		if host.ReqConcurrent != 0 && host.ReqConcurrent != newHost.ReqConcurrent {
			log.WithFields(logrus.Fields{
				"orig": host.ReqConcurrent,
				"new":  newHost.ReqConcurrent,
				"host": name,
			}).Warn("Changing reqPerSec settings for registry")
		}
		host.ReqConcurrent = newHost.ReqConcurrent
	}

	return nil
}

func copyMapString(src map[string]string) map[string]string {
	copy := map[string]string{}
	for k, v := range src {
		copy[k] = v
	}
	return copy
}

func stringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
