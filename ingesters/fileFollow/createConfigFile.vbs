Dim fso, configFile
Dim params, paramsArray, CONFIGDIR
Dim CONFIG_LOG_LEVEL, CONFIG_CLEARTEXT_BACKEND_TARGET, CONFIG_INGEST_SECRET
Set fso = CreateObject("Scripting.FileSystemObject")

params = Session.Property("CustomActionData")
paramsArray = split(params, "|")
CONFIGDIR = paramsArray(0)
CONFIG_LOG_LEVEL = paramsArray(1)
CONFIG_CLEARTEXT_BACKEND_TARGET = paramsArray(2)

CONFIG_INGEST_SECRET = mid(params, len(CONFIGDIR) + len(CONFIG_LOG_LEVEL) + len(CONFIG_CLEARTEXT_BACKEND_TARGET) + 4)

Set configFile = fso.CreateTextFile(CONFIGDIR & "file_follow.cfg", True)

configFile.WriteLine ("[Global]")
configFile.WriteLine ("Ingest-Secret = " & CONFIG_INGEST_SECRET)
configFile.WriteLine ("Connection-Timeout = 0")
configFile.WriteLine ("Insecure-Skip-TLS-Verify = true")
configFile.WriteLine ("#note that backslashes (\) are an escape character and must be escaped themselves")
configFile.WriteLine ("Cleartext-Backend-Target=" & CONFIG_CLEARTEXT_BACKEND_TARGET & " #example of adding a cleartext connection")
configFile.WriteLine ("#Cleartext-Backend-Target=127.1.0.1:4023 #example of adding another cleartext connection")
configFile.WriteLine ("#Encrypted-Backend-Target=127.1.1.1:4024 #example of adding an encrypted connection")
configFile.WriteLine ("#Ingest-Cache-Path=""C:\\ProgramData\\gravwell\\filefollow\\filefollow.cache""")
configFile.WriteLine ("#Max-Ingest-Cache=1024 #Number of MB to store, localcache will only store 1GB before stopping.  This is a safety net")
configFile.WriteLine ("Log-Level=" & CONFIG_LOG_LEVEL)
configFile.WriteLine ("Max-Files-Watched=64")
configFile.WriteLine ()
configFile.WriteLine ("[Follower ""cbs""]")
configFile.WriteLine ("	Base-Directory=""C:\\Windows\\Logs\\CBS""")
configFile.WriteLine ("	File-Filter=""*.log""")
configFile.WriteLine ("	Tag-Name=auth")
configFile.WriteLine ("	Assume-Local-Timezone=true #Default for assume localtime is false")
configFile.WriteLine ("	#Ignore-Line-Prefix=""/""")
configFile.Close
