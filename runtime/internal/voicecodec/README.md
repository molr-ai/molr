# Embedded SILK decoder

`silk.wasm` contains the SILK SDK decoder and `source/decode.c`. Runtime runs it with wazero, a 32 MiB guest memory cap, a five-second deadline and no imported host functions. There is no filesystem, network, process or environment access. Each decode has a fresh runtime and guest instance. The process retains compiled code for the embedded decoder in an in-memory compilation cache; audio input, guest memory and returned WAV bytes are private to each call. The Go Runtime remains `CGO_ENABLED=0`; deployment needs neither Node.js nor a native SILK decoder.

Input is bounded to 4 MiB; output to two minutes of 24 kHz mono PCM. The wrapper validates packet boundaries, frame count and output capacity before wrapping PCM as WAV. Invalid/oversized streams fail; the Weixin adapter retains their original audio attachment. Automatic transcription is not provided.

## Rebuild

Sources:

- SILK SDK via https://github.com/kn007/silk-v3-decoder at commit `507be6bca8ce1fb977a061481f1d79e8c610e309`.
- Source archive SHA-256: `d9ee6a2c5ee411f29f3ef31b4a43c238d8aaf942640792012db1ee348b147a38`.
- Official WASI SDK 34.0, `wasi-sdk-34.0-x86_64-linux.tar.gz`, SHA-256 `b761e3a0721dbae9c09a0059e5fdb2bf917d1b4a8a7b430fb3b5aafb0984b2c4`.

Run `python3 source/build.py <wasi-sdk-root> <silk-v3-decoder-root>`, then run the `voicecodec` tests. The build script does not fetch dependencies. `LICENSE.silk` reproduces the SDK license and is included in release archives.

## Test vector

`testdata/tone.silk` is a generated 0.2-second, 440 Hz sine wave, encoded at 24 kHz with `silk-wasm@3.7.1`. `pcm.sha256` was computed by the independently built SDK `silk/decoder` command with `-Fs_API 24000`, not by this wrapper. Tests compare the full decoded PCM and validate the WebAssembly import surface, malformed input and cancellation. The codec source matches the SDK bundled with the reference plugin's silk-wasm version; its JavaScript glue is not used by Runtime.
