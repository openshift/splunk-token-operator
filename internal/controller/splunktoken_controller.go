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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	stv1alpha1 "github.com/openshift/splunk-token-operator/api/v1alpha1"
	"github.com/openshift/splunk-token-operator/config"
	splunkapi "github.com/openshift/splunk-token-operator/internal/splunk"
)

// SplunkTokenReconciler reconciles a SplunkToken object
type SplunkTokenReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	SplunkApi    splunkapi.TokenManager
	SplunkConfig config.General
}

// +kubebuilder:rbac:groups=splunktoken.managed.openshift.io,resources=splunktokens,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=splunktoken.managed.openshift.io,resources=splunktokens/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=splunktoken.managed.openshift.io,resources=splunktokens/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,resourceNames=splunk-hec-token,verbs=get;delete

// Reconcile takes the following actions depending on the state of the SplunkToken:
//   - If the SplunkToken no longer exists there is nothing to do and Reconcile ends.
//   - If the SplunkToken has a deletion timestamp, the HEC Token is deleted from the Splunk server.
//   - If the CreationTimestamp of the SplunkToken is older than the configured MaxAge,
//     the SplunkToken object is deleted so the token can be rotated.
//   - If there is no Secret object for the HEC token,
//     a new token is created on the Splunk server.
//     The Reconciler stores the token value in a Secret,
//     and a SyncSet is created to push the token to the managed cluster.
func (r *SplunkTokenReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("namespace", req.Namespace)
	log.Info("reconciling splunk token")

	var tokenObject stv1alpha1.SplunkToken
	err := r.Get(ctx, req.NamespacedName, &tokenObject)
	if errors.IsNotFound(err) {
		log.Info("token not found")
		return ctrl.Result{}, nil
	} else if err != nil {
		log.Error(err, "error retrieving SplunkToken")
		return ctrl.Result{}, err
	}

	if !tokenObject.DeletionTimestamp.IsZero() {
		log.Info("SplunkToken has deletion timestamp, deleting HEC token from Splunk server")
		if err := r.SplunkApi.DeleteToken(ctx, tokenObject.Spec.Name); err != nil {
			log.Error(err, "error deleting HEC token from Splunk")
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(&tokenObject, config.TokenFinalizer)
		if err := r.Update(ctx, &tokenObject); err != nil {
			log.Error(err, "error removing finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	currentTime := time.Now()
	tokenRotationDeadline := tokenObject.CreationTimestamp.Add(r.SplunkConfig.TokenMaxAge)
	if currentTime.After(tokenRotationDeadline) {
		log.Info("SplunkToken is stale, rotating")
		if err := r.Delete(ctx, &tokenObject); err != nil {
			log.Error(err, "error deleting SplunkToken object")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	ownedObjectKey := types.NamespacedName{
		Namespace: req.Namespace,
		Name:      config.OwnedObjectName,
	}
	var tokenSecret corev1.Secret
	if err := r.Get(ctx, ownedObjectKey, &tokenSecret); errors.IsNotFound(err) {
		log.Info("token Secret not found, requesting new token from Splunk")
		if controllerutil.AddFinalizer(&tokenObject, config.TokenFinalizer) {
			if err := r.Update(ctx, &tokenObject); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("finalizer added to SplunkToken")
		}
		tokenOptions := splunkapi.HECToken{
			Spec: tokenObject.Spec,
		}
		hecToken, err := r.SplunkApi.CreateToken(ctx, tokenOptions)
		if err != nil {
			log.Error(err, "error creating HEC token")
			return ctrl.Result{}, err
		}
		r.newSecretObject(req.Namespace, hecToken.Value, &tokenSecret)
		if err := controllerutil.SetControllerReference(&tokenObject, &tokenSecret, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, &tokenSecret); err != nil {
			log.Error(err, "error creating Secret object")
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "unable to fetch token Secret")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SplunkTokenReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&stv1alpha1.SplunkToken{}).
		Named("splunktoken").
		Owns(&corev1.Secret{}).
		Complete(r)
}

func (r *SplunkTokenReconciler) newSecretObject(namespace, tokenValue string, secret *corev1.Secret) {
	secret.Name = config.OwnedObjectName
	secret.Namespace = namespace
	outputsConf := `[httpout]
httpEventCollectorToken = %s
uri = %s`
	data := fmt.Appendf([]byte{}, outputsConf, tokenValue, r.collectorUri())
	secret.Data = map[string][]byte{
		config.SecretDataKey: data,
	}
	truePtr := true
	secret.Immutable = &truePtr
}

func (r *SplunkTokenReconciler) collectorUri() string {
	return fmt.Sprintf("https://http-inputs-%s.splunkcloud.com:443", r.SplunkConfig.SplunkInstance)
}
