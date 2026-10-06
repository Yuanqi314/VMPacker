# VMPacker — Android app

A Jetpack Compose front-end for the aarch64 VMPacker engine, styled with
[miuix](https://github.com/compose-miuix-ui/miuix) (a Compose Multiplatform
implementation of Xiaomi's HyperOS / MiUIX design language).

The app lets you pick an ARM64 ELF, analyze its functions, choose which to
virtualize, and export the protected binary — all on-device. The packing engine
is the exact same Go code used by the CLI and desktop GUI, exposed to Kotlin
through [gomobile](https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile).

## Architecture

```
  Compose UI (miuix)                 Go engine (pure Go, no cgo)
  com.vmpacker.app          JNI      package vmpmobile  ->  pkg/binary/elf
  ├─ MainActivity           ◀────▶    ├─ Analyze(path) -> JSON
  ├─ PackerScreen (UI+flow)           ├─ Protect(..., Logger) -> outPath
  └─ Saf (SAF <-> files,              └─ embeds vm_interp.bin (the ARM64
     ComposeLogger)                       VM interpreter blob)
           ▲                                        │
           └── vmpmobile.aar  ◀── gomobile bind ────┘
```

- `../mobile/vmpmobile.go` is the gomobile-bindable wrapper. Running
  `gomobile bind` produces `app/libs/vmpmobile.aar`, which the app consumes.
- Structured data crosses the JNI boundary as JSON strings; engine progress is
  streamed through a Kotlin-implemented `Logger` callback.
- File access uses the Storage Access Framework, so the app needs **no**
  runtime permissions.

## Build constellation

| Component | Version |
|-----------|---------|
| miuix | `top.yukonga.miuix.kmp:miuix-ui:0.9.4` |
| Compose | JetBrains Compose Multiplatform `1.12.1` |
| Kotlin | `2.4.20` |
| AGP | `9.4.1` |
| Gradle | `9.8.0` |
| compileSdk / targetSdk | `36` |
| minSdk | `24` |

These mirror what miuix 0.9.4 is built against — the known-good path for an
Android-only consumer.

## Building

The APK is built automatically on every commit by
`.github/workflows/android.yml`, which:

1. generates the `vm_interp.bin` blob (same aarch64-cross recipe as the Go CI),
2. runs `gomobile bind` to produce `app/libs/vmpmobile.aar`,
3. runs `./gradlew assembleDebug`, and
4. uploads the debug APK as a workflow artifact (`vmpacker-android-debug`).

To build locally you need the Android SDK (platforms 24 + 36, build-tools 36),
an NDK, and Go with gomobile. From the repo root:

```sh
# 1. generate the interpreter blob into mobile/vm_interp.bin (see the CI step)
# 2. produce the binding
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile bind -target=android/arm64,android/arm -androidapi 24 \
    -javapkg com.vmpacker -o android/app/libs/vmpmobile.aar ./mobile
# 3. build the APK
cd android && ./gradlew assembleDebug
```

> The native library ships arm64-v8a + armeabi-v7a only, so the app runs on
> ARM devices (not x86 emulators).
