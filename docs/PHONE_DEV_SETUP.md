# Phone Dev Setup

End-to-end guide for installing Eva Board on a real iPhone or Android
device and developing against a backend running on your Mac with hot
reload.

## Prerequisites

- Mac (for running the backend + Metro)
- iPhone or Android device
- Free [Expo account](https://expo.dev/signup)
- For Path B (EAS dev build):
  - [EAS CLI](https://docs.expo.dev/eas/) — `npm install -g eas-cli`
  - Apple Developer account ($99/yr) for iOS device distribution
  - Xcode is **not** required when using EAS cloud builds
- For ngrok-based reachability: an [ngrok](https://ngrok.com) account
  (free tier is fine) and the `ngrok` CLI installed and authed

## Path A — Quick start with Expo Go (no native modules)

Fastest path to "running on my phone". Limitation: any custom native
modules (e.g. `@react-native-firebase/*`) will not load. Eva Board's
board features work fine in Expo Go today.

1. Install [Expo Go](https://expo.dev/go) on your phone (App Store or
   Play Store).
2. Start the backend on your Mac:
   ```bash
   make dev
   ```
3. Find your Mac's LAN IP (must be reachable from your phone):
   ```bash
   ipconfig getifaddr en0
   ```
4. Start Metro pointed at the backend on your LAN IP:
   ```bash
   cd mobile
   EXPO_PUBLIC_API_URL=http://<your-mac-lan-ip>:8090 npx expo start
   ```
5. Scan the QR code with Expo Go on your phone.
6. Sign in via magic link — the email link will deep-link back into
   Expo Go via the `eva-board://` scheme.

If you need to bring up native modules later, switch to Path B.

## Path B — EAS dev build (full native module support)

Builds a custom installable app ("dev client") that loads its JS
bundle from your Mac's Metro server. You get hot reload on the phone
while keeping access to any native module in your `package.json`.

### One-time setup

1. Install the EAS CLI:
   ```bash
   npm install -g eas-cli
   ```
2. Sign in:
   ```bash
   eas login
   ```
3. Initialise the project (writes `extra.eas.projectId` into
   `app.config.ts`):
   ```bash
   cd mobile
   eas init
   ```
4. **iOS only** — register your device with Apple so the dev build can
   be installed on it:
   ```bash
   eas device:create
   ```
   Open the link this prints on your iPhone and install the
   provisioning profile.
5. Trigger the dev client build:
   ```bash
   make phone-build-ios
   # or
   make phone-build-android
   ```
   Builds run on EAS cloud (~10–15 min on first run). When the build
   finishes you'll get an email with an install link — open it on the
   phone and install the app.

### Daily workflow

1. Backend up:
   ```bash
   make up        # full Docker stack
   # or
   make dev       # API on host
   ```
2. Metro on LAN:
   ```bash
   make phone-dev
   ```
3. Open the **Eva Board** dev client (the app you installed in step 5
   above) on your phone.
4. Scan the QR code shown by `make phone-dev`. The bundle loads.
5. Edit code on your Mac → changes appear on your phone within 1–2
   seconds via Fast Refresh.

## Backend reachability

Three ways to make the backend reachable from your phone. Either set
`EXPO_PUBLIC_API_URL` when building/starting Metro, **or** after
launching the app go to **Settings → Backend**, paste the URL of your
backend, and tap **Save**. The in-app override is persisted (in
`expo-secure-store` on native, `localStorage` on web) and lets you
switch between localhost / ngrok / production without rebuilding —
useful on a phone where each EAS build is a 10–15 minute round-trip.

The build-time `EXPO_PUBLIC_API_URL` remains the default; the in-app
override wins when set, and **Reset to default** clears it.

### LAN (simplest, same WiFi)

Build-time:

```bash
EXPO_PUBLIC_API_URL=http://<mac-lan-ip>:8090 make phone-dev
```

Or in the app: Settings → Backend → `http://<mac-lan-ip>:8090` → Save.

Phone must be on the same WiFi as your Mac. Watch out for "guest"
networks that block client-to-client traffic.

### ngrok tunnel (works from anywhere)

```bash
make phone-tunnel
# In another terminal (build-time):
EXPO_PUBLIC_API_URL=https://<random>.ngrok-free.app make phone-dev
```

Or just paste the ngrok URL into Settings → Backend in the running
app and tap Save — no Metro restart required.

You also want to set the backend's `APP_URL` to the ngrok URL so
magic-link emails point at a publicly reachable host.

### Deployed backend (always-on, no Mac required)

Deploy the backend to your hosting of choice
(see [SELF_HOSTING.md](SELF_HOSTING.md)) and either bake the URL in:

```bash
EXPO_PUBLIC_API_URL=https://api.example.com make phone-dev
```

…or set it from the app at Settings → Backend.

## Magic-link deep linking

The login email contains a URL like
`<APP_URL>/auth/verify?token=…`. To open it inside the installed app
instead of Safari:

- Set the backend's `APP_URL` to whichever base URL the phone can
  reach (LAN IP, ngrok URL, or deployed host).
- The backend's `/auth/verify` GET handler redirects to
  `eva-board://?token=…`.
- The installed app (Expo Go or your dev client) registers the
  `eva-board://` scheme and catches the redirect.
- `AuthSessionProvider` on the mobile side reads the token off the URL
  and stores the JWT in `expo-secure-store`.

To verify: tap the magic link on your phone — the app should open and
you should land signed-in.

## Troubleshooting

- **Phone can't reach the backend** — check macOS firewall (System
  Settings → Network → Firewall), reconfirm the LAN IP, or fall back
  to the ngrok tunnel.
- **Magic link opens Safari instead of the app** — confirm the
  `eva-board://` scheme is registered (iPhone Settings → scroll to
  Eva Board → check "Open Links"). Reinstall the app if missing.
- **Hot reload not firing** — check the Metro terminal for errors,
  shake the phone to open the dev menu and toggle Fast Refresh, or
  restart `make phone-dev`.
- **EAS: "Build failed: missing bundle ID"** — set
  `ios.bundleIdentifier` in `mobile/app.config.ts` and re-run.
- **Other EAS build failures** — open the build page from
  `eas build:list` and inspect the actual log; most failures are
  Apple-side credential/provisioning issues that EAS reports clearly.
