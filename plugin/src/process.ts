import { spawn } from 'node:child_process';
import { constants } from 'node:fs';
import { access, open, realpath, stat } from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import { delimiter, isAbsolute, join } from 'node:path';
import { isatty } from 'node:tty';
import type { Config, Layer } from './schemas.js';

export const installHint = 'brew tap jogvan-k/tap\nbrew install fit-agent';
export class Failure extends Error {
  constructor(readonly layer: Layer, message: string, readonly diagnostics?: { code: number | null; stderr: string }) { super(message); }
}

// Never forward agent-provider keys, FIT_AGENT_API_KEY, endpoint overrides,
// NODE_OPTIONS, shell startup hooks, or arbitrary gateway environment variables.
export function childEnvironment(env: NodeJS.ProcessEnv = process.env): NodeJS.ProcessEnv {
  const allowed = ['PATH', 'HOME', 'USER', 'LOGNAME', 'LANG', 'LC_ALL', 'TERM', 'TMPDIR',
    'XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_RUNTIME_DIR', 'DBUS_SESSION_BUS_ADDRESS',
    'DISPLAY', 'WAYLAND_DISPLAY', 'FIT_AGENT_PROFILE'];
  return Object.fromEntries(allowed.filter((key) => env[key] !== undefined).map((key) => [key, env[key]]));
}

export function redact(text: string): string {
  return text.replace(/(?:authorization|api[_ -]?key|token|secret|password)\s*[:=]?\s*[^\r\n]*/gi, '[redacted]')
    .replace(/Basic\s+[A-Za-z0-9+/=]+/gi, '[redacted]').slice(0, 4096);
}

export async function discoverBinary(override?: string, path = process.env.PATH ?? ''): Promise<string> {
  if (process.platform !== 'linux' && process.platform !== 'darwin') throw new Failure('binary', 'Plugin v1 supports macOS and Linux.');
  const candidates = override
    ? (isAbsolute(override) ? [override] : [])
    : path.split(delimiter).filter(isAbsolute).map((dir) => join(dir, 'fit-agent'));
  for (const candidate of candidates) {
    try {
      const resolved = await realpath(candidate);
      if (!(await stat(resolved)).isFile()) continue;
      await access(resolved, constants.X_OK);
      return resolved;
    } catch { /* Try the next PATH entry. */ }
  }
  throw new Failure('binary', `Install the fit-agent CLI, or configure an absolute binaryPath:\n${installHint}`);
}

export function parseVersion(output: string): string {
  const match = /^v?(\d{1,6})\.(\d{1,6})\.(\d{1,6})\s*$/.exec(output);
  if (!match || (Number(match[1]) === 0 && Number(match[2]) === 0)) {
    throw new Failure('binary', 'fit-agent >=0.1.0 (stable release) required. Run brew upgrade fit-agent.');
  }
  return `${match[1]}.${match[2]}.${match[3]}`;
}

export type ProcessResult = { code: number | null; stdout: string; stderr: string };
export class Runner {
  constructor(readonly config: Config) {}

  async run(binary: string, args: string[], cwd?: string, signal?: AbortSignal, terminal = false): Promise<ProcessResult> {
    if (signal?.aborted) throw new Failure('validation', 'Command cancelled.');
    let tty: FileHandle | undefined;
    if (terminal) {
      try {
        tty = await open('/dev/tty', constants.O_RDWR);
        if (!isatty(tty.fd)) throw new Error('not a terminal');
      } catch {
        await tty?.close();
        throw new Failure('profile/config', 'Interactive setup needs a local controlling terminal. Run fit-agent init --workspace <configured-workspace> there; never paste credentials in chat.');
      }
    }
    try {
      return await new Promise<ProcessResult>((resolve, reject) => {
        const child = spawn(binary, args, { cwd, env: childEnvironment(), shell: false,
          detached: !terminal, stdio: tty ? [tty.fd, tty.fd, tty.fd] : ['ignore', 'pipe', 'pipe'] });
        let stdout = '', stderr = '', bytes = 0;
        let stopped: Failure | undefined;
        let escalation: NodeJS.Timeout | undefined;
        const kill = (signal: NodeJS.Signals) => {
          try { if (!terminal && child.pid) process.kill(-child.pid, signal); else child.kill(signal); } catch { /* Already exited. */ }
        };
        const stop = (message: string) => {
          if (stopped) return;
          stopped = new Failure('network', message);
          kill('SIGTERM');
          escalation = setTimeout(() => kill('SIGKILL'), 250);
        };
        const timer = setTimeout(() => stop('CLI timed out. Check local CLI state before retrying a write.'), this.config.timeoutMs);
        const abort = () => stop('Command cancelled. Check local CLI state before retrying a write.');
        signal?.addEventListener('abort', abort, { once: true });
        const capture = (chunk: Buffer, stream: 'stdout' | 'stderr') => {
          bytes += chunk.length;
          if (bytes > 256 * 1024) { stop('CLI output exceeded the safe limit. Inspect it locally.'); return; }
          if (stream === 'stdout') stdout += chunk.toString('utf8'); else stderr += chunk.toString('utf8');
        };
        child.stdout?.on('data', (chunk: Buffer) => capture(chunk, 'stdout'));
        child.stderr?.on('data', (chunk: Buffer) => capture(chunk, 'stderr'));
        const cleanup = () => { clearTimeout(timer); if (escalation) clearTimeout(escalation); signal?.removeEventListener('abort', abort); };
        child.on('error', () => { cleanup(); reject(new Failure('binary', 'Cannot execute fit-agent. Check binaryPath and executable permissions.')); });
        child.on('close', (code) => { if (stopped) kill('SIGKILL'); cleanup(); if (stopped) reject(stopped); else resolve({ code, stdout, stderr: redact(stderr) }); });
      });
    } finally { await tty?.close(); }
  }
}
