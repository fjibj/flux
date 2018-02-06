---
title: Requirements for Helm Integration with Flux
menu_order: 20
---

# Helm

 - tiller must be running in the cluster

# Git repo

 - One repo containing both release state information (Custom Resource manifests) and Charts themselves
 - Release state information in the form of Custom Resources manifests is located under a particular path ("releaseconfig" by default; can be overriden)
 - Charts are colocated under another path ("charts" by default; can be overriden)
 - Custom Resource namespace reflects where the release should be done
 - example of a test repo: https://github.com/tamarakaufler/helm-fhr-test

# Custom Resource manifest content
## Example of manifest content

```
 ---
  apiVersion: integrations.flux/v1
  kind: FluxHelmResource
  metadata:
    name: mongodb
    namespace: kube-system
    labels:
      chart: charts_mongodb
  spec:
    chartgitpath: charts/mongodb
    releasename: kube-system-mongodb
    customizations:
      - name: image
        value: bitnami/mongodb:3.7.1-r0
      - name: imagePullPolicy
        value: IfNotPresent
      - name: some_param
        value: 301
      - name: resources.requests.memory
        values: 256Mi
```

## Compulsory fields

 - name
 - namespace
 - label.chart  ... the same as chartgitpath, with slash replaced with  an underscore
 - chartgitpath ... path (from repo root) to a Chart subdirectory


## Optional field

 - releasename:

  - if a release already exists and Flux should start managing it, then releasename must be provided
  - if releasename is not provided, Flux will construct a release name based on the namespace and the Custom Resource name (ie $namespace-$CR_name)
