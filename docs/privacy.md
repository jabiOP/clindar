# clindar privacy policy

Effective 25 November 2026.

clindar is a terminal client for Google Calendar. It runs entirely on your own
computer. There is no clindar server, and the developer never receives, sees or
stores any of your data.

## What clindar accesses

When you log in, you grant clindar the Google Calendar scope
(`https://www.googleapis.com/auth/calendar`). clindar uses it only to do what
you ask in the app: list your calendars, read events, and create, edit,
delete and respond to events.

## Where your data goes

- **Between your computer and Google only.** clindar talks directly to
  Google's Calendar and OAuth servers. It sends nothing to the developer or
  to any third party, and it has no analytics, telemetry, ads or tracking.
- **Local cache.** Calendars and events are cached in a database on your
  computer (`~/.local/share/clindar/clindar.db` by default), so the app is
  fast and works offline.
- **Login token.** Your OAuth token is stored in your system keychain (macOS
  Keychain, or the Secret Service on Linux). If no keychain is available,
  it is kept in a file readable only by you
  (`~/.local/share/clindar/token.json`).

## Sharing

clindar does not sell, share or transfer your data to anyone. Its use of
information received from Google APIs adheres to the
[Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy),
including the Limited Use requirements.

## Deleting your data

- `clindar auth logout` deletes the stored token and the local cache.
- You can also revoke clindar's access at any time at
  <https://myaccount.google.com/permissions>.

## Changes

If this policy changes, the new version will be posted at this address with a
new effective date.

## Contact

Questions: open an issue at <https://github.com/jabiOP/clindar/issues>
