# Third-Party Notices

Eva Board redistributes the following open-source dependencies. Their
licenses are reproduced here in summary; full license texts are
available in each dependency's source distribution.

This file is hand-curated for v1; see ROADMAP for automating via
[go-licenses](https://github.com/google/go-licenses) and
[license-checker](https://github.com/davglass/license-checker).

Sources of truth: [`backend/go.mod`](backend/go.mod) and
[`mobile/package.json`](mobile/package.json).

---

## Apache License 2.0

Same license as Eva Board itself. Full text:
[apache.org/licenses/LICENSE-2.0](https://www.apache.org/licenses/LICENSE-2.0).

Mobile (dev dependency):

- [typescript](https://github.com/microsoft/TypeScript) — Microsoft

---

## MIT License

Standard permissive MIT terms. Full text:
[opensource.org/license/mit](https://opensource.org/license/mit).

Backend (direct):

- [github.com/gofiber/fiber/v2](https://github.com/gofiber/fiber) — Fiber web framework
- [github.com/jackc/pgx/v5](https://github.com/jackc/pgx) — PostgreSQL driver and toolkit
- [github.com/golang-migrate/migrate/v4](https://github.com/golang-migrate/migrate) — database migrations
- [github.com/joho/godotenv](https://github.com/joho/godotenv) — `.env` loader
- [github.com/teslashibe/magiclink-auth-go](https://github.com/teslashibe/magiclink-auth-go) — magic-link auth helper

Mobile (direct):

- [expo](https://github.com/expo/expo) and the `expo-*` family
  (`expo-constants`, `expo-font`, `expo-haptics`, `expo-linking`,
  `expo-router`, `expo-secure-store`, `expo-status-bar`,
  `expo-web-browser`)
- [@expo/metro-runtime](https://github.com/expo/expo)
- [@expo-google-fonts/inter](https://github.com/expo/google-fonts) and
  [@expo-google-fonts/space-grotesk](https://github.com/expo/google-fonts)
  — wrapper code is MIT; the font files themselves are licensed under
  the SIL Open Font License 1.1 (see below).
- [react](https://github.com/facebook/react) and
  [react-dom](https://github.com/facebook/react)
- [react-native](https://github.com/facebook/react-native)
- [react-native-gesture-handler](https://github.com/software-mansion/react-native-gesture-handler)
- [react-native-reanimated](https://github.com/software-mansion/react-native-reanimated)
- [react-native-screens](https://github.com/software-mansion/react-native-screens)
- [react-native-safe-area-context](https://github.com/th3rdwave/react-native-safe-area-context)
- [react-native-svg](https://github.com/software-mansion/react-native-svg)
- [react-native-web](https://github.com/necolas/react-native-web)
- [react-native-markdown-display](https://github.com/iamacup/react-native-markdown-display)
- [@react-navigation/native](https://github.com/react-navigation/react-navigation) and
  [@react-navigation/bottom-tabs](https://github.com/react-navigation/react-navigation)
- [@hello-pangea/dnd](https://github.com/hello-pangea/dnd) — drag-and-drop
- [nativewind](https://github.com/nativewind/nativewind)
- [tailwindcss](https://github.com/tailwindlabs/tailwindcss) (dev)
- [tailwind-merge](https://github.com/dcastil/tailwind-merge)
- [clsx](https://github.com/lukeed/clsx)
- [punycode](https://github.com/mathiasbynens/punycode.js)
- [@types/react](https://github.com/DefinitelyTyped/DefinitelyTyped) (dev,
  via DefinitelyTyped)

---

## BSD 3-Clause License

Full text:
[opensource.org/license/bsd-3-clause](https://opensource.org/license/bsd-3-clause).

Backend (transitively, via Go standard auxiliary packages):

- [github.com/google/uuid](https://github.com/google/uuid)
- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto),
  [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync),
  [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys),
  [golang.org/x/text](https://pkg.go.dev/golang.org/x/text)

---

## ISC License

Full text:
[opensource.org/license/isc-license-txt](https://opensource.org/license/isc-license-txt).

Mobile (direct):

- [lucide-react-native](https://github.com/lucide-icons/lucide) — icon set

---

## Mozilla Public License 2.0

Full text:
[mozilla.org/MPL/2.0](https://www.mozilla.org/MPL/2.0/).

Backend (transitively):

- [github.com/hashicorp/errwrap](https://github.com/hashicorp/errwrap)
- [github.com/hashicorp/go-multierror](https://github.com/hashicorp/go-multierror)

---

## SIL Open Font License 1.1

Applies to the font files redistributed via the `@expo-google-fonts/*`
packages above. Full text:
[scripts.sil.org/OFL](https://scripts.sil.org/OFL).

- [Inter](https://github.com/rsms/inter) — Rasmus Andersson
- [Space Grotesk](https://github.com/floriankarsten/space-grotesk) — Florian Karsten

---

## Notes on completeness

This list covers Eva Board's direct dependencies plus the most widely-
distributed transitive Go modules. It is not a complete bill of
materials. For an exact, machine-generated inventory, run:

```bash
# Go
cd backend && go install github.com/google/go-licenses@latest \
  && go-licenses report ./...

# JavaScript / TypeScript
cd mobile && npx license-checker --production --summary
```

Automating this generation in CI is tracked as a follow-up.
