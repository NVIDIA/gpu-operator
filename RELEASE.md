# Artifacts

This repository outputs two artifacts:
- The GPU Operator container.
- The GPU Operator helm chart.

# Versioning

This repository follows Semantic Versioning 2.0.0
The artifacts will be versioned as follows:
- **nightly**: 1.0.0-nightly-shortSHA
    - The version names contain "nightly".
    - Leading number of pre-release version tracked in main.
    - build meta data of SHA hash is appended to version string.
    - May be buggy
    - Features may be removed at any time.
    - The API may change in incompatible ways in a later software release without notice.
    - Recommended for use in short-lived clusters
    - when Docker supports it, we'll use +shortSHA in SemVer 2.0 fashion
- **alpha**: 1.0.0-alpha.N
    - The version names contain "alpha".
    - May be buggy, enabling features may expose bugs.
    - Features may be removed at any time.
    - The API may change in incompatible ways in a later software release without notice.
    - Recommended for use in short-lived clusters and tech previews
- **beta**: 1.0.0-rc.N
    - The version names contain "rc".
    - Code is well tested. Using the feature is considered safe.
    - Features will not be dropped.
    - The API may change in incompatible ways but when this happens we will provided instructions for migrating to the next version.
    - Recommended for only non-business-critical uses.
- **stable**: 1.X.Y
    - The version follows [SemVer 2.0.0](http://semver.org/)
    - Stable versions of features will appear in released software for many subsequent versions.

*Note: Some of the items were copied from Kubernetes' own API versioning policy: [https://kubernetes.io/docs/concepts/overview/kubernetes-api/](https://kubernetes.io/docs/concepts/overview/kubernetes-api/)*

**The GPU Operator helm chart MUST be the same as the GPU Operator container.**

# Nightly Release Process

After every commit that successfully passes all tests, the following actions are performed:
- The GPU Operator container is persisted on the dockerhub registry (e.g: 1.X.Y-nightly-shortSHA)
- The GPU Operator helm chart is pushed on the repository's github pages (e.g: 1.X.Y-nightly-shortSHA)

# Release Process

After a commit that successfully passes all tests, a maintainer tags that commit with the release version (e.g: `1.0.0-alpha.1`):
- The GPU Operator container is persisted on the dockerhub and NGC registry
  - The tag for that container is the commit tag
- The GPU Operator helm chart is pushed on the repository's github pages and NGC registry
  - The tag for that container is the commit tag
- The Readme should be updated with the changelog
- The helm chart values.yaml and Chart.yaml should be updated with the newer version
