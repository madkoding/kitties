# Google accounts

The **Google Workspace** connection signs a Google account in with OAuth (authorization code with
PKCE): one click in **Settings → Connections**, a browser window to pick the account and allow it,
and the refresh token goes to the gateway's settings. It works with personal `@gmail.com`
accounts as well as Workspace ones. In Spanish: [GOOGLE.es.md](GOOGLE.es.md).

A release without a Google app built in asks for an app of yours: a free OAuth client in a Google
Cloud project of yours, made once, in about five minutes.

## 1. Make the OAuth client (once)

1. Open [console.cloud.google.com](https://console.cloud.google.com/) with your Google account and
   create a project (top bar → project picker → **New project**, for example "Kitties").
2. **APIs & Services → Library**: enable the APIs of the services you will tick:
   Gmail API, Google Calendar API, Google Drive API, Google Sheets API, Google Docs API,
   People API (contacts) and Google Tasks API.
3. **Google Auth Platform → Branding** (formerly *OAuth consent screen*): app name ("Kitties"),
   your email as support and developer contact. Save.
4. **Audience**: user type **External**. While the app is in **Testing**, add every account you
   will sign in with under **Test users** (your personal Gmail accounts). See
   [Testing or production](#testing-or-production) below.
5. **Clients → Create client**: application type **Desktop app**, any name. Copy the **Client ID**
   and the **Client secret**.

A Desktop app client takes any `http://127.0.0.1:<port>` address back, so there is no redirect
address to register: Kitties sends `http://127.0.0.1:<gateway port>/v1/oauth/callback`.

## 2. Sign in from Kitties

1. **Settings → Connections → Add → Google Workspace**.
2. **Services**: untick what the kitties should not reach. Kitties asks Google only for those
   scopes (the table below).
3. Paste the **Client ID** and **Client secret** of step 1 (the secret of a Desktop client is not
   really secret, but Google asks for it).
4. **Sign in with Google**: the browser opens Google's account chooser. Pick the account, allow
   it, and the page says *Connected*. The connection shows the account's email.

### More than one account

Each connection is one account. For another one, add another **Google Workspace** connection
(name it, for example, `google-work`) and sign in again: the chooser lets you pick the other
account. Each kitty uses the connections you tick for it under *Which kitties may use it*.
Signing in again on a connection suggests its own account (`login_hint`).

### One app for every connection

Instead of pasting the client in each connection, set it once on the gateway:
`KITTIES_GOOGLE_CLIENT_ID` and `KITTIES_GOOGLE_CLIENT_SECRET`. A build can carry it too:
`make OAUTH_APPS="google=<client id>:<client secret>" dist` (the release reads the
`KITTIES_OAUTH_APPS` repository secret).

### Gemini with the same app

With that gateway-wide app, a Gemini model signs in with your Google account too (Quick start or
**Settings → Models → Google Gemini → Google account → Sign in with Google**), with no gcloud: type
the Cloud project Gemini is billed to and sign in. Enable the **Generative Language API** in that
project. It asks for the scopes gcloud asks for (`cloud-platform`, `generative-language.retriever`),
which Google treats as sensitive: keep the app in Testing with your accounts as test users, or have
it verified. A browser on another machine lands on a `127.0.0.1` page that does not load: copy its
address back into Kitties, as for the connections. Without the app, *Use my gcloud login* still
takes `gcloud auth application-default login`.

## Scopes

Kitties always asks `openid email` (to show which account signed in) and, for each service ticked,
the narrowest scope its actions need:

| Service | Scope | What it allows |
| --- | --- | --- |
| Gmail | `gmail.readonly`, `gmail.send` | read and search mail, send mail (no deleting) |
| Calendar | `calendar.events` | list and add events |
| Drive | `drive.file` | only the files Kitties creates |
| Sheets | `spreadsheets` | append rows to a sheet you name |
| Docs | `documents` | create documents |
| Contacts | `contacts` | find and add contacts |
| Tasks | `tasks` | list, add and complete tasks |

Google's consent page lets you untick a permission; the connection then keeps only the services
that were allowed.

## Testing or production

- **Testing**: only the test users can sign in, and Google ends their sign-ins after **7 days**.
  Kitties then says *the sign-in expired or was revoked* and the connection asks you to sign in
  again.
- **In production** (Audience → **Publish app**): no 7-day limit. Without Google's verification
  the consent page warns *Google hasn't verified this app*; for your own app and account choose
  **Advanced → Go to Kitties (unsafe)**. Unverified apps are limited to 100 users, which is
  plenty for personal use. Verification (and, for Gmail reading, Google's security assessment) is
  only needed to offer the app to other people.

## A gateway on another machine

Google only sends the browser back to `127.0.0.1` (Desktop clients) or to an `https` address
(Web clients). Kitties refuses, before opening Google, a sign-in that would come back to a LAN
address or a plain-`http` name such as `http://192.168.1.20:7488`. Either:

- sign in from the computer that runs the gateway (the desktop app does this), or
- give the gateway a public `https` address (`KITTIES_PUBLIC_URL`, see [DEPLOY.md](DEPLOY.md)),
  create a **Web application** client instead, and add
  `https://<your address>/v1/oauth/callback` under *Authorized redirect URIs*.

## When it fails

| Google says | Fix |
| --- | --- |
| `access_denied` / *has not completed the Google verification process* | the app is in Testing and the account is not a test user: add it (step 1.4) |
| `redirect_uri_mismatch` | a Web client without this gateway's callback: add the address Settings shows, or use a Desktop client |
| `invalid_client` | the client id or secret is wrong, or the client was deleted |
| 403 *… API has not been used in project …* | enable that API in the project (step 1.2) |
| *the sign-in expired or was revoked* | Testing's 7 days passed, or access was removed at myaccount.google.com/permissions: sign in again |
