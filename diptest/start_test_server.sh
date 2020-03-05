#!/bin/bash

dev_appserver.py --require_indexes --skip_sdk_update_check=true --clear_datastore=true --datastore_consistency_policy=consistent ${GITHUB_WORKSPACE}/app.yaml > /dev/null 2>&1 &
while ! curl -s http://localhost:8080/; do
  sleep 1
  echo -n .
done
