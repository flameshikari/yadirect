import { fileURLToPath } from 'node:url';

export const UPX_VERSION = '5.3.0';
const minimumVersion = UPX_VERSION.split('.').map(Number);

export async function resolveUPX({
  override = process.env.UPX_BINARY,
  cached = fileURLToPath(new URL(`../node_modules/.cache/upx/${UPX_VERSION}/${process.platform}-${process.arch}/upx`, import.meta.url)),
  install,
} = {}) {
  // Runner images may contain an older UPX. Use the pinned installer unless explicitly overridden.
  const executable = override || cached;
  if (!override && !(await Bun.file(cached).exists())) await install(cached);
  const command = Bun.spawn([executable, '--version'], { stdout: 'pipe', stderr: 'pipe' });
  const [stdout, stderr, status] = await Promise.all([
    new Response(command.stdout).text(), new Response(command.stderr).text(), command.exited,
  ]);
  const match = `${stdout}\n${stderr}`.match(/^upx\s+(\d+)\.(\d+)\.(\d+)(?:\s|$)/im);
  const actual = match?.slice(1).map(Number);
  const differing = actual?.findIndex((part, index) => part !== minimumVersion[index]);
  if (status !== 0 || !actual || (differing >= 0 && actual[differing] < minimumVersion[differing])) {
    throw new Error(`UPX ${UPX_VERSION} or newer is required; found ${actual?.join('.') ?? 'unknown version'} at ${executable}`);
  }
  console.log(`Using UPX ${actual.join('.')}: ${executable}`);
  return executable;
}
