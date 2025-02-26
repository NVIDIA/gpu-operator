SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

kubectl patch clusterpolicy/cluster-policy --type='json' -p='[{"op": "replace", "path": "/spec/cdi/enabled", "value": true}]'
if [ "$?" -ne 0 ]; then
  echo "failed to enable CDI in clusterpolicy"
  exit 1
fi
sleep 5
