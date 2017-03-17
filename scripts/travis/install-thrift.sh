#!/bin/bash

set -euo pipefail

TMP="$(mktemp -d)"
trap "rm -rf ${TMP}" EXIT
cd "${TMP}"

THRIFT_VERSION=0.10.0

wget "http://www-us.apache.org/dist/thrift/${THRIFT_VERSION}/thrift-${THRIFT_VERSION}.tar.gz"
tar xfz "thrift-${THRIFT_VERSION}.tar.gz"
cd "thrift-${THRIFT_VERSION}"
./configure \
  --without-c_glib \
  --without-csharp \
  --without-cpp \
  --without-d \
  --without-erlang \
  --without-gnu-ld \
  --without-haskell \
  --without-java \
  --without-lua \
  --without-nodejs \
  --without-perl \
  --without-php \
  --without-php_extension \
  --without-python \
  --without-ruby \
  --without-qt4
sudo make install
