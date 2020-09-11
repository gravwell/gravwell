set VERSION=4.0.2
"%wix%"\bin\candle -arch x64 LicenseAgreementDlg_HK.wxs ConfigurationDlg.wxs WixUI_HK.wxs product.wxs -dVersion=%VERSION%
"%wix%"\bin\light -cultures:en-us -loc en-us.wxl -ext WixUIExtension -ext WixUtilExtension -sacl -spdb  -out gravwell_win_events_%VERSION%.msi LicenseAgreementDlg_HK.wixobj WixUI_HK.wixobj product.wixobj ConfigurationDlg.wixobj
