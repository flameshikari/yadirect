import { createReadStream, createWriteStream } from 'node:fs';
import { pipeline } from 'node:stream/promises';
import { createGzip } from 'node:zlib';

export async function gzipArchive(source, destination, level) {
  await pipeline(createReadStream(source), createGzip({ level }), createWriteStream(destination));
}
