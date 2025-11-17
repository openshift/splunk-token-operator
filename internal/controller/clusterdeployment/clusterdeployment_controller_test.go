/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clusterdeployment

import (
	"fmt"
	"reflect"
	"testing"

	hivescheme "github.com/openshift/hive/apis"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	stv1alpha1 "github.com/openshift/splunk-token-operator/api/v1alpha1"
	"github.com/openshift/splunk-token-operator/config"
)

var request = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Namespace: "foo-namespace",
		Name:      "foo",
	},
}

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(stv1alpha1.AddToScheme(scheme))
	utilruntime.Must(hivescheme.AddToScheme(scheme))

	for _, tt := range []struct {
		Name                    string
		ClusterDeploymentLabels map[string]string
		WantTokenSpec           stv1alpha1.SplunkTokenSpec
		WantError               error
	}{
		{
			Name: "creates SplunkToken for Classic ClusterDeployment",
			ClusterDeploymentLabels: map[string]string{
				"api.openshift.com/id": "foo-cluster-id",
			},
			WantTokenSpec: stv1alpha1.SplunkTokenSpec{
				Name:           "foo-cluster-id",
				DefaultIndex:   "classic_index",
				AllowedIndexes: []string{"another_classic_index"},
			},
		},
		{
			Name: "creates SplunkToken for management-cluster ClusterDeployment",
			ClusterDeploymentLabels: map[string]string{
				"api.openshift.com/id":                     "foo-cluster-id",
				"ext-hypershift.openshift.io/cluster-type": "management-cluster",
			},
			WantTokenSpec: stv1alpha1.SplunkTokenSpec{
				Name:           "foo-cluster-id",
				DefaultIndex:   "hcp_index",
				AllowedIndexes: []string{"another_hcp_index"},
			},
		},
		{
			Name:      "returns error if clusterID label is not found",
			WantError: fmt.Errorf("label api.openshift.com/id not found on ClusterDeployment"),
		},
	} {
		t.Run(tt.Name, func(t *testing.T) {
			clusterdeployment := &hivev1.ClusterDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo-namespace",
					Name:      "foo",
					Labels:    tt.ClusterDeploymentLabels,
				},
			}

			wantToken := tokenSkeleton()
			wantToken.Spec = tt.WantTokenSpec

			fakeClient := fakeclient.
				NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(clusterdeployment).
				Build()

			testIndexConfig := splunkIndexConfig{
				Classic: config.SplunkIndexes{
					DefaultIndex:   "classic_index",
					AllowedIndexes: []string{"another_classic_index"},
				},
				HCP: config.SplunkIndexes{
					DefaultIndex:   "hcp_index",
					AllowedIndexes: []string{"another_hcp_index"},
				},
			}

			reconciler := ClusterDeploymentReconciler{
				Client: fakeClient,
				Scheme: scheme,
				config: testIndexConfig,
			}
			_, err := reconciler.Reconcile(t.Context(), request)
			if tt.WantError != nil {
				if err.Error() != tt.WantError.Error() {
					t.Fatalf("expected error `%+v` but got `%+v`", tt.WantError, err)
				}
			} else if err != nil {
				t.Fatalf("got unexpected reconcile error %s", err)
			} else {
				gotToken := &stv1alpha1.SplunkToken{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "foo-namespace",
						Name:      "cluster",
					},
				}
				if err := fakeClient.Get(t.Context(), client.ObjectKeyFromObject(gotToken), gotToken); err != nil {
					t.Fatalf("error retrieving SplunkToken %s", err)
				}

				if !reflect.DeepEqual(wantToken.Spec, gotToken.Spec) {
					t.Errorf("did not get expected token spec\n\twant: %+v\n\tgot: %+v",
						wantToken.Spec,
						gotToken.Spec)
				}
			}
		})
	}

	for _, tt := range []struct {
		Name                            string
		CurrentTokenSpec, WantTokenSpec stv1alpha1.SplunkTokenSpec
	}{
		{
			Name: "ends successfully if SplunkToken already has desired spec",
			CurrentTokenSpec: stv1alpha1.SplunkTokenSpec{
				Name:           "foo-cluster-id",
				DefaultIndex:   "splunk_index",
				AllowedIndexes: []string{"another_index"},
			},
			WantTokenSpec: stv1alpha1.SplunkTokenSpec{
				Name:           "foo-cluster-id",
				DefaultIndex:   "splunk_index",
				AllowedIndexes: []string{"another_index"},
			},
		},
		{
			Name: "updates SplunkToken spec if different",
			CurrentTokenSpec: stv1alpha1.SplunkTokenSpec{
				Name:         "foo-cluster-id",
				DefaultIndex: "splunk_index",
			},
			WantTokenSpec: stv1alpha1.SplunkTokenSpec{
				Name:           "foo-cluster-id",
				DefaultIndex:   "splunk_index",
				AllowedIndexes: []string{"another_index"},
			},
		},
	} {
		t.Run(tt.Name, func(t *testing.T) {
			clusterdeployment := &hivev1.ClusterDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo-namespace",
					Name:      "foo",
					Labels: map[string]string{
						"api.openshift.com/id": "foo-cluster-id",
					},
				},
			}

			currentToken := tokenSkeleton()
			currentToken.Spec = tt.CurrentTokenSpec

			fakeClient := fakeclient.
				NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(clusterdeployment, currentToken).
				Build()

			testIndexConfig := splunkIndexConfig{
				Classic: config.SplunkIndexes{
					DefaultIndex:   "splunk_index",
					AllowedIndexes: []string{"another_index"},
				},
				HCP: config.SplunkIndexes{
					DefaultIndex:   "hcp_index",
					AllowedIndexes: []string{"another_hcp_index"},
				},
			}

			reconciler := ClusterDeploymentReconciler{
				Client: fakeClient,
				Scheme: scheme,
				config: testIndexConfig,
			}
			if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
				t.Fatalf("got unexpected error during reconcile: %s", err)
			}

			wantToken := tokenSkeleton()
			wantToken.Spec = tt.WantTokenSpec
			gotToken := &stv1alpha1.SplunkToken{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo-namespace",
					Name:      "cluster",
				},
			}
			if err := fakeClient.Get(t.Context(), client.ObjectKeyFromObject(gotToken), gotToken); err != nil {
				t.Fatalf("unexpected error when retrieving token: %s", err)
			}

			if !reflect.DeepEqual(gotToken.Spec, wantToken.Spec) {
				t.Errorf("did not get expected token spec\n\twant: %+v\n\tgot: %+v",
					wantToken.Spec,
					gotToken.Spec)
			}
		})
	}
}

func tokenSkeleton() *stv1alpha1.SplunkToken {
	return &stv1alpha1.SplunkToken{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo-namespace",
			Name:      "cluster",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "hive.openshift.io/v1",
					Kind:       "ClusterDeployment",
					Name:       "foo",
				},
			},
			ResourceVersion: "1",
		},
	}

}
