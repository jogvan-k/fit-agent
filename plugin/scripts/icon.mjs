// Reproducible original FitAgent pulse artwork, MIT licensed.
import { mkdir, writeFile } from 'node:fs/promises';
import { deflateSync } from 'node:zlib';
const crc = (bytes) => {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let i = 0; i < 8; i++) value = (value >>> 1) ^ ((value & 1) ? 0xedb88320 : 0);
  }
  return (value ^ 0xffffffff) >>> 0;
};
const chunk = (type, bytes) => {
  const body = Buffer.concat([Buffer.from(type), bytes]);
  const length = Buffer.alloc(4), checksum = Buffer.alloc(4);
  length.writeUInt32BE(bytes.length); checksum.writeUInt32BE(crc(body));
  return Buffer.concat([length, body, checksum]);
};
const pixels = Buffer.alloc(64 * (1 + 64 * 3));
for (let y = 0; y < 64; y++) for (let x = 0; x < 64; x++) {
  const target = x < 18 ? 32 : x < 26 ? 32 - (x - 18) * 2 : x < 38 ? 16 + (x - 26) * 2.6 : x < 46 ? 47 - (x - 38) * 1.875 : 32;
  const color = Math.abs(y - target) < 2.5 && x > 7 && x < 57 ? [214, 255, 103] : [14, 74, 74];
  color.forEach((v, i) => { pixels[y * 193 + 1 + x * 3 + i] = v; });
}
const header = Buffer.alloc(13); header.writeUInt32BE(64, 0); header.writeUInt32BE(64, 4); header[8] = 8; header[9] = 2;
await mkdir('assets', { recursive: true });
await writeFile('assets/icon.png', Buffer.concat([Buffer.from('89504e470d0a1a0a', 'hex'), chunk('IHDR', header), chunk('IDAT', deflateSync(pixels)), chunk('IEND', Buffer.alloc(0))]));
