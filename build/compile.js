import { fileURLToPath } from 'node:url';
import { mkdir, mkdtemp, rename, rm } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { buildPage } from '../client/index.js';
import { resolveUPX } from './upx.js';

const root = new URL('../', import.meta.url);
const serverDirectory = fileURLToPath(new URL('server/', root));
const hostOS = { linux: 'linux', darwin: 'darwin', win32: 'windows' }[process.platform];
const hostArch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
export const targets = ['linux', 'windows', 'darwin'].flatMap(os => ['amd64', 'arm64'].map(arch => ({ os, arch })));
async function run(args, message, { cwd = serverDirectory, env = {} } = {}) {
  const command = Bun.spawn(args, { cwd, env: { ...process.env, CGO_ENABLED: '0', GOOS: hostOS, GOARCH: hostArch, ...env }, stdout: 'inherit', stderr: 'inherit' });
  if (await command.exited !== 0) throw new Error(message);
}
async function findUPX() {
  return resolveUPX({ install: cached => run(['go', 'run', './tools/upx', cached], 'UPX installation failed') });
}
async function resolveVersion(version) {
  version ||= process.env.RELEASE_VERSION || (await Bun.file(new URL('package.json', root)).json()).version;
  version = version.replace(/^v/, '');
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(version)) throw new Error(`Invalid version: ${version}`);
  return version;
}
export async function compile({ compress = true, os = hostOS, arch = hostArch, output, frontend = true, version } = {}) {
  if (!targets.some(target => target.os === os && target.arch === arch)) throw new Error(`Unsupported target: ${os}/${arch}`);
  version = await resolveVersion(version);
  if (frontend) await buildPage();
  const filename = os === 'windows' ? 'yadirect.exe' : 'yadirect';
  const binary = resolve(output || fileURLToPath(new URL(filename, root)));
  // Modern macOS binaries keep Go's linker signature; UPX would invalidate it.
  const packed = compress && os !== 'darwin';
  const upx = packed ? await findUPX() : null;
  const temporaryDirectory = await mkdtemp(fileURLToPath(new URL('.yadirect-build-', root)));
  const temporaryBinary = join(temporaryDirectory, filename);
  const resource = join(serverDirectory, `rsrc_windows_${arch}.syso`);
  let ownsResource = false;
  try {
    if (os === 'windows') {
      if (await Bun.file(resource).exists()) throw new Error(`Resource file already exists: ${resource}`);
      ownsResource = true;
      await run(['go', 'run', './tools/winres', fileURLToPath(new URL('client/favicon.ico', root)), version, arch, resource], 'Windows resources failed');
    }
    await run(['go', 'build', '-trimpath', `-ldflags=-s -w -X main.version=${version}`, '-o', temporaryBinary, '.'], 'Go build failed', { env: { GOOS: os, GOARCH: arch } });
    if (packed) {
      await run([upx, '--best', '--lzma', '--no-progress', ...(os === 'windows' ? ['--compress-icons=0', '--compress-resources=0'] : []), temporaryBinary], 'UPX compression failed');
      await run([upx, '-t', temporaryBinary], 'UPX integrity check failed');
    }
    await mkdir(dirname(binary), { recursive: true });
    await rename(temporaryBinary, binary);
    console.log(`Built ${binary} (${os}/${arch}${packed ? ', UPX' : ''})`);
    return binary;
  } finally {
    if (ownsResource) await rm(resource, { force: true });
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
}
export async function buildRelease({ selectedTargets = targets, compress = true, version, directory = fileURLToPath(new URL('release/', root)) } = {}) {
  directory = resolve(directory);
  version = await resolveVersion(version);
  await buildPage();
  await mkdir(directory, { recursive: true });
  for (const filename of ['SHA256SUMS', ...targets.map(target => `SHA256SUMS-${target.os}-${target.arch}`)]) {
    await rm(join(directory, filename), { force: true });
  }
  for (const target of selectedTargets) {
    const name = `yadirect-${target.os}-${target.arch}`;
    const filename = target.os === 'windows' ? 'yadirect.exe' : 'yadirect';
    const archiveName = `yadirect-${version}-${target.os}-${target.arch}.${target.os === 'windows' ? 'zip' : 'tar'}`;
    const staging = await mkdtemp(join(directory, '.archive-'));
    try {
      await compile({ ...target, version, compress, frontend: false, output: join(staging, filename) });
      const temporaryArchive = join(staging, archiveName);
      if (target.os === 'windows') {
        await run(['zip', '-0', '-q', '-X', temporaryArchive, filename], 'ZIP archive failed', { cwd: staging });
      } else {
        await run(['tar', '-cf', temporaryArchive, '-C', staging, filename], 'TAR archive failed');
      }
      await rename(temporaryArchive, join(directory, archiveName));
      for (const legacy of [name, `${name}.exe`, `${name}.zip`, `${name}.tar.gz`]) {
        await rm(join(directory, legacy), { force: true });
      }
      console.log(`Archived ${archiveName} (store only)`);
    } finally {
      await rm(staging, { recursive: true, force: true });
    }
  }
}
if (import.meta.main) {
  const { values } = parseArgs({ options: { all: { type: 'boolean' }, os: { type: 'string' }, arch: { type: 'string' }, output: { type: 'string' }, version: { type: 'string' }, release: { type: 'boolean' }, 'no-compress': { type: 'boolean' } }, strict: true });
  if (values.all && (values.os || values.arch || values.output)) throw new Error('--all cannot be combined with --os, --arch or --output');
  const options = { os: values.os || hostOS, arch: values.arch || hostArch, output: values.output, version: values.version, compress: !values['no-compress'] };
  if (values.all || values.release) {
    if (values.output) throw new Error('--release cannot be combined with --output');
    await buildRelease({ version: options.version, compress: options.compress, selectedTargets: values.all ? targets : [{ os: options.os, arch: options.arch }] });
  } else await compile(options);
}
