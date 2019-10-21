# Artifacts

This repository outputs two artifacts:
- The GPU Operator container.
- The GPU Operator helm chart.

# Versioning

This repository follows Semantic Versioning 2.0.0
Before a 1.0.0 release the artifacts will be versioned as follows:
- **alpha**: 1.0.0+techpreview.N
    - The version names contain "techpreview".
    - May be buggy, enabling features may expose bugs.
    - Features may be removed at any time.
    - The API may change in incompatible ways in a later software release without notice.
    - Recommended for use in short-lived clusters
- **beta**: 1.0.0+rc.N
    - The version names contain "rc".
    - Code is well tested. Using the feature is considered safe.
    - Features will not be dropped.
    - The API may change in incompatible ways but when this happens we will provided instructions for migrating to the next version.
    - Recommended for only non-business-critical uses.
- **stable**: 1.X.Y
    - The version follows SEMVER
    - Stable versions of features will appear in released software for many subsequent versions.

*Note: Some of the items were copied from Kubernetes' own API versioning policy: [https://kubernetes.io/docs/concepts/overview/kubernetes-api/](https://kubernetes.io/docs/concepts/overview/kubernetes-api/)*

**The GPU Operator helm chart MUST be the same as the GPU Operator container.**

# Staging Release Process

After every commit that successfully passes all tests, the following actions are performed:
- The GPU Operator container is persisted on the gitlab registry
  - The tag for that container is the commit sha
- The GPU Operator helm chart is pushed on the repositorie's github pages
  - The tag for that helm chart is the commit sha

# Release Process

After a commit that successfully passes all tests, a maintainer tags that commit with the release version (e.g: `1.0.0+techpreview.1`):
- The GPU Operator container is persisted on the dockerhub and NGC registry
  - The tag for that container is the commit tag
- The GPU Operator helm chart is pushed on the repositorie's github pages and NGC registry
  - The tag for that container is the commit tag
