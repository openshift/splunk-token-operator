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
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	config splunkIndexConfig
}

type splunkIndexConfig struct {
	Classic, HCP config.SplunkIndexes
}

const (
	ClusterIDLabel   string = "api.openshift.com/id"
	ClusterTypeLabel string = "ext-hypershift.openshift.io/cluster-type"
	TokenObjectName  string = "cluster"
)

// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch

// Reconcile ensures that ClusterDeployments have a corresponding SplunkToken.
func (r *ClusterDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("namespace", req.Namespace)

	clusterdeployment := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, req.NamespacedName, clusterdeployment); err != nil {
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
		defaultIndex = r.config.HCP.DefaultIndex
		allowedIndexes = r.config.HCP.AllowedIndexes
	} else {
		log.Info("setting log indexes for classic cluster")
		defaultIndex = r.config.Classic.DefaultIndex
		allowedIndexes = r.config.Classic.AllowedIndexes
	}

	splunktoken := &stv1alpha1.SplunkToken{
		ObjectMeta: v1.ObjectMeta{
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
	if err := controllerutil.SetOwnerReference(clusterdeployment, splunktoken, r.Scheme); err != nil {
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
