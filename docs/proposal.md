# Addon API Proposal

## Goals

- Define an API resource that aggregates the complete state of an addon
- Be resilient to future changes

## Non-Goals

- Define the implementation of an addon
- Describe how addons are installed
- Configuration of an addon post-install

## Proposal

Introduce a cluster-scoped API resource that represents an addon as a set of component resources selected with a unique label.

Its status will surface:

- info about the addon
- the label selector used to gather its components
- a set of status enriched references to its components
- top-level conditions that summarize any abnormal state

An example of an `Addon`:

```yaml
apiVersion: addons.k8s.io/v1alpha1
kind: Addon
metadata:
  name: plumbus
status:
  components:
    matchLabels:
        addons.k8s.io/plumbus: ""
    refs:
    - kind: CustomResourceDefinition
      name: plumbai.how.dotheydoit.com
      apiVersion: apiextensions.k8s.io/v1beta1
      conditions:
      - lastTransitionTime: "2019-11-25T12:43:26Z"
        message: no conflicts found
    	reason: NoConflicts
    	status: "True"
    	type: NamesAccepted
    - kind: Deployment
      name: plumbus-addon-controller
      apiVersion: apps/v1
      conditions:
      - lastTransitionTime: "2019-11-25T12:43:27Z"
        lastUpdateTime: "2019-11-25T12:43:39Z"
        message: ReplicaSet "plumbus-addon-controller-6999db5767" has successfully progressed.
        reason: NewReplicaSetAvailable
        status: "True"
        type: Progressing
   - kind: ClusterRoleBinding
     namespace: operators
     name: rb-9oacj
     apiVersion: rbac.authorization.k8s.io/v1
```

### Metadata Extraction (Future Work)

It might be useful to have a way for a component of an addon to register a subset of data as interesting to the top-level addon.

The example below uses an annotation that indicates how to extract the GVK from CRDs that are part of the Addon:

```yaml
apiVersion: addons.k8s.io/v1alpha1
kind: Addon
metadata:
  name: plumbus
status:
  metadata:
    apis:
    - group: how.theydoit.com
      version: v2alpha1
      kind: Plumbus
      plural: plumbai

  components:
    # ... 
---
kind: CRD
metadata:
    label:
        addons.k8s.io/plumbus: ""
    annotations:
        # jq object builder syntax?
        addons.k8s.io/metadata.apis: "[{group: spec.group, version: spec.versions[].name, kind: spec.names.kind, plural: spec.names.plural}]"
```

### Condition Probes (Future Work)

Add a resource that maps a query/response tuple to an output condition on its status. This resource can be included as a component of a `Addon` to drive its `status.conditions` field. Future iterations could support executing `Jobs` and parsing their results.
