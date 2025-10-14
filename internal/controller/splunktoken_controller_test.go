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

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openshift/splunk-token-operator/config"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	stv1alpha1 "github.com/openshift/splunk-token-operator/api/v1alpha1"
	splunkapi "github.com/openshift/splunk-token-operator/internal/splunk"
)

var request = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Namespace: "test-namespace",
		Name:      config.TokenSecretName,
	},
}

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := stv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("error adding SplunkToken to Scheme: %s", err)
	}

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
			t.Errorf("got unexpected error during reconcile: %s", err)
		}
	})

	t.Run("deletes external resources and removes finalizer when object is being deleted", func(t *testing.T) {
		splunkToken := stv1alpha1.SplunkToken{
			TypeMeta: metav1.TypeMeta{
				Kind:       "SplunkToken",
				APIVersion: "splunktoken.managed.openshift.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      config.TokenSecretName,
				DeletionTimestamp: &metav1.Time{
					Time: time.Now(),
				},
			},
			Spec: stv1alpha1.SplunkTokenSpec{
				Name: "internal-cluster-id",
			},
		}
		controllerutil.AddFinalizer(&splunkToken, config.TokenFinalizer)

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
			t.Errorf("got unexpected error during reconcile: %s", err)
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
}

type errorClient struct {
	client.Client
	err func() *kerrors.StatusError
}

func (e errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return e.err()
}

func objectNotFound() *kerrors.StatusError {
	return kerrors.NewNotFound(schema.GroupResource{}, config.TokenSecretName)
}

type mockSplunkClient struct {
	splunkapi.TokenManager

	deleteCalled bool
	create       func() (*splunkapi.HECToken, error)
	delete       func() error
}

func (m *mockSplunkClient) CreateToken(ctx context.Context, token *splunkapi.HECToken) (*splunkapi.HECToken, error) {
	return m.create()
}
func (m *mockSplunkClient) DeleteToken(ctx context.Context, name string) error {
	m.deleteCalled = true
	return m.delete()
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
