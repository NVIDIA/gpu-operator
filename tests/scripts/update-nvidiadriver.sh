#!/bin/bash

if [[ "${SKIP_UPDATE}" == "true" ]]; then
    echo "Skipping update: SKIP_UPDATE=${SKIP_UPDATE}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

test_driver_image_updates() {
    # Update driver image version
    kubectl patch nvidiadriver/default --type='json' -p='[{"op": "replace", "path": "/spec/version", "value": '"$TARGET_DRIVER_VERSION"'}]'
    if [ "$?" -ne 0 ]; then
        echo "cannot update driver image with version $TARGET_DRIVER_VERSION for driver-daemonset"
        exit 1
    fi

    # Wait for 10 seconds for the change to be applied by operator
    sleep 10

    # Verify update is applied to Driver Daemonset
    UPDATED_IMAGE=$(kubectl get daemonset -l "app.kubernetes.io/component=nvidia-driver" -n "$TEST_NAMESPACE" -o json | jq '.items[0].spec.template.spec.containers[0].image')
    if [[ "$UPDATED_IMAGE" != *"$TARGET_DRIVER_VERSION"* ]]; then
        echo "Image update failed for driver daemonset to version $TARGET_DRIVER_VERSION"
        exit 1
    fi
    echo "driver daemonset image updated successfully to version $TARGET_DRIVER_VERSION"

    # Delete driver pod to trigger update due to OnDelete policy
    kubectl delete pod -l "app.kubernetes.io/component=nvidia-driver" -n "$TEST_NAMESPACE"

    # Wait for the driver upgrade to transition to "upgrade-done" state
    wait_for_driver_upgrade_done
    
    echo "ensuring that the new driver pods with version $TARGET_DRIVER_VERSION come up successfully"

    check_nvidia_driver_pods_ready

    return 0
}

test_custom_labels_override() {
  if ! kubectl patch nvidiadriver/default --type='json' -p='[{"op": "add", "path": "/spec/labels", "value": {"cloudprovider": "aws", "platform": "kubernetes"}}]';
  then
    echo "cannot update the labels of the NVIDIADriver resource"
    exit 1
  fi

  # The labels override triggers a rollout of all gpu-operator operands, so we wait for the driver upgrade to transition to "upgrade-done" state.
  wait_for_driver_upgrade_done

  check_nvidia_driver_pods_ready

  echo "checking nvidia-driver-daemonset labels"
  for pod in $(kubectl get pods -n "$TEST_NAMESPACE" -l "app.kubernetes.io/component=nvidia-driver" --output=jsonpath={.items..metadata.name})
  do
    cp_label_value=$(kubectl get pod -n "$TEST_NAMESPACE" "$pod" --output jsonpath={.metadata.labels.cloudprovider})
    if [ "$cp_label_value" != "aws" ]; then
        echo "Custom Label cloudprovider is incorrect when nvidiadriver labels are overridden - $pod"
        exit 1
    fi
    platform_label_value=$(kubectl get pod -n "$TEST_NAMESPACE" "$pod" --output jsonpath={.metadata.labels.platform})
    if [ "$platform_label_value" != "kubernetes" ]; then
        echo "Custom Label platform is incorrect when nvidiadriver labels are overridden - $pod"
        exit 1
    fi
  done
}

test_driver_image_updates
test_custom_labels_override
