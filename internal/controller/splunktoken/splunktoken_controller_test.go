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

package splunktoken

import (
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	stv1alpha1 "github.com/openshift/splunk-token-operator/api/v1alpha1"
	"github.com/openshift/splunk-token-operator/config"
	splunkapi "github.com/openshift/splunk-token-operator/internal/splunk"
)

var request = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Namespace: "test-namespace",
		Name:      "cluster",
	},
}

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(stv1alpha1.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	t.Run("exits early if the token is not present", func(t *testing.T) {
		fakeClient := fakeclient.NewClientBuilder().WithScheme(scheme).Build()
		cl := errorClient{
			fakeClient,
			objectNotFound,
		}
		reconciler := SplunkTokenReconciler{
			Client: cl,
			Scheme: scheme,
			SplunkApi: &mockSplunkClient{
				create: createErrorIfCalled,
				delete: deleteErrorIfCalled,
			},
		}
		if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
			t.Errorf("unexpected error during reconcile: %s", err)
		}
	})

	t.Run("deletes external resources and removes finalizer when object is being deleted", func(t *testing.T) {
		splunkToken := testSplunkToken()
		deleteTime := metav1.Now()
		splunkToken.DeletionTimestamp = &deleteTime

		fakeClient := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(&splunkToken).
			Build()

		mockSplunk := mockSplunkClient{
			create: createErrorIfCalled,
			delete: deleteSuccess,
		}

		reconciler := SplunkTokenReconciler{
			Client:    fakeClient,
			Scheme:    scheme,
			SplunkApi: &mockSplunk,
		}

		if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
			t.Errorf("unexpected error during reconcile: %s", err)
		}
		if !mockSplunk.deleteCalled {
			t.Errorf("should have called DeleteToken for token '%s'", splunkToken.Spec.Name)
		}

		var resultToken stv1alpha1.SplunkToken
		err := fakeClient.Get(t.Context(), request.NamespacedName, &resultToken)
		if !kerrors.IsNotFound(err) {
			t.Errorf("expected token to be deleted after reconcile, instead got SplunkToken: %v, err: %s", resultToken, err)
		}
	})

	t.Run("deletes SplunkToken object if past rotation time", func(t *testing.T) {
		splunkToken := testSplunkToken()
		splunkToken.CreationTimestamp = metav1.NewTime(time.Now().Add(-3 * time.Hour))

		fakeClient := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(&splunkToken).
			Build()

		mockSplunk := mockSplunkClient{
			create: createErrorIfCalled,
			delete: deleteSuccess,
		}

		splunkConfig := config.General{TokenMaxAge: time.Hour}

		reconciler := SplunkTokenReconciler{
			Client:       fakeClient,
			Scheme:       scheme,
			SplunkApi:    &mockSplunk,
			SplunkConfig: splunkConfig,
		}

		if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
			t.Errorf("unexpected error during reconcile: %s", err)
		}

		var resultToken stv1alpha1.SplunkToken
		err := fakeClient.Get(t.Context(), request.NamespacedName, &resultToken)
		if err != nil {
			t.Errorf("error checking updated token: %s", err)
		}

		if resultToken.DeletionTimestamp.IsZero() {
			t.Error("SplunkToken object should have DeletionTimestamp")
		}
	})

	t.Run("creates new token if Secret does not exist", func(t *testing.T) {
		splunkToken := testSplunkToken()

		fakeClient := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(&splunkToken).
			Build()

		mockSplunk := mockSplunkClient{
			create: createSuccess,
			delete: deleteErrorIfCalled,
		}

		splunkConfig := config.General{
			TokenMaxAge:    time.Hour,
			SplunkInstance: "<splunk-collector-uri>",
		}

		reconciler := SplunkTokenReconciler{
			Client:       fakeClient,
			Scheme:       scheme,
			SplunkApi:    &mockSplunk,
			SplunkConfig: splunkConfig,
		}

		if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
			t.Errorf("unexpected error during reconcile: %s", err)
		}

		if !mockSplunk.createCalled {
			t.Error("should have called CreateToken")
		}

		var hecSecret corev1.Secret
		err := fakeClient.Get(t.Context(),
			types.NamespacedName{
				Namespace: request.Namespace,
				Name:      config.OwnedObjectName,
			},
			&hecSecret)
		if err != nil {
			t.Errorf("error getting secret: %s", err)
		}

		// base64 encoding of this outputs.conf:
		//
		//     [httpout]
		//     httpEventCollectorToken = <guid-value>
		//     uri = https://http-inputs-<splunk-collector-uri>.splunkcloud.com:443
		wantStr := "W2h0dHBvdXRdCmh0dHBFdmVudENvbGxlY3RvclRva2VuID0gPGd1aWQtdmFsdWU+CnVyaSA9IGh0dHBzOi8vaHR0cC1pbnB1dHMtPHNwbHVuay1jb2xsZWN0b3ItdXJpPi5zcGx1bmtjbG91ZC5jb206NDQz"
		if gotData, ok := hecSecret.Data["outputs.conf"]; !ok {
			keys := slices.Sorted(maps.Keys(hecSecret.Data))
			t.Errorf("token not stored on correct key\nwant: %s\ngot: %v", "outputs.conf", keys)
		} else {
			gotStr := base64.StdEncoding.EncodeToString(gotData)
			if gotStr != wantStr {
				t.Errorf("secret data not formatted correctly\ngot: %s\nwant: %s", gotStr, wantStr)
			}
		}
	})
}

type errorClient struct {
	client.Client
	err func() *kerrors.StatusError
}

func (e errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return e.err()
}

func objectNotFound() *kerrors.StatusError {
	return kerrors.NewNotFound(schema.GroupResource{}, config.OwnedObjectName)
}

type mockSplunkClient struct {
	splunkapi.TokenManager

	createCalled bool
	deleteCalled bool
	create       func() (*splunkapi.HECToken, error)
	delete       func() error
}

func (m *mockSplunkClient) CreateToken(ctx context.Context, token splunkapi.HECToken) (*splunkapi.HECToken, error) {
	m.createCalled = true
	return m.create()
}
func (m *mockSplunkClient) DeleteToken(ctx context.Context, name string) error {
	m.deleteCalled = true
	return m.delete()
}

func createSuccess() (*splunkapi.HECToken, error) {
	token := splunkapi.HECToken{
		Spec: stv1alpha1.SplunkTokenSpec{
			Name: "<internal-cluster-id>",
		},
		Value: "<guid-value>",
	}
	return &token, nil
}

func createErrorIfCalled() (*splunkapi.HECToken, error) {
	return nil, errors.New("should not call CreateToken")
}

func deleteSuccess() error {
	return nil
}

func deleteErrorIfCalled() error {
	return errors.New("should not call DeleteToken")
}

func testSplunkToken() stv1alpha1.SplunkToken {
	token := stv1alpha1.SplunkToken{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         request.Namespace,
			Name:              request.Name,
			CreationTimestamp: metav1.Now(),
		},
		Spec: stv1alpha1.SplunkTokenSpec{
			Name: "<internal-cluster-id>",
		},
	}
	controllerutil.AddFinalizer(&token, config.TokenFinalizer)
	return token
}
