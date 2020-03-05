#!/bin/bash

echo "Starting dev_appserver.py"
${GCLOUD_ROOT}/bin/dev_appserver.py --require_indexes --skip_sdk_update_check=true --clear_datastore=true --datastore_consistency_policy=consistent ${GITHUB_WORKSPACE}/app.yaml > /dev/null 2>&1 &
echo -n "Waiting for it to start serving"
while ! curl -s -o /dev/null http://localhost:8080/; do
  sleep 1
  echo -n .
done
echo "done"
