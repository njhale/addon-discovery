package controllers

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/reference"

	discoveryv1alpha1 "github.com/njhale/addon-discovery/pkg/api/v1alpha1"
)

const (
	// ComponentLabelKeyPrefix is the key prefix used for labels marking addon component resources.
	ComponentLabelKeyPrefix = "discovery.addons.k8s.io/"

	newAddonError          = "Cannot create new Addon: %s"
	componentLabelKeyError = "Cannot generate component label key: %s"
)

var componentScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(AddToScheme(componentScheme))
}

// AddonNames returns a list of addon names extracted from the given labels.
func AddonNames(labels map[string]string) (names []types.NamespacedName) {
	for key := range labels {
		if !strings.HasPrefix(key, ComponentLabelKeyPrefix) {
			continue
		}

		names = append(names, types.NamespacedName{
			Name: strings.TrimPrefix(key, ComponentLabelKeyPrefix),
		})
	}

	return
}

// Addon decorates an external Addon and provides convenience methods for managing it.
type Addon struct {
	*discoveryv1alpha1.Addon
}

// NewAddon returns a new Addon instance.
func NewAddon(addon *discoveryv1alpha1.Addon) (*Addon, error) {
	if addon == nil {
		return nil, fmt.Errorf(newAddonError, "nil Addon argument")
	}

	o := &Addon{
		Addon: addon.DeepCopy(),
	}

	return o, nil
}

// ComponentLabelKey returns the addon's completed component label key
func (a *Addon) ComponentLabelKey() (string, error) {
	if a.GetName() == "" {
		return "", fmt.Errorf(componentLabelKeyError, "empty name field")
	}

	return ComponentLabelKeyPrefix + a.GetName(), nil
}

// ComponentLabelSelector returns a LabelSelector that matches this addon's component label.
func (a *Addon) ComponentLabelSelector() (*metav1.LabelSelector, error) {
	key, err := a.ComponentLabelKey()
	if err != nil {
		return nil, err
	}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      key,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}

	return labelSelector, nil
}

// ComponentSelector returns a Selector that matches this addon's component label.
func (a *Addon) ComponentSelector() (labels.Selector, error) {
	labelSelector, err := a.ComponentLabelSelector()
	if err != nil {
		return nil, err
	}

	return metav1.LabelSelectorAsSelector(labelSelector)
}

// ResetComponents resets the component selector and references in the addon's status.
func (a *Addon) ResetComponents() error {
	labelSelector, err := a.ComponentLabelSelector()
	if err != nil {
		return err
	}

	a.Status.Components = &discoveryv1alpha1.Components{
		LabelSelector: labelSelector,
	}

	return nil
}

// AddComponents adds the given components to the addon's status and returns an error
// if a component isn't associated with the addon by label.
// List type arguments are flattened to their nested elements before being added.
func (a *Addon) AddComponents(components ...runtime.Object) error {
	selector, err := a.ComponentSelector()
	if err != nil {
		return err
	}

	var refs []discoveryv1alpha1.Ref
	for _, component := range components {
		// Unpack nested components
		if nested, err := meta.ExtractList(component); err == nil {
			if err = a.AddComponents(nested...); err != nil {
				return err
			}

			continue
		}

		m, err := meta.Accessor(component)
		if err != nil {
			return err
		}

		t, err := meta.TypeAccessor(component)
		if err != nil {
			return err
		}

		if !selector.Matches(labels.Set(m.GetLabels())) {
			return fmt.Errorf("Cannot add component %s/%s/%s to Addon %s: component labels not selected by %s", t.GetKind(), m.GetNamespace(), m.GetName(), a.GetName(), selector.String())
		}

		ref, err := truncatedReference(component)
		if err != nil {
			return err
		}

		componentRef := discoveryv1alpha1.Ref{
			ObjectReference: ref,
		}
		refs = append(refs, componentRef)
	}

	if a.Status.Components == nil {
		if err := a.ResetComponents(); err != nil {
			return err
		}
	}

	a.Status.Components.Refs = append(a.Status.Components.Refs, refs...)

	return nil
}

// SetComponents sets the component references in the addon's status to the given components.
func (a *Addon) SetComponents(components ...runtime.Object) error {
	if err := a.ResetComponents(); err != nil {
		return err
	}

	return a.AddComponents(components...)
}

func truncatedReference(component runtime.Object) (ref *corev1.ObjectReference, err error) {
	ref, err = reference.GetReference(componentScheme, component)
	if err != nil {
		return
	}

	ref = &corev1.ObjectReference{
		Kind:       ref.Kind,
		APIVersion: ref.APIVersion,
		Namespace:  ref.Namespace,
		Name:       ref.Name,
	}

	return
}
