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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	discoveryv1alpha1 "github.com/njhale/addon-discovery/pkg/api/v1alpha1"
	"github.com/njhale/addon-discovery/pkg/lib/controller-runtime/source"
)

var (
	localSchemeBuilder = runtime.NewSchemeBuilder(
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
	// Options:
	// 1. Create a new source that watches discovery, dynamically adds unversioned types to a shared schema, and opens/closes
	// 	  sources on newly discovered types. This may require a threadsafe implementation of Scheme.
	return ctrl.NewControllerManagedBy(mgr).
		For(&discoveryv1alpha1.Addon{}).
		Watches(rec.source, enqueueAddon).
		Complete(rec)
}

// +kubebuilder:rbac:groups=discovery.addons.k8s.io,resources=addons,verbs=create;update;patch;delete
// +kubebuilder:rbac:groups=discovery.addons.k8s.io,resources=addons/status,verbs=update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch

type addonReconciler struct {
	client.Client

	log logr.Logger
	mu  sync.RWMutex
	// addons contains the names of Addons the addonReconciler has observed exist.
	addons map[types.NamespacedName]struct{}
	source *source.Dynamic
}

func newAddonReconciler(cli client.Client, log logr.Logger) *addonReconciler {
	return &addonReconciler{
		Client: cli,

		log:    log,
		addons: map[types.NamespacedName]struct{}{},
		source: &source.Dynamic{},
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
			// TODO(njhale): Recreate addon if we can find any components.
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
	var componentLists []runtime.Object
	for _, gvk := range r.source.Active() {
		gvk.Kind = gvk.Kind + "List"
		ul := &unstructured.UnstructuredList{}
		ul.SetGroupVersionKind(gvk)
		componentLists = append(componentLists, ul)
	}

	opt := client.MatchingLabelsSelector{Selector: selector}
	for _, list := range componentLists {
		if err := r.List(ctx, list, opt); err != nil {
			return nil, err
		}
	}

	return componentLists, nil
}

func (r *addonReconciler) observed(name types.NamespacedName) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.addons[name]
	return ok
}

func (r *addonReconciler) observe(name types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addons[name] = struct{}{}
}

func (r *addonReconciler) unobserve(name types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.addons, name)
}

func (r *addonReconciler) mapComponentRequests(obj handler.MapObject) (requests []reconcile.Request) {
	if obj.Meta == nil {
		return
	}

	for _, name := range AddonNames(obj.Meta.GetLabels()) {
		// Only enqueue if we can find the addon in our cache
		if r.observed(name) {
			requests = append(requests, reconcile.Request{NamespacedName: name})
			continue
		}

		// otherwise, best-effort generate a new addon
		// TODO(njhale): Implement verification that the addon-discovery admission webhook accepted this label (JWT or maybe sign a set of fields?)
		addon := &discoveryv1alpha1.Addon{}
		addon.SetName(name.Name)
		if err := r.Create(context.Background(), addon); err != nil && !apierrors.IsAlreadyExists(err) {
			r.log.Error(err, "couldn't generate addon", "addon", name, "component", obj.Meta.GetSelfLink())
		}
	}

	return
}
