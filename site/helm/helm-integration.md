---
title: Helm Integration with Flux
menu_order: 10
---

# Background

The purpose of Flux is to automate deployment of applications. User provides a git
repo with deployment configuration for his services and Flux ensures the cluster
state corresponds to the single source of truth - the repo content. Flux was built
to work with pure Kubernetes manifests.
As Helm has gained prominence among Kubernetes tools, Weave Deploy functionality,
facilitated by Flux, has been extended to cater for deployments described though Helm
Charts.

# Design overview

Chart release information is described through Kubernetes Custom Resource (CR) manifests.

Flux-Helm Integration implementation consists of two parts:

1. *Flux agent* monitors user git repo containing deployment configuration for applications/services. On detecting changes it applies the manifests.

2. *Helm operator* deals with Helm Chart releases. The operator watches for changes of Custom Resources of kind FluxHelmResource. It receives Kubernetes Events and acts accordingly, installing, upgrading or deleting a release.

# More detail

 - Kubernetes Custom Resource (CR) manifests contain all the information needed to do a Chart release. There is 1-2-1 releationship between a Helm Chart and a Custom Resource.
 
 - Flux works, at the moment, with one git repo. For Helm integration this repo will initially contain both the desired Chart release information and Chart directories for each application/service. 

 - All Chart directories are located under one git path. All Charts release configuration is located under one git path. The git paths cannot be the repo root.

 - Example of Custom Resource manifest:
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
  - name of the resource must be unique across all namespaces
  - namespace is where both the Custom Resource and the Chart, whose deployment state it describes, will live
  - labels.chart must be provided. the label contains this Chart's path within the repo (slash replaced with underscore)
  - chartgitpath ... this Chart's path within the repo
  - releasename is optional. Must be provided if there is already a Chart release in the cluster that Flux should start looking after. Otherwise a new release is created for the application/service when the Custom Resource is created. Can be provided for a brand new release - if it is not, then Flux will create a release names as $namespace-$CR_name
  - customizations section contains user customizations overriding the Chart values

 - Helm operator uses (Kubernetes) shared informer caching and a work queue, that is processed by a configurable number of workers.
