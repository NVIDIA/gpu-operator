#!/usr/bin/env bash
# Exercise the installed toolkit mount contract without changing driver mounts.
set -Eeuo pipefail

mode=candidate
case "${1:-}" in
    ""|--mode=candidate) ;;
    --mode=baseline) mode=baseline ;;
    *) echo "Usage: TARGET_NODE=<node> $0 [--mode=candidate|--mode=baseline]" >&2; exit 2 ;;
esac
[[ $# -le 1 ]] || exit 2
: "${TARGET_NODE:?Set TARGET_NODE to a disposable Kubernetes test node}"
for command in kubectl jq cmp; do command -v "$command" >/dev/null; done
operator_namespace="${OPERATOR_NAMESPACE:-gpu-operator}"
image="${TEST_IMAGE:-busybox:1.37.0}"
timeout="${TIMEOUT:-180s}"
run="$(date -u +%Y%m%d%H%M%S)-$$"
namespace="rootfs-mount-$run"
host_root="/var/tmp/gpu-operator-rootfs-$run"
artifacts="${ARTIFACT_DIR:-./driver-rootfs-mount-artifacts}/$run"
mkdir -p "$(dirname "$artifacts")"
mkdir "$artifacts"
printf '%s\n' "$mode" >"$artifacts/mode.txt"

kubectl get node "$TARGET_NODE" -o json >"$artifacts/node.json"
kubectl version -o json >"$artifacts/kubernetes.json"
kubectl -n "$operator_namespace" get daemonset nvidia-container-toolkit-daemonset -o json >"$artifacts/daemonset.json"
jq -e '
  .spec.template.spec as $s |
  ($s.initContainers[] | select(.name == "driver-validation")) as $v |
  ([ $v.env[]? | select(.name == "DRIVER_INSTALL_DIR") | .value ][0] // "/run/nvidia/driver") as $host |
  ([ $v.env[]? | select(.name == "DRIVER_INSTALL_DIR_CTR_PATH") | .value ][0] // "/run/nvidia/driver") as $validation |
  ($s.volumes[] | select(.name == "driver-install-dir") | .hostPath.path) as $source |
  ($host | sub("/[^/]+$"; "") | if . == "" then "/" else . end) as $parent |
  (if $source == $host then "" elif $source == $parent then "/" + ($host | split("/")[-1])
   else error("driver root volume must mount the driver root or its parent") end) as $suffix |
  [ ($v, ($s.containers[] | select(.name == "nvidia-container-toolkit-ctr"))) |
    . as $c | .volumeMounts[] | select(.name == "driver-install-dir") |
    if has("subPath") or has("subPathExpr") then error("driver root must not use subPath") else . end |
    {name: $c.name, mount: ., source: $source, child: ($host | split("/")[-1]), suffix: $suffix, root: (.mountPath + $suffix)} |
    if .name == "driver-validation" and .root != $validation then error("validation root does not match its mount") else . end ] |
  if length != 2 then error("expected validation and toolkit driver root mounts") else . end
' "$artifacts/daemonset.json" >"$artifacts/contracts.json"
child="$(jq -r '.[0].child' "$artifacts/contracts.json")"
[[ "$child" != "" && "$child" != . && "$child" != .. ]]

snapshot() {
    local stage="$1" name
    mkdir "$artifacts/$stage"
    kubectl -n "$namespace" get pods -o json >"$artifacts/$stage/pods.json" 2>&1 || true
    kubectl -n "$namespace" get events -o json >"$artifacts/$stage/events.json" 2>&1 || true
    kubectl -n "$namespace" exec producer -- cat /proc/self/mountinfo >"$artifacts/$stage/producer-mountinfo.txt" 2>&1 || true
    while read -r name; do
        kubectl -n "$namespace" exec consumer -c "$name" -- cat /proc/self/mountinfo >"$artifacts/$stage/$name-mountinfo.txt" 2>&1 || true
    done < <(jq -r '.[].name' "$artifacts/contracts.json")
}
cleanup() {
    local rc=$?
    trap - EXIT
    set +e
    snapshot final
    kubectl -n "$namespace" delete pod consumer --ignore-not-found --wait --timeout="$timeout"
    if ! kubectl -n "$namespace" exec producer -- sh -ec '
        umount "$TEST_ROOT/$DRIVER_LEAF"
        rmdir "$TEST_ROOT/$DRIVER_LEAF" "$TEST_ROOT"
    '; then
        echo "Cleanup incomplete: inspect $host_root on $TARGET_NODE; artifacts: $artifacts" >&2
        rc=1
    fi
    kubectl delete namespace "$namespace" --ignore-not-found --wait --timeout="$timeout" || rc=1
    exit "$rc"
}
kubectl create namespace "$namespace"
trap cleanup EXIT
kubectl label namespace "$namespace" pod-security.kubernetes.io/enforce=privileged

# Publish A before creating either consumer, so observing A is a real mount barrier.
jq -n --arg node "$TARGET_NODE" --arg image "$image" --arg run "$run" --arg child "$child" '
  {apiVersion:"v1",kind:"Pod",metadata:{name:"producer"},spec:{nodeName:$node,restartPolicy:"Never",
   tolerations:[{operator:"Exists"}],containers:[{name:"producer",image:$image,securityContext:{privileged:true},
   env:[{name:"TEST_ROOT",value:("/test/gpu-operator-rootfs-"+$run)},{name:"RUN",value:$run},{name:"DRIVER_LEAF",value:$child}],
   command:["sh","-ec", "
     trap '\''umount \"$TEST_ROOT/$DRIVER_LEAF\" 2>/dev/null || true; exit'\'' TERM INT
     mkdir -p /generation-a /generation-b \"$TEST_ROOT/$DRIVER_LEAF\"
     printf %s \"$RUN-a\" >/generation-a/generation
     printf %s \"$RUN-b\" >/generation-b/generation
     printf same-driver-version >/generation-a/version
     printf same-driver-version >/generation-b/version
     mount --bind /generation-a \"$TEST_ROOT/$DRIVER_LEAF\"
     while :; do sleep 1 & wait $!; done"],
   readinessProbe:{exec:{command:["sh","-ec","test -f \"$TEST_ROOT/$DRIVER_LEAF/generation\""]},periodSeconds:1},
   volumeMounts:[{name:"scratch",mountPath:"/test",mountPropagation:"Bidirectional"}]}],
   volumes:[{name:"scratch",hostPath:{path:"/var/tmp",type:"Directory"}}]}}
' >"$artifacts/producer.json"
kubectl -n "$namespace" create -f "$artifacts/producer.json"
kubectl -n "$namespace" wait --for=condition=Ready pod/producer --timeout="$timeout"

# Use the installed volumeMount definitions, including readOnly and propagation.
jq -n --arg node "$TARGET_NODE" --arg image "$image" --arg root "$host_root" --slurpfile contracts "$artifacts/contracts.json" '
  {apiVersion:"v1",kind:"Pod",metadata:{name:"consumer"},spec:{nodeName:$node,restartPolicy:"Never",
   tolerations:[{operator:"Exists"}],containers:[$contracts[0][] |
     {name:.name,image:$image,securityContext:{privileged:true},command:["sh","-c","while :; do sleep 1; done"],
      volumeMounts:[(.mount + {name:"driver-root"})]}],
   volumes:[{name:"driver-root",hostPath:{path:($root + (if $contracts[0][0].suffix == "" then "/" + $contracts[0][0].child else "" end)),type:"Directory"}}]}}
' >"$artifacts/consumer.json"
kubectl -n "$namespace" create -f "$artifacts/consumer.json"
kubectl -n "$namespace" wait --for=condition=Ready pod/consumer --timeout="$timeout"
identity() {
    kubectl -n "$namespace" get pod consumer -o json |
        jq -S '{uid:.metadata.uid,containers:([.status.containerStatuses[] | {name,containerID,restartCount}] | sort_by(.name))}'
}
assert_generation() {
    local expected="$1" name root actual version
    while IFS=$'\t' read -r name root; do
        actual="$(kubectl -n "$namespace" exec consumer -c "$name" -- cat "$root/generation")"
        version="$(kubectl -n "$namespace" exec consumer -c "$name" -- cat "$root/version")"
        printf '%s generation=%s version=%s\n' "$name" "$actual" "$version" | tee -a "$artifacts/observations.txt"
        [[ "$actual" == "$expected" && "$version" == same-driver-version ]] || return 1
    done < <(jq -r '.[] | [.name,.root] | @tsv' "$artifacts/contracts.json")
}
assert_generation "$run-a"
identity >"$artifacts/identity-before.json"
snapshot bound-a

kubectl -n "$namespace" exec producer -- sh -ec 'umount "$TEST_ROOT/$DRIVER_LEAF"; mount --bind /generation-b "$TEST_ROOT/$DRIVER_LEAF"'
[[ "$(kubectl -n "$namespace" exec producer -- sh -ec 'cat "$TEST_ROOT/$DRIVER_LEAF/generation"')" == "$run-b" ]]
snapshot rotated-b
expected="$run-b"
[[ "$mode" != baseline ]] || expected="$run-a"
assert_generation "$expected"
kubectl -n "$namespace" wait --for=condition=Ready pod/consumer --timeout="$timeout"
identity >"$artifacts/identity-after.json"
cmp "$artifacts/identity-before.json" "$artifacts/identity-after.json"
jq -e 'all(.containers[]; .restartCount == 0 and .containerID != "")' "$artifacts/identity-after.json" >/dev/null
echo "PASS ($mode): both mount contracts observed $expected in unchanged containers; artifacts: $artifacts"
