#!/bin/bash

set -e

#cd "$(dirname "$0")"
#rm -rf thrift-release.zip
#wget https://github.com/prashantv/thrift/releases/download/p0.0.1/thrift-release.zip
#unzip thrift-release.zip

sudo apt-get update -qq
sudo apt-get install libboost-dev libboost-test-dev libboost-program-options-dev libevent-dev automake libtool flex bison pkg-config g++ libssl-dev
wget http://www.us.apache.org/dist/thrift/0.10.0/thrift-0.10.0.tar.gz
tar xfz thrift-0.10.0.tar.gz
cd thrift-0.10.0 && ./configure && sudo make install
