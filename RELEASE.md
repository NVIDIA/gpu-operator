# Release Process

```bash
# On the master branch
$ helm package deployments/gpu-operator
$ git checkout gh-pages

# Generate the index.yml
$ helm repo index . --url https://nvidia.github.io/gpu-operator

# Make the release
$ git add ./*.tgz
$ git commit -m 'Release version XXX'
```
