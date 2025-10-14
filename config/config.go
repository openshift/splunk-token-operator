package config

const (
	OperatorName      string = "splunk-token-operator"
	OperatorNamespace string = "openshift-splunk-token-operator"

	TokenFinalizer  string = "splunktoken.managed.openshift.io/finalizer"
	TokenSecretName string = "splunk-hec-token"
)
