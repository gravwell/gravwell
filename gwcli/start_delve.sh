# As Bubble Tea is occupying the terminal, we cannot use delve directly.
# Instead, this spawns a delve server at localhost:43000 to connect from a different terminal/tmux/ide.
# Throw the following into a launch.json file if you want to delve from VSCodium.
# {
# "name": "ATTACH -> ./start_delve",
# "type": "go",
# "debugAdapter": "dlv-dap",
# "request": "attach",
# "mode": "remote",
# "remotePath": "${workspaceFolder}",
# "port": 43000,
#  "host": "127.0.0.1"
# },
#
#
#!/bin/bash

~/go/bin/dlv debug --headless --api-version=2 --listen=127.0.0.1:43000 . -- -u admin -p changeme --insecure
