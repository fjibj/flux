---
title: Requirements for Helm Integration with Flux
menu_order: 20
---

# Helm

 - tiller must be running in the cluster

# Git repo

 - One repo containing both release state information and Charts themselves

 - Release state information in the form of Custom Resources manifests is located under a particular path ("releaseconfig" by default; can be overriden)

 - Charts are colocated under another path ("charts" by default; can be overriden)

 - Custom Resource namespace reflects where the release should be done

 - example of a test repo: https://github.com/tamarakaufler/helm-fhr-test

# Custom Resource manifest content

## Compulsory


## Optional
