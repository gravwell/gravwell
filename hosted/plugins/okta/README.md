# Okta Ingester

This ingester pulls from the [Okta](https://www.okta.com/) Management API and forwards each record to
Gravwell as Okta's original JSON:

- **System Log** (`/api/v1/logs`) to the `okta` tag, timestamped by the event's `published` time.
- **Users** (`/api/v1/users`) to the `okta-users` tag. Every 10 minutes it pulls the users whose
  `lastUpdated` falls in the last window.

The System Log is polled from the last ingested event, following Okta's `next` links. The cursor and
latest timestamp are saved after every page, so a restart resumes where it left off. Both
authentication methods use the same cursor, so switching between them neither drops nor repeats events.

## Authentication

There are two options, and they are mutually exclusive: configure `Token`, or `Client-ID` plus
a private key (`Private-Key` or `Private-Key-File`).

### API Token

`Token` is an SSWS API token from **Security > API > Tokens**. A token acts with the permissions
of the admin who created it, so this option needs a dedicated administrator account.

### OAuth 2.0 Service App

This option uses an API Services app with the client credentials grant and `private_key_jwt`. It is
limited to the `okta.logs.read` and `okta.users.read` scopes and the Read-only Administrator role,
so it needs no admin account. This is the preferred method for a number of reasons including:

1. Does not require a dedicated (paid) seat or binding to an actual human admin's account
2. Least privilege with narrow scopes defined in the app
3. Tokens are short-lived and re-issued automatically (API tokens last indefinitely)
4. Clearer audit trail in the Okta system log

#### Setup

In the Okta Admin Console:

1. **Applications > Applications > Create App Integration**, choose **API Services**, name it and save.
2. On the **General** tab, edit **Client Credentials** and set client authentication to
   **Public key / Private key**. Under **Add key**, choose **Generate new key**. The console shows the
   private key as a JWK only once, so copy it now. Leaving **Require Demonstrating Proof of
   Possession (DPoP) header in token requests** on is fine, the ingester handles DPoP automatically.
3. On the **Okta API Scopes** tab, grant `okta.logs.read` and `okta.users.read`. Okta still issues
   a token when a scope is not granted, it just leaves the scope out. The ingester logs a warning
   naming the missing scope, and the API returns 403 for it.
4. On the **Admin roles** tab, assign **Read-only Administrator** over the whole organization. The
   scopes alone are not enough.
5. Copy the **Client ID** from the **General** tab.

A client secret is not supported, because Okta only accepts `private_key_jwt` client authentication
for tokens with Okta API (`okta.*`) scopes.

#### Using your own key

The key pair does not have to come from Okta. It can be generated where the ingester runs, so the
private key never leaves that host and Okta is given only the public key. This is the preferred
setup where someone other than the Okta administrator runs the ingester.

1. Generate a 2048-bit RSA key that only the ingester can read:

   ```
   openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out okta-private.pem
   chmod 600 okta-private.pem
   ```

2. Build the public key as a JWK. Okta requires a `kid`, any ID that is unique within the app will
   do. `AQAB` is the public exponent 65537, the `openssl` default.

   ```
   n=$(openssl rsa -in okta-private.pem -noout -modulus | cut -d= -f2 | xxd -r -p | base64 | tr -d '=\n' | tr '+/' '-_')
   printf '{"kty":"RSA","use":"sig","alg":"RS256","kid":"gravwell-1","e":"AQAB","n":"%s"}\n' "$n"
   ```

3. In step 2 of the setup above, paste that JWK into the **Add a public key** dialog in place of
   generating a key.

4. Point the ingester at the private key and give it the same `kid`:

   ```
   Client-ID="0oa1b2c3d4e5f6g7h8i9"
   Private-Key-File="/opt/gravwell/etc/okta-private.pem"
   Private-Key-ID="gravwell-1"
   ```

To rotate the key, add the new public key to the app under a new `kid`, replace the file, update
`Private-Key-ID` and reload or restart the ingester, then remove the old key from the app. Replace
the file by renaming a new one over it: the file is read when the config is loaded, and a config
whose key file is missing or half written is rejected as a whole.

## Configuration

| Key | Required | Description |
| --- | --- | --- |
| `Domain` | yes | Okta org domain, e.g. `example.okta.com`. `*.oktapreview.com`, `*.okta-emea.com`, `*.okta-gov.com` and `*.okta.mil` also work. |
| `Token` | one of | SSWS API token. |
| `Client-ID` | one of | Service app client ID. |
| `Private-Key` | one of, with `Client-ID` | Service app private key, as the console's JWK JSON or as an unencrypted RSA PEM (PKCS#1 or PKCS#8). |
| `Private-Key-File` | one of, with `Client-ID` | Path of a file holding the private key in either of those forms. Mutually exclusive with `Private-Key`. |
| `Private-Key-ID` | no | Key ID (`kid`) sent with the client assertion. Defaults to the JWK `kid`, and Okta does not require one. With your own key, set it to the `kid` of the public JWK. |
| `Request-Batch-Size` | no | System Log events per request (default 100, max 3000). |
| `Request-Per-Minute` | no | Client-side request rate limit (default 60, max 6000). |
| `Request-Burst` | no | Request burst allowance (default 10). |
| `Ingester-UUID` | no | Ingester UUID. |

`Private-Key-File` keeps the key out of the config file. The file is read when the config is
loaded, and a reload picks up a changed key. The ingester logs a warning at startup if users other
than the file's owner can access it. Given inline, the private key can take any of these forms:

```
Private-Key=`{"kty":"RSA","kid":"...","n":"...","e":"AQAB","d":"...","p":"...","q":"...","dp":"...","dq":"...","qi":"..."}`
Private-Key="{\"kty\":\"RSA\",\"kid\":\"...\",\"n\":\"...\", ... }"
Private-Key="-----BEGIN PRIVATE KEY-----\nMIIEv...\n-----END PRIVATE KEY-----"
```

A backtick-quoted value is raw, so the JWK can be pasted as is. A PEM can also be pasted, line breaks
included, between backticks. In double quotes, escape each `"` as `\"`, and write the PEM's line breaks as `\n`.

See `example.conf` for a complete example.
