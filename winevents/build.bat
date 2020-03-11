set VERSION=3.2.8
candle -arch x64 LicenseAgreementDlg_HK.wxs ConfigurationDlg.wxs WixUI_HK.wxs product.wxs -dVersion=%VERSION%
light -cultures:en-us -loc en-us.wxl -ext WixUIExtension -ext WixUtilExtension -sacl -spdb  -out gravwell_win_events_%VERSION%.msi LicenseAgreementDlg_HK.wixobj WixUI_HK.wixobj product.wixobj ConfigurationDlg.wixobj
