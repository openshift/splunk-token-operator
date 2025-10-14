package config

import "time"

const (
	OperatorName      string = "splunk-token-operator"
	OperatorNamespace string = "openshift-splunk-token-operator"

	TokenFinalizer  string = "splunktoken.managed.openshift.io/finalizer"
	TokenSecretName string = "splunk-hec-token"
)

type Splunk struct {
	TokenMaxAge time.Duration
}
