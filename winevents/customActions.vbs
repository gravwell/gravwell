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