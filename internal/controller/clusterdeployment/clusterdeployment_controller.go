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
	"context"
	"fmt"
	"reflect"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	stv1alpha1 "github.com/openshift/splunk-token-operator/api/v1alpha1"
	"github.com/openshift/splunk-token-operator/config"
)

// ClusterDeploymentReconciler reconciles a ClusterDeployment object
type ClusterDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config SplunkIndexConfig
}

type SplunkIndexConfig struct {
	Classic, HCP config.SplunkIndexes
}

const (
	ClusterIDLabel   string = "api.openshift.com/id"
	ClusterTypeLabel string = "ext-hypershift.openshift.io/cluster-type"
	TokenObjectName  string = "cluster"
)

// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=syncsets,verbs=list;watch;create
// +kubebuilder:rbac:groups=hive.openshift.io,resources=syncsets,resourceNames=splunk-hec-token,verbs=get;update;patch;delete

// Reconcile ensures that ClusterDeployments have a corresponding SplunkToken.
func (r *ClusterDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("namespace", req.Namespace)

	clusterdeployment := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, req.NamespacedName, clusterdeployment); errors.IsNotFound(err) {
		log.Info("clusterdeployment has been deleted, ending reconciliation")
		return ctrl.Result{}, nil
	} else if err != nil {
		log.Error(err, "error retrieving ClustedDeployment")
		return ctrl.Result{}, err
	}

	tokenName, ok := clusterdeployment.Labels[ClusterIDLabel]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("label %s not found on ClusterDeployment", ClusterIDLabel)
	}

	var defaultIndex string
	var allowedIndexes []string
	clusterType := clusterdeployment.Labels[ClusterTypeLabel]
	if clusterType == "management-cluster" {
		log.Info("setting log indexes for management cluster")
		defaultIndex = r.Config.HCP.DefaultIndex
		allowedIndexes = r.Config.HCP.AllowedIndexes
	} else {
		log.Info("setting log indexes for classic cluster")
		defaultIndex = r.Config.Classic.DefaultIndex
		allowedIndexes = r.Config.Classic.AllowedIndexes
	}

	splunktoken := &stv1alpha1.SplunkToken{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      TokenObjectName,
		},
	}
	tokenExists := true
	if err := r.Get(ctx, client.ObjectKeyFromObject(splunktoken), splunktoken); client.IgnoreNotFound(err) != nil {
		log.Error(err, "error retrieving SplunkToken")
		return ctrl.Result{}, err
	} else if errors.IsNotFound(err) {
		log.Info("token does not exist, creating new token")
		tokenExists = false
	}

	// don't update the SplunkToken object if the indexes are the same
	if defaultIndex == splunktoken.Spec.DefaultIndex && reflect.DeepEqual(allowedIndexes, splunktoken.Spec.AllowedIndexes) {
		log.Info("token spec is unchanged, ending reconciliation")
		return ctrl.Result{}, nil
	}

	splunktoken.Spec = stv1alpha1.SplunkTokenSpec{
		Name:           tokenName,
		DefaultIndex:   defaultIndex,
		AllowedIndexes: allowedIndexes,
	}
	if err := controllerutil.SetControllerReference(clusterdeployment, splunktoken, r.Scheme); err != nil {
		log.Error(err, "error setting owner reference")
		return ctrl.Result{}, err
	}

	if tokenExists {
		if err := r.Update(ctx, splunktoken); err != nil {
			log.Error(err, "error when updating SplunkToken")
			return ctrl.Result{}, err
		}
	} else {
		if err := r.Create(ctx, splunktoken); err != nil {
			log.Error(err, "error creating SplunkToken")
			return ctrl.Result{}, err
		}
	}

	// create SyncSet for Secret
	tokenSyncSet := &hivev1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      config.OwnedSecretName,
		},
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(tokenSyncSet), tokenSyncSet); errors.IsNotFound(err) {
		log.Info("creating SyncSet for HEC token secret")
		r.createSyncSet(req.Name, tokenSyncSet)
		if err := controllerutil.SetControllerReference(clusterdeployment, tokenSyncSet, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, tokenSyncSet); err != nil {
			log.Error(err, "error creating SyncSet")
		}
	} else if err != nil {
		log.Error(err, "error fetching SyncSet")
		return ctrl.Result{}, err
	} else {
		log.Info("secret SyncSet already exists")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Named("clusterdeployment").
		Owns(&stv1alpha1.SplunkToken{}).
		Complete(r)
}

func (r *ClusterDeploymentReconciler) createSyncSet(clusterName string, syncset *hivev1.SyncSet) {
	syncset.Spec.ClusterDeploymentRefs = []corev1.LocalObjectReference{
		{
			Name: clusterName,
		},
	}
}
