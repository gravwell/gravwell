# Hosted Ingester
Hosted Ingesters, sometimes called Fetchers, exist to solve a problem of ingesting non-streamed data.
Primarily this is audit logs from various services (e.g. Okta) that don't offer webhooks. 
The code here includes the individual plugins, the interfaces they must satisfy, the runtime provided to them, and a simple runner to run them as a "normal" ingester.
