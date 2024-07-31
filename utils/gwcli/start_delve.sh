#!/bin/bash

~/go/bin/dlv debug --headless --api-version=2 --listen=127.0.0.1:43000 . -- -u admin -p changeme --insecure
