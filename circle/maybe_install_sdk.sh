#!/usr/bin/env bash

API_CHECK=https://appengine.google.com/api/updatecheck
SDK_VERSION=$(curl -s $API_CHECK | awk -F '\"' '/release/ {print $2}')
# Remove the dots.
SDK_VERSION_S=${SDK_VERSION//./}

SDK_URL=https://storage.googleapis.com/appengine-sdks/
SDK_URL_A="${SDK_URL}featured/go_appengine_sdk_linux_amd64-${SDK_VERSION}.zip"
SDK_URL_B="${SDK_URL}deprecated/$SDK_VERSION_S/go_appengine_sdk_linux_amd64-${SDK_VERSION}.zip"

function download_sdk {
  echo ">>> Removing old installation"
  rm -r $HOME/go_appengine
  echo ">>> Downloading $SDK_VERSION"
  curl -fo $HOME/gae.zip $SDK_URL_A || \
      curl -fo $HOME/gae.zip $SDK_URL_B || \
      exit 1
  echo ">>> Unpacking $SDK_VERSION"
  unzip -qd $HOME $HOME/gae.zip
  echo ">>> Cleaning up download"
  rm $HOME/gae.zip
}

if [ ! -d ~/go_appengine ] || test $(find ~/go_appengine -maxdepth 0 -ctime +7); then
  download_sdk
fi
