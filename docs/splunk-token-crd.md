# Overview

The `splunk-token` custom resource definition is designed to be a management interface for Splunk HTTP Event Collector (HEC) tokens on a hive cluster.

```yaml
apiVersion: managed.openshift.io/v1alpha1
kind: SplunkToken
metadata:
    name: "cluster"
    namespace: "<cluster-hive-namespace>"
spec:
    tokenName: "<internal-cluster-id>"
```

The `tokenName` field refers to the name of the token on Splunk, which is the internal cluster ID.
Since the custom resource mirrors the token itself, the age of the `SplunkToken` custom resource is also the age of the token.
Once the custom resource has aged past a given threshold, the CR and token will be deleted and recreated in order to rotate the secret.
The token can also be rotated manually by deleting the `SplunkToken` object for the cluster.
