package config

import (
	"time"
)

const (
	OperatorName      string = "splunk-token-operator"
	OperatorNamespace string = "openshift-splunk-token-operator"

	ApiTokenEnvKey  string = "SPLUNK_API_TOKEN"
	ConfigFile      string = "/etc/splunktoken.d/splunktoken.toml"
	OwnedObjectName string = "splunk-hec-token"
	SecretDataKey   string = "outputs.conf"
	TokenFinalizer  string = "splunktoken.managed.openshift.io/finalizer"
)

type Splunk struct {
	General `toml:"General"`
	Classic Deployment
	HCP     Deployment
}

type General struct {
	TokenMaxAge    time.Duration
	SplunkInstance string
}

type Deployment struct {
	DefaultIndex   string
	AllowedIndexes []string
}
