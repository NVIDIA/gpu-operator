#! /bin/bash

if [[ "${SKIP_UPDATE}" == "true" ]]; then
    echo "Skipping update: SKIP_UPDATE=${SKIP_UPDATE}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

# Test updates to Images used in Daemonsets in ClusterPolicy
test_image_updates() {
    # Update driver image version
    kubectl patch clusterpolicy/cluster-policy --type='json' -p='[{"op": "replace", "path": "/spec/driver/version", "value": '$TARGET_DRIVER_VERSION'}]'
    if [ "$?" -ne 0 ]; then
        echo "cannot update driver image with version $TARGET_DRIVER_VERSION for driver-daemonset"
        exit 1
    fi

    # Wait for 5 seconds for the change to be applied by operator
    sleep 10

    # Verify update is applied to Driver Daemonset
    UPDATED_IMAGE=$(kubectl get daemonset -lapp="nvidia-driver-daemonset" -n $TEST_NAMESPACE -o json | jq '.items[0].spec.template.spec.containers[0].image')
    if [[ "$UPDATED_IMAGE" != *"$TARGET_DRIVER_VERSION"* ]]; then
        echo "Image update failed for driver daemonset to version $TARGET_DRIVER_VERSION"
        exit 1
    fi
    echo "driver image updated successfully to version $TARGET_DRIVER_VERSION"

    # Verify that driver-daemonset is running successfully after update
    check_pod_ready "nvidia-driver-daemonset"

    return 0
}

# Test updates to ENV passed to Daemonsets in ClusterPolicy
test_env_updates() {
    # Update any ENV on Device Plugin
    ENV_NAME="MY_TEST_ENV_NAME"
    ENV_VALUE="test"
    kubectl patch clusterpolicy/cluster-policy --type='json' -p='[{"op": "replace", "path": "/spec/devicePlugin/env/0/name", "value": '$ENV_NAME'}]'
    if [ "$?" -ne 0 ]; then
        echo "cannot update env_name $ENV_NAME for device-plugin"
        exit 1
    fi
    kubectl patch clusterpolicy/cluster-policy --type='json' -p='[{"op": "replace", "path": "/spec/devicePlugin/env/0/value", "value": '$ENV_VALUE'}]'
    if [ "$?" -ne 0 ]; then
        echo "cannot update env value for $ENV_NAME to $ENV_VALUE for device-plugin"
        exit 1
    fi

    # Wait for env updates to be applied by operator
    sleep 10

    # Verify update is applied to Device Plugin Daemonset
    UPDATED_ENV_MAP=$(kubectl get daemonset -lapp="nvidia-device-plugin-daemonset" -n $TEST_NAMESPACE -o json | jq '.items[0].spec.template.spec.containers[0].env' | jq  --arg n "$ENV_NAME" -c '.[] | select(.name | contains($n))')
    UPDATED_ENV_NAME=$(echo $UPDATED_ENV_MAP | jq --arg n $ENV_NAME 'if .name == $n then .name else empty end' | tr -d '"')
    if [ "$UPDATED_ENV_NAME" != "$ENV_NAME" ]; then
        echo "Env update failed for device-plugin daemonset to set $ENV_NAME, got $UPDATED_ENV_NAME"
        exit 1
    fi
    echo "Env $ENV_NAME for device-plugin daemonset updated successfully"

    UPDATED_ENV_VALUE=$(echo $UPDATED_ENV_MAP | jq --arg v $ENV_VALUE 'if .value == $v then .value else empty end' | tr -d '"')
    if [ "$UPDATED_ENV_VALUE" != "$ENV_VALUE" ]; then
        echo "Env update failed for device-plugin daemonset to set $ENV_NAME:$ENV_VALUE, got $UPDATED_ENV_VALUE"
        exit 1
    fi
    echo "Env $ENV_NAME for device-plugin daemonset updated successfully to value $ENV_VALUE"

    # Verify that driver-daemonset is running successfully after update
    check_pod_ready "nvidia-device-plugin-daemonset"

    return 0
}

# Test updates to MIG Strategy in ClusterPolicy
test_mig_strategy_updates() {
    MIG_STRATEGY=mixed
    kubectl patch clusterpolicy/cluster-policy --type='json' -p='[{"op": "replace", "path": "/spec/mig/strategy", "value": '$MIG_STRATEGY'}]'
    if [ "$?" -ne 0 ]; then
        echo "cannot update MIG strategy to value $MIG_STRATEGY with ClusterPolicy"
        exit 1
    fi

    # Wait for changes to be applied to both GFD and Device-Plugin Daemonsets.
    sleep 10

    # Validate that MIG strategy value is applied to both GFD and Device-Plugin Daemonsets
    kubectl get daemonsets -lapp=gpu-feature-discovery -n $TEST_NAMESPACE  -o=jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.template.spec.containers[*].env[?(@.name=="GFD_MIG_STRATEGY")]}{"\n"}{end}' | grep GFD_MIG_STRATEGY.*$MIG_STRATEGY
    if [ "$?" -ne 0 ]; then
        echo "cannot update MIG strategy to value $MIG_STRATEGY with GFD Daemonset"
        exit 1
    fi
    kubectl get daemonsets -lapp=nvidia-device-plugin-daemonset -n $TEST_NAMESPACE  -o=jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.template.spec.containers[*].env[?(@.name=="MIG_STRATEGY")]}{"\n"}{end}' | grep MIG_STRATEGY.*$MIG_STRATEGY
    if [ "$?" -ne 0 ]; then
        echo "cannot update MIG strategy to value $MIG_STRATEGY with Device-Plugin Daemonset"
        exit 1
    fi
    echo "MIG strategy successfully updated to $MIG_STRATEGY"
    return 0
}

test_enable_dcgm() {
    kubectl patch clusterpolicy/cluster-policy --type='json' -p='[{"op": "replace", "path": "/spec/dcgm/enabled", "value": 'true'}]'
    if [ "$?" -ne 0 ]; then
        echo "cannot enable standalone DCGM engine with ClusterPolicy update"
        exit 1
    fi
    # Verify that standalone nvidia-dcgm and exporter pods are running successfully after update
    check_pod_ready "nvidia-dcgm"
    check_pod_ready "nvidia-dcgm-exporter"
}

test_image_updates
test_env_updates
test_mig_strategy_updates
test_enable_dcgm
