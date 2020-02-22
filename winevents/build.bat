candle -arch x64 LicenseAgreementDlg_HK.wxs WixUI_HK.wxs product.wxs
light -ext WixUIExtension -ext WixUtilExtension -sacl -spdb  -out gravwell_win_events_3.2.8.msi LicenseAgreementDlg_HK.wixobj WixUI_HK.wixobj product.wixobj
