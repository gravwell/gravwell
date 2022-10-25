Function QueryPurgeProgramData()
  Dim fso, Message, Style, Title
  Set fso = CreateObject("Scripting.FileSystemObject")
  Message = Session.Property("UninstallQuestion_Message")
  Style = vbYesNo + vbQuestion + vbDefaultButton2
  Title = Session.Property("UninstallQuestion_Title")

  Response = MsgBox(Message, Style, Title)
  If Response = vbYes Then
    Session.Property("PURGE") = "1"
  End If

  QueryPurgeProgramData = 1
End Function

Function Validate_CONFIG_INGEST_SECRET()
  Dim Secret, IsValid, RE, Match
  Secret = Trim(Session.Property("CONFIG_INGEST_SECRET"))
  IsValid = "0"
  Set RE = new RegExp
  RE.Pattern = "\""|\'|`"

  If Len(Secret) > 0 Then
    Match = RE.Test(Secret)
    If Not Match Then
      IsValid = "1"
    End If
  End If

  Session.Property("CONFIG_INGEST_SECRET_VALID") = IsValid
  
  Validate_CONFIG_INGEST_SECRET = 1
End Function

Function Validate_CONFIG_CLEARTEXT_BACKEND_TARGET()
  Dim CleartextBackendTarget, IsValid, REIPAddress, REFQDN, Match
  CleartextBackendTarget = Trim(Session.Property("CONFIG_CLEARTEXT_BACKEND_TARGET"))

  IsValid = "0"
  Set REIPAddress = new RegExp
  REIPAddress.Pattern = "^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])(|:([0-9]|[1-9][0-9]{0,3}))$"

  Set REFQDN = new RegExp
  REFQDN.Pattern = "(?=^.{4,253}$)(^((?!-)[a-zA-Z0-9-]{0,62}[a-zA-Z0-9]\.)+[a-zA-Z]{2,63})(|:([0-9]|[1-9][0-9]{0,3}))$"

  If Len(CleartextBackendTarget) > 0 Then
    Match = REIPAddress.Test(CleartextBackendTarget)

    If Match Then
      IsValid = "1"
    Else
      Match = REFQDN.Test(CleartextBackendTarget)

      If Match Then
        IsValid = "1"
      End If
    End If
  End If

  Session.Property("CONFIG_CLEARTEXT_BACKEND_TARGET_VALID") = IsValid
  Validate_CONFIG_CLEARTEXT_BACKEND_TARGET = 1
End Function

Function Select_WATCHED_DIRECTORY()
  Dim objFolder, objShell
  Set objShell = CreateObject("Shell.Application")
  Set objFolder = objShell.BrowseForFolder(0, "Select Folder to watch", 65, Session.Property("CONFIG_WATCHED_DIRECTORY"))
  If Not (objFolder Is Nothing) Then
    Session.Property("CONFIG_WATCHED_DIRECTORY") = objFolder.Self.path
  End If
End Function

Function Validate_CONFIG_WATCHER()
  Dim Dir, Tag, Filter, IsValid
  Dir = Trim(Session.Property("CONFIG_WATCHED_DIRECTORY"))
  Tag = Trim(Session.Property("CONFIG_TAG_NAME"))
  Filter = Trim(Session.Property("CONFIG_FILE_FILTER"))
  IsValid = "1"
  
  'TODO make this actually check the tag for banned characters
  If Len(Tag) > 0 Then  
	Session.Property("CONFIG_TAG_NAME") = Tag
  Else
    IsValid = "0"
  End If
  
  'we can only really check that it is not empty
  If Len(Filter) > 0 Then
	Session.Property("CONFIG_FILE_FILTER") = Filter
  Else
    IsValid = "0"
  End If
  
  'TODO we should check if this folder actually exists
  If Len(Dir) > 0 Then
	Session.Property("CONFIG_WATCHED_DIRECTORY") = Dir
  Else
    IsValid = "0"
  End If
 
  Session.Property("CONFIG_WATCHER_VALID") = IsValid
  Validate_CONFIG_WATCHER = 1
End Function