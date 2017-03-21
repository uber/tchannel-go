#!/bin/bash

set -euo pipefail

TMP="$(mktemp -d)"
# travis does not give permissions to do this and sudo rm -rf is not the best
#trap "rm -rf ${TMP}" EXIT
cd "${TMP}"

THRIFT_VERSION=0.10.0

wget "http://www-us.apache.org/dist/thrift/${THRIFT_VERSION}/thrift-${THRIFT_VERSION}.tar.gz"
tar xfz "thrift-${THRIFT_VERSION}.tar.gz"
cd "thrift-${THRIFT_VERSION}"
./configure \
  --without-c_glib \
  --without-cpp \
  --without-csharp \
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
  --without-qt4 \
  --without-ruby
sudo make install
