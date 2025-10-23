#!/bin/bash

set -euxo pipefail

make build

sudo cp home-ci /usr/local/bin/
sudo cp home-ci-e2e /usr/local/bin/
sudo cp home-ci-diag /usr/local/bin/