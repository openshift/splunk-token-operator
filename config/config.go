package config

import (
	"time"
)

const (
	OperatorName      string = "splunk-token-operator"
	OperatorNamespace string = "openshift-splunk-token-operator"

	ApiTokenEnvKey string = "SPLUNK_API_TOKEN" // #nosec G101 -- this is not a credential
	ConfigFile     string = "/etc/splunktoken.d/config.toml"
	SecretDataKey  string = "outputs.conf"
	TokenFinalizer string = "splunktoken.managed.openshift.io/finalizer"
)

type Splunk struct {
	General `toml:"General"`
	Classic SplunkIndexes
	HCP     SplunkIndexes
}

type General struct {
	TokenMaxAge    time.Duration
	SplunkInstance string
}

type SplunkIndexes struct {
	DefaultIndex   string
	AllowedIndexes []string
}
