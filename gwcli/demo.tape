# Charm VHS script to generate demo.gif


Output demo.gif

Set Shell "bash"
Set FontSize 28
Set Width 1920
Set Height 1080
Set Margin 5
Set MarginFill "#9c7af7"
Set WindowBar ColorfulRight
Set BorderRadius 20
Set Framerate 60

Type "./gwcli -u admin --insecure" Sleep 500ms  Enter
Tab 1
Type "changeme" Enter
Type "help" Sleep 500ms Enter
Sleep 2s
Type "help macros" Sleep 500ms Enter
Sleep 2s
Type "macros" Sleep 500ms Enter
Sleep 1s
Type "tree" Sleep 500ms Enter
Sleep 1s
Type "li" Sleep 800ms Tab 1 Sleep 500ms Enter
Sleep 2s
Up 1
Type " --json" Sleep 800ms Enter
Sleep 2s
Type ".." Sleep 500ms Enter
Type "query tag=gravwell" Sleep 500ms Enter
Sleep 4s
Tab 1
Sleep 1100ms
Tab 1
Sleep 1100ms
Tab 1
Sleep 1100ms
Tab 1 
Sleep 2s
Down 5 Sleep 1s
Down 5 Sleep 1s
Right 2 Sleep 1s
Left 1 Sleep 1s
Sleep 2s
Ctrl+C
Ctrl+C
Sleep 1s
Type "./gwcli --insecure --no-interactive system indexers list --csv" Sleep 500ms  Enter
Sleep 6s