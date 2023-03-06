#!/bin/bash

echo "Starting dev_appserver.py"
${GCLOUD_ROOT}/bin/dev_appserver.py --require_indexes --skip_sdk_update_check=true --clear_datastore=true --datastore_consistency_policy=consistent ${GITHUB_WORKSPACE}/app.yaml > /tmp/start_test_server.log 2>&1 &
echo "Waiting for it to start serving"
while ! curl -s -o /dev/null http://localhost:8080/; do
  sleep 10
  echo "Waiting.. ($(date))"
done
echo "done"
