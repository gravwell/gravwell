# Mimecast Ingester
This ingester polls the MTA (Mail Transfer Agent) SIEM and Audit APIs from [Mimecast](https://www.mimecast.com/).
Intended to help customers get data in faster with less pain points. 
All efforts are taken to preserver timestamps from the Events so they are as accurate as possible, even if we skip polling intervals or are down. 

## Mimecast Docs
- [Api Overview](https://developer.services.mimecast.com/api-overview)
- [SIEM Tutorial](https://developer.services.mimecast.com/siem-tutorial-cg)
- [Mimecast Side Setup (how to get a token)](https://mimecastsupport.zendesk.com/hc/en-us/articles/34000360548755-API-Integrations-Managing-API-2-0-for-Cloud-Gateway#h_01KFBA474MS5X46Z6H5XRNKPJR)
- [Audit API Endpoints](https://developer.services.mimecast.com/docs/auditevents/1/routes/api/audit/get-audit-events/post)
- [SIEM Audit API Endpoints](https://developer.services.mimecast.com/docs/threatssecurityeventsanddataforcg/1/routes/siem/v1/events/cg/get)
- [SIEM Batch API Endpoints](https://developer.services.mimecast.com/docs/threatssecurityeventsanddataforcg/1/routes/siem/v1/batch/events/cg/get)