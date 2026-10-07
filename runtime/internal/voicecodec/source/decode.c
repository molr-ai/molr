#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "SKP_Silk_SDK_API.h"

// Fixed 24 kHz, mono signed 16-bit PCM. Caller supplies bounded WASM buffers.
int decode_silk(const uint8_t *data, int size, uint8_t *output, int capacity) {
  int offset = size > 0 && data[0] == 2 ? 1 : 0;
  if (size - offset < 9 || memcmp(data + offset, "#!SILK_V3", 9)) return -1;
  offset += 9;
  SKP_int state_size = 0;
  if (SKP_Silk_SDK_Get_Decoder_Size(&state_size) || state_size <= 0) return -1;
  void *state = malloc(state_size);
  if (!state) return -1;
  if (SKP_Silk_SDK_InitDecoder(state)) { free(state); return -1; }
  SKP_SILK_SDK_DecControlStruct control = {0};
  control.API_sampleRate = 24000;
  int written = 0;
  while (offset < size) {
    if (size - offset < 2) goto invalid;
    int length = data[offset] | (data[offset + 1] << 8);
    offset += 2;
    if (length == 65535 && offset == size) break;
    if (length <= 0 || length > 5120 || length > size - offset) goto invalid;
    int frames = 0;
    do {
      SKP_int16 pcm[4800], count = 0;
      if (SKP_Silk_SDK_Decode(state, &control, 0, data + offset, length, pcm, &count)) goto invalid;
      if (count <= 0 || count > 4800 || written > capacity - count * 2) goto invalid;
      memcpy(output + written, pcm, count * 2);
      written += count * 2;
      if (++frames > SILK_MAX_FRAMES_PER_PACKET) goto invalid;
    } while (control.moreInternalDecoderFrames);
    offset += length;
  }
  free(state);
  return written;
invalid:
  free(state);
  return -1;
}
