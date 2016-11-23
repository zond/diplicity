#!/usr/bin/env bash

SDK_URL=https://storage.googleapis.com/golang/go1.7.3.linux-amd64.tar.gz

function download_sdk {
  echo ">>> Removing old installation"
  rm -r $HOME/golang
  echo ">>> Downloading $SDK_URL"
  curl -fo $HOME/golang.tar.gz $SDK_URL || exit 1
  echo ">>> Unpacking $SDK_URL"
  mkdir -p $HOME/golang
  tar -C $HOME/golang -xf $HOME/golang.tar.gz --strip-components=1
  echo ">>> Cleaning up download"
  rm $HOME/golang.tar.gz
}

if [ ! -d ~/golang ]; then
  download_sdk
fi
