candle -arch x64 LicenseAgreementDlg_HK.wxs ConfigurationDlg.wxs WixUI_HK.wxs product.wxs
light -cultures:en-us -loc en-us.wxl -ext WixUIExtension -ext WixUtilExtension -sacl -spdb  -out gravwell_win_events_3.2.8.msi LicenseAgreementDlg_HK.wixobj WixUI_HK.wixobj product.wixobj ConfigurationDlg.wixobj
