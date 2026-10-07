#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-gpu-operator}"
CONFIGMAP="${CONFIGMAP:-gpu-operator-certification-results}"
OUTPUT_DIR="${OUTPUT_DIR:-output}"

mkdir -p "$OUTPUT_DIR"

echo "Fetching results from ConfigMap '$CONFIGMAP' in namespace '$NAMESPACE'..."

kubectl get configmap "$CONFIGMAP" -n "$NAMESPACE" \
  -o jsonpath='{.data.results\.json}' > "$OUTPUT_DIR/results.json"

kubectl get configmap "$CONFIGMAP" -n "$NAMESPACE" \
  -o jsonpath='{.data.report\.html}' > "$OUTPUT_DIR/report.html"

echo "Results saved to:"
echo "  $OUTPUT_DIR/results.json"
echo "  $OUTPUT_DIR/report.html"
