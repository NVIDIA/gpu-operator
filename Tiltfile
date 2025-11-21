# Tiltfile for GPU Operator

# Load local configuration for registry
# Create a file named 'tilt_config.json' with content like:
# {
#   "registry": "docker.io/myuser",
#   "image_pull_secrets": ["my-registry-secret"],
#   "namespace": "gpu-operator-resources",
#   "container_runtime": "docker",
#   "registry_username": "myuser",
#   "registry_password": "mypassword"
# }
# Or pass it via command line: tilt up -- --registry=docker.io/myuser
# Define command line arguments
config.define_string('registry')
config.define_string_list('image_pull_secrets')
config.define_string('namespace')
config.define_string('container_runtime')
config.define_string('registry_username')
config.define_string('registry_password')

cfg_cli = config.parse()
cfg_json = read_json('tilt_config.json', default={})

registry = cfg_cli.get('registry', cfg_json.get('registry', ''))
image_pull_secrets = cfg_cli.get('image_pull_secrets', cfg_json.get('image_pull_secrets', []))

# Ensure image_pull_secrets is a list if it came as a string (e.g. from JSON)
if type(image_pull_secrets) == 'string':
    image_pull_secrets = [image_pull_secrets]

registry_username = cfg_cli.get('registry_username', cfg_json.get('registry_username', ''))
registry_password = cfg_cli.get('registry_password', cfg_json.get('registry_password', ''))

namespace = cfg_cli.get('namespace', cfg_json.get('namespace', 'gpu-operator'))
container_runtime = cfg_cli.get('container_runtime', cfg_json.get('container_runtime', 'docker'))

# Construct the image name based on the registry
# If a registry is provided, we use it to construct the full image name: registry/gpu-operator
# This avoids Tilt's default behavior of appending the original name to the registry.
image_name_only = 'gpu-operator'
if registry:
    # e.g. docker.io/user/gpu-operator
    # We strip trailing slash from registry just in case
    registry = registry.rstrip('/')
    image_repo = registry
    full_image = '{}/{}'.format(image_repo, image_name_only)
else:
    # Default to the upstream name if no registry is provided (local dev)
    image_repo = 'nvcr.io/nvidia'
    full_image = '{}/{}'.format(image_repo, image_name_only)

allow_k8s_contexts('kubernetes-admin@kubernetes')

# 1. Sync CRDs
# The Dockerfile expects CRDs to be present in deployments/gpu-operator/crds
# We run `make sync-crds` to ensure they are up to date.
local_resource(
    name='sync-crds',
    cmd='make sync-crds',
    deps=['config/crd/bases'],
    ignore=['deployments/gpu-operator/crds'] # Ignore the output directory to avoid loop
)

# 2. Create Namespace (Idempotent)
# We create the namespace explicitly to ensure it exists for any pre-install setup (like secrets).
local_resource(
    name='create-namespace',
    cmd='kubectl create namespace {} --dry-run=client -o yaml | kubectl apply -f -'.format(namespace),
    deps=[],
    ignore=[]
)

# 3. Create Registry Secret (Optional)
# If credentials are provided, we create the secret.
if registry and registry_username and registry_password:
    secret_name = 'registry-secret'
    # Add to image_pull_secrets if not already there
    if secret_name not in image_pull_secrets:
        image_pull_secrets.append(secret_name)
    
    # We use a shell command to create the secret idempotently
    # Note: We extract the server from the registry URL (e.g. docker.io/user -> docker.io)
    # This is a simple heuristic; for more complex cases user might need to provide server explicitly.
    registry_server = registry.split('/')[0]
    if registry_server == 'docker.io':
        registry_server = 'https://index.docker.io/v1/'
    
    create_secret_cmd = """
    kubectl create secret docker-registry {} \
        --docker-server={} \
        --docker-username={} \
        --docker-password={} \
        -n {} --dry-run=client -o yaml | kubectl apply -f -
    """.format(secret_name, registry_server, registry_username, registry_password, namespace)

    local_resource(
        name='create-secret',
        cmd=create_secret_cmd,
        deps=[],
        ignore=[]
    )

# 4. Build the GPU Operator Image
# We read the version from the VERSION file or default to 'latest'
version = 'latest'
git_commit = str(local('git rev-parse --short HEAD', quiet=True)).strip()

# Note: We do NOT call default_registry() here because we are manually constructing 
# the image name to include the registry. Tilt will automatically push if the 
# image name implies a remote registry.

# We use custom_build so we can push the image with the git_commit tag as well.
# The Operator (via ClusterPolicy) expects the validator image to be tagged with the git commit.
# Tilt manages the Deployment image (using $EXPECTED_REF), but we need to ensure the 
# validator image exists in the registry with the tag the Operator looks for.
timestamp = str(local('date +%s%3N', quiet=True)).strip()
tag = "{}-{}".format(git_commit, timestamp)
build_cmd = """
docker build \
    --platform linux/amd64 \
    --build-arg VERSION={version} \
    --build-arg GIT_COMMIT={git_commit} \
    -t {full_image}:{tag} \
    -f docker/Dockerfile \
    .
docker push {full_image}:{tag}
""".format(full_image=full_image, version=version, git_commit=git_commit, tag=tag)

custom_build(
    full_image,
    tag=tag,
    command=build_cmd,
    deps=['.'],
    ignore=['deployments/gpu-operator/templates'],
)

docker_prune_settings(
    max_age_mins=120,
    num_builds=5,
)

# 3. Deploy with Helm
extra_set_args = []
for i, secret in enumerate(image_pull_secrets):
    extra_set_args.append('operator.imagePullSecrets[{}]={}'.format(i, secret))
    extra_set_args.append('validator.imagePullSecrets[{}]={}'.format(i, secret))

extra_set_args.append('operator.defaultRuntime={}'.format(container_runtime))

# Override the image in Helm to match what we built
extra_set_args.append('operator.repository={}'.format(image_repo))
extra_set_args.append('operator.image={}'.format(image_name_only))
extra_set_args.append('validator.repository={}'.format(image_repo))
extra_set_args.append('validator.image={}'.format(image_name_only))
extra_set_args.append('validator.version={}'.format(tag))


k8s_yaml(
    helm(
        'deployments/gpu-operator',
        name='gpu-operator',
        namespace=namespace,
        values=[
            'deployments/gpu-operator/values.yaml',
        ],
        set=extra_set_args
    )
)

