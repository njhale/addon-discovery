package controllers

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"

	discoveryv1alpha1 "github.com/njhale/addon-discovery/pkg/api/v1alpha1"
	"github.com/njhale/addon-discovery/pkg/lib/testobj"
)

var _ = Describe("Addon Decorator", func() {
	DescribeTable("getting addon names from labels",
		func(labels map[string]string, names []types.NamespacedName) {
			Expect(AddonNames(labels)).To(ConsistOf(names))
		},
		Entry("should handle nil labels", nil, nil),
		Entry("should handle empty labels", map[string]string{}, nil),
		Entry("should ignore non-component labels",
			map[string]string{
				"":                               "",
				"discovery.addons.k8s.io/ghost":  "ooooooooo",
				"addon/ghoul":                    "",
				"discovery.addons.k8s.io/goblin": "",
				"addon":                          "wizard",
			},
			[]types.NamespacedName{
				{Name: "ghost"},
				{Name: "goblin"},
			},
		),
	)

	Describe("component selection", func() {
		var (
			addon                          *Addon
			expectedKey                    string
			expectedComponentLabelSelector *metav1.LabelSelector
		)

		BeforeEach(func() {
			addon = newAddon("ghost")
			expectedKey = ComponentLabelKeyPrefix + addon.GetName()
			expectedComponentLabelSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      expectedKey,
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			}
		})

		Describe("selector generation", func() {
			var expectedComponentSelector labels.Selector

			BeforeEach(func() {
				expectedComponentSelector, _ = metav1.LabelSelectorAsSelector(expectedComponentLabelSelector)
			})

			It("can generate a valid component label key", func() {
				key, err := addon.ComponentLabelKey()
				Expect(err).ToNot(HaveOccurred())
				Expect(key).To(Equal(expectedKey))
			})

			It("can generate a valid component label selector", func() {
				labelSelector, err := addon.ComponentLabelSelector()
				Expect(err).ToNot(HaveOccurred())
				Expect(labelSelector).To(Equal(expectedComponentLabelSelector))
			})

			It("can generate a valid component selector", func() {
				componentSelector, err := addon.ComponentSelector()
				Expect(err).ToNot(HaveOccurred())
				Expect(componentSelector).To(Equal(expectedComponentSelector))
			})

			Specify("component label selector in the addon's status upon reset", func() {
				err := addon.ResetComponents()
				Expect(err).ToNot(HaveOccurred())
				Expect(addon.Status.Components).ToNot(BeNil())
				Expect(addon.Status.Components.LabelSelector).To(Equal(expectedComponentLabelSelector))
			})
		})

		Describe("adding components", func() {
			var (
				components []runtime.Object
				err        error
			)

			BeforeEach(func() {
				components = testobj.WithLabel(expectedKey, "",
					testobj.WithName("imp", &corev1.ServiceAccount{}),
					testobj.WithName("spectre", &rbacv1.Role{}),
					testobj.WithName("zombie", &appsv1.Deployment{}),
					testobj.WithName("boggart", &apiextensionsv1beta1.CustomResourceDefinition{}),
					testobj.WithName("dragon", &apiregistrationv1.APIService{}),
					testobj.WithName("ent", &discoveryv1alpha1.Addon{}),
				)
			})

			JustBeforeEach(func() {
				err = addon.AddComponents(components...)
			})

			Context("associated with the addon", func() {

				It("should not error", func() {
					Expect(err).ToNot(HaveOccurred())
				})

				Specify("non-nil components", func() {
					Expect(addon.Status.Components).ToNot(BeNil())
				})

				It("should be referenced in its status", func() {
					Expect(addon.Status.Components.Refs).To(ConsistOf(toRefs(scheme, components...)))
				})

				It("should retain existing references on further addition", func() {
					component := testobj.WithLabel(expectedKey, "", testobj.WithName("orc", &rbacv1.ClusterRoleBinding{}))
					err := addon.AddComponents(component...)
					Expect(err).ToNot(HaveOccurred())
					Expect(addon.Status.Components.Refs).To(ConsistOf(toRefs(scheme, append(component, components...)...)))
				})

				It("should be removed from its status upon reset", func() {
					err := addon.ResetComponents()
					Expect(err).ToNot(HaveOccurred())
					Expect(addon.Status.Components).ToNot(BeNil())
					Expect(addon.Status.Components.Refs).To(HaveLen(0))
				})

				Context("with nested list elements", func() {
					var (
						nested []runtime.Object
						list   runtime.Object
					)

					BeforeEach(func() {
						nested = testobj.WithLabel(expectedKey, "",
							testobj.WithName("nessie", &rbacv1.RoleBinding{}),
							testobj.WithName("troll", &rbacv1.RoleBinding{}),
						)
						list = testobj.WithItems(&rbacv1.RoleBindingList{}, nested...)
					})

					Specify("references for nested list elements", func() {
						err = addon.AddComponents(list)
						Expect(err).ToNot(HaveOccurred())
						Expect(addon.Status.Components.Refs).To(ConsistOf(toRefs(scheme, append(components, nested...)...)))
					})
				})

				It("should drop existing references when set", func() {
					components := testobj.WithLabel(expectedKey, "",
						testobj.WithName("imp", &corev1.ServiceAccount{}),
						testobj.WithName("spectre", &rbacv1.Role{}),
						testobj.WithName("zombie", &appsv1.Deployment{}),
					)
					err := addon.SetComponents(components...)
					Expect(err).ToNot(HaveOccurred())
					Expect(addon.Status.Components.Refs).To(ConsistOf(toRefs(scheme, components...)))
				})
			})

			Context("not associated with the addon", func() {
				var (
					expectedStatusComponents *discoveryv1alpha1.Components
				)

				BeforeEach(func() {
					expectedStatusComponents = &discoveryv1alpha1.Components{
						LabelSelector: expectedComponentLabelSelector,
						Refs:          toRefs(scheme, components...),
					}

					err = addon.AddComponents(components...)
					Expect(err).ToNot(HaveOccurred())

					// Append an unlabelled resource
					components = append(components, testobj.WithName("satyr", &corev1.Service{}))
				})

				It("should error", func() {
					Expect(err).To(HaveOccurred())
				})

				It("should still contain good component references", func() {
					Expect(addon.Status.Components).To(Equal(expectedStatusComponents))
				})

				It("should error and reset component references when set", func() {
					err := addon.SetComponents(components...)
					Expect(err).To(HaveOccurred())
					Expect(addon.Status.Components).ToNot(BeNil())

					// Should be nil after reset
					Expect(addon.Status.Components.Refs).To(BeNil())
				})
			})
		})

	})

})

func newAddon(name string) *Addon {
	return &Addon{
		Addon: &discoveryv1alpha1.Addon{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}
}

func toRefs(scheme *runtime.Scheme, objs ...runtime.Object) (refs []discoveryv1alpha1.Ref) {
	for _, ref := range testobj.GetReferences(scheme, objs...) {
		componentRef := discoveryv1alpha1.Ref{
			ObjectReference: &corev1.ObjectReference{
				Kind:       ref.Kind,
				APIVersion: ref.APIVersion,
				Namespace:  ref.Namespace,
				Name:       ref.Name,
			},
		}
		refs = append(refs, componentRef)
	}

	return
}
