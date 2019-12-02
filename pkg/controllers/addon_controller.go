/*

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

package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	discoveryv1alpha1 "github.com/njhale/addon-discovery/pkg/api/v1alpha1"
)

var (
	localSchemeBuilder = runtime.NewSchemeBuilder(
		kscheme.AddToScheme,
		apiextensionsv1beta1.AddToScheme,
		apiregistrationv1.AddToScheme,
		discoveryv1alpha1.AddToScheme,
	)
	AddToScheme = localSchemeBuilder.AddToScheme
)

// SetManager adds the addon controller's to the given manager.
func SetManager(mgr ctrl.Manager, log logr.Logger) error {
	if err := AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	rec := newAddonReconciler(mgr.GetClient(), log.WithName("addon-controller"))
	enqueueAddon := &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(rec.mapComponentRequests),
	}

	// Note: If we want to support resources composed of custom resources, we need to figure out how
	// to dynamically add resource types to watch.
	return ctrl.NewControllerManagedBy(mgr).
		For(&discoveryv1alpha1.Addon{}).
		Watches(&source.Kind{Type: &appsv1.Deployment{}}, enqueueAddon).
		Watches(&source.Kind{Type: &corev1.Namespace{}}, enqueueAddon).
		Watches(&source.Kind{Type: &corev1.ServiceAccount{}}, enqueueAddon).
		Watches(&source.Kind{Type: &corev1.Secret{}}, enqueueAddon).
		Watches(&source.Kind{Type: &rbacv1.Role{}}, enqueueAddon).
		Watches(&source.Kind{Type: &rbacv1.RoleBinding{}}, enqueueAddon).
		Watches(&source.Kind{Type: &rbacv1.ClusterRole{}}, enqueueAddon).
		Watches(&source.Kind{Type: &rbacv1.ClusterRoleBinding{}}, enqueueAddon).
		Watches(&source.Kind{Type: &apiextensionsv1beta1.CustomResourceDefinition{}}, enqueueAddon).
		Watches(&source.Kind{Type: &apiregistrationv1.APIService{}}, enqueueAddon).
		Complete(rec)
}

// +kubebuilder:rbac:groups=discovery.addons.k8s.io,resources=addons,verbs=create;update;patch;delete
// +kubebuilder:rbac:groups=discovery.addons.k8s.io,resources=addons/status,verbs=update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch

type addonReconciler struct {
	client.Client

	log logr.Logger
	mux sync.RWMutex
	// addons contains the names of Addons the addonReconciler has observed exist.
	addons map[types.NamespacedName]struct{}
}

func newAddonReconciler(cli client.Client, log logr.Logger) *addonReconciler {
	return &addonReconciler{
		Client: cli,

		log:    log,
		addons: map[types.NamespacedName]struct{}{},
	}
}

// Implement reconcile.Reconciler so the controller can reconcile objects
var _ reconcile.Reconciler = &addonReconciler{}

func (r *addonReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	// Set up a convenient log object so we don't have to type request over and over again
	log := r.log.WithValues("request", req)
	log.V(1).Info("reconciling addon")

	// Fetch the Addon from the cache
	ctx := context.TODO()
	in := &discoveryv1alpha1.Addon{}
	if err := r.Get(ctx, req.NamespacedName, in); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Could not find Addon")
			r.unobserve(req.NamespacedName)
		} else {
			log.Error(err, "Error finding Addon")
		}

		return reconcile.Result{}, nil
	}
	r.observe(req.NamespacedName)

	// Wrap with convenience decorator
	addon, err := NewAddon(in)
	if err != nil {
		log.Error(err, "Could not wrap Addon with convenience decorator")
		return reconcile.Result{}, nil
	}

	if err = r.updateComponents(ctx, addon); err != nil {
		log.Error(err, "Could not update components")
		return reconcile.Result{}, nil

	}

	if err := r.Update(ctx, addon.Addon); err != nil {
		log.Error(err, "Could not update Addon status")
		return ctrl.Result{}, err
	}

	if err := r.Get(ctx, req.NamespacedName, addon.Addon); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *addonReconciler) updateComponents(ctx context.Context, addon *Addon) error {
	selector, err := addon.ComponentSelector()
	if err != nil {
		return err
	}

	components, err := r.listComponents(ctx, selector)
	if err != nil {
		return err
	}

	return addon.SetComponents(components...)
}

func (r *addonReconciler) listComponents(ctx context.Context, selector labels.Selector) ([]runtime.Object, error) {
	// Note: We need to figure out how to dynamically add new list types here (or some equivalent) in
	// order to support addons composed of custom resources.
	opt := client.MatchingLabelsSelector{Selector: selector}
	componentLists := []runtime.Object{
		&appsv1.DeploymentList{},
		&corev1.NamespaceList{},
		&corev1.ServiceAccountList{},
		&corev1.SecretList{},
		&rbacv1.RoleList{},
		&rbacv1.RoleBindingList{},
		&rbacv1.ClusterRoleList{},
		&rbacv1.ClusterRoleBindingList{},
		&apiextensionsv1beta1.CustomResourceDefinitionList{},
		&apiregistrationv1.APIServiceList{},
	}

	for _, list := range componentLists {
		if err := r.List(ctx, list, opt); err != nil {
			return nil, err
		}
	}

	return componentLists, nil
}

func (r *addonReconciler) observed(name types.NamespacedName) bool {
	r.mux.RLock()
	defer r.mux.RUnlock()
	_, ok := r.addons[name]
	return ok
}

func (r *addonReconciler) observe(name types.NamespacedName) {
	r.mux.Lock()
	defer r.mux.Unlock()
	r.addons[name] = struct{}{}
}

func (r *addonReconciler) unobserve(name types.NamespacedName) {
	r.mux.Lock()
	defer r.mux.Unlock()
	delete(r.addons, name)
}

func (r *addonReconciler) mapComponentRequests(obj handler.MapObject) (requests []reconcile.Request) {
	if obj.Meta == nil {
		return
	}

	for _, name := range AddonNames(obj.Meta.GetLabels()) {
		// Only enqueue if we can find the addon in our cache
		if !r.observed(name) {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: name})
	}

	return
}
