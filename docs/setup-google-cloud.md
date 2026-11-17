# Set up Google access

clindar talks to Google Calendar with your own Google OAuth client: a small "app" you create once in Google Cloud.
It takes about 10 minutes and costs nothing. Google's screens change often, so the steps below name each screen by
its title rather than by where its buttons are.

## 1. Create a project

1. Open <https://console.cloud.google.com/> and sign in with the Google account whose calendar you want to use.
2. In the project picker at the top, choose **New project**, name it (for example `clindar`), and create it.
   Make sure it's the selected project for the next steps.

## 2. Turn on the Calendar API

1. Go to **APIs & Services → Library**.
2. Search for **Google Calendar API**, open it, and click **Enable**.

## 3. Describe the app (Google Auth Platform)

This screen used to be called **OAuth consent screen**.

1. Go to **Google Auth Platform** (or **APIs & Services → OAuth consent screen**) and click **Get started**.
2. **App information:** an app name (`clindar`) and your email as the support email.
3. **Audience:** choose **External**.
4. **Contact information:** your email again. Accept the policy and create.
5. Under **Audience → Test users**, add **your own Google address**. While the app is in "Testing", only test users
   can log in.

About "Testing": Google may expire a test app's login after 7 days, and clindar would then ask you to
`clindar auth login` again. To avoid that, click **Publish app** under **Audience**. An unverified app used only by
you is fine: at login Google shows a "Google hasn't verified this app" warning; choose **Advanced → Go to clindar**.

## 4. Create the client

1. Go to **Google Auth Platform → Clients** and click **Create client**.
2. **Application type: Desktop app.** Any name.
3. Create it, then **Download JSON**. You get a file like `client_secret_1234-abcd.apps.googleusercontent.com.json`.

## 5. Give it to clindar and log in

```bash
clindar auth setup ~/Downloads/client_secret_*.json
```

```bash
clindar auth login
```

`auth setup` copies the file to `~/.config/clindar/client.json` (mode 600). `auth login` opens your browser, asks
Google for access to your calendars, and does a first sync. Your login token is kept in the system keyring (macOS
Keychain, GNOME Keyring, KWallet), or in a private file in clindar's data folder where there is no keyring.

Then run `clindar` for the calendar, or see [cli.md](cli.md).

## Troubleshooting

- **"Access blocked: clindar has not completed the Google verification process"**: your address isn't a test user
  (step 3.5), or you logged in with another account.
- **`redirect_uri_mismatch`**: the client isn't of type **Desktop app**. Create a new one (step 4).
- **A Google Workspace (work or school) account refuses the app**: the administrator may have to allow it, or create
  the project inside the organization and choose **Internal** in step 3.3.
- **Logged out after a week**: the app is still in "Testing"; publish it (step 3).
- **"no OAuth client configured"**: run `clindar auth setup` with the downloaded file first.
