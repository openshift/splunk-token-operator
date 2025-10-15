package config

import "time"

const (
	OperatorName      string = "splunk-token-operator"
	OperatorNamespace string = "openshift-splunk-token-operator"

	OwnedObjectName string = "splunk-hec-token"
	SecretDataKey   string = "outputs.conf"
	TokenFinalizer  string = "splunktoken.managed.openshift.io/finalizer"
)

type Splunk struct {
	TokenMaxAge time.Duration
	URI         string
}
