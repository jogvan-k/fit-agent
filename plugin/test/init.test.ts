import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { fixture } from './helpers.js';
import { outputs } from '../src/schemas.js';
import { Bridge } from '../src/bridge.js';

for (const mode of ['full', 'dry-run', 'error']) test(`protected terminal init ${mode}`, async (t) => {
  const f = await fixture(t, false), output = join(f.root, 'result.json');
  if (mode === 'error') { Object.assign(f.state, { mode: 'error', command: 'init' }); await f.save(); }
  const python = `import os,pty,sys\npid,fd=pty.fork()\nif pid==0: os.execv(sys.argv[1],sys.argv[1:])\nwhile True:\n try:\n  if not os.read(fd,4096): break\n except OSError: break\n_,status=os.waitpid(pid,0)\nsys.exit(os.waitstatus_to_exitcode(status))`;
  const child = spawn('python3', ['-c', python, process.execPath, '--import', 'tsx', fileURLToPath(new URL('./init-flow.ts', import.meta.url)), f.binary, f.workspace, output, mode], { stdio: ['ignore', 'pipe', 'pipe'] });
  let stderr = ''; child.stderr.on('data', (data: Buffer) => { stderr += data.toString(); });
  const code = await new Promise((resolve, reject) => { child.on('close', resolve); child.on('error', reject); });
  assert.equal(code, 0, stderr);
  const results: unknown[] = JSON.parse(await readFile(output, 'utf8'));
  const init = outputs.init.parse(results[1]);
  if (mode === 'error') { assert(!init.ok); assert(!JSON.stringify(init).includes('private-canary')); return; }
  assert(init.ok); assert.equal(init.data.dryRun, mode === 'dry-run');
  if (mode === 'dry-run') assert.equal(init.data.filesCreated.length, 0);
  else {
    assert(init.data.filesCreated.includes('.fit-agent.toml'));
    assert(outputs.doctor.parse(results[2]).ok);
    assert(outputs.refresh.parse(results[3]).ok);
    assert(outputs.preview_workout_sync.parse(results[4]).ok);
    assert(outputs.sync_workouts.parse(results[5]).ok);
  }
  const initCall = (await f.calls()).find((c) => c.args[0] === 'init');
  assert(initCall); assert(!initCall.args.includes('--api-key')); assert(!initCall.args.includes('--non-interactive'));
});

test('init rejects credential arguments and headless mode provides private-terminal remediation', async (t) => {
  const f = await fixture(t, false), bridge = new Bridge({ binaryPath: f.binary });
  const args = outputs.init.parse(await bridge.call('init', { apiKey: 'private-canary' }, f.session));
  assert(!args.ok && args.error.layer === 'validation'); assert(!JSON.stringify(args).includes('private-canary'));
  // Tests normally run headlessly; force a fresh session without a controlling
  // terminal rather than inheriting the developer's terminal.
  const output = join(f.root, 'headless.json');
  const child = spawn(process.execPath, ['--import', 'tsx', fileURLToPath(new URL('./init-flow.ts', import.meta.url)), f.binary, f.workspace, output, 'headless'], { detached: true, stdio: 'ignore' });
  await new Promise<void>((resolve, reject) => { child.on('close', (code) => code === 0 ? resolve() : reject(new Error(`exit ${code}`))); child.on('error', reject); });
  const results: unknown[] = JSON.parse(await readFile(output, 'utf8'));
  const result = outputs.init.parse(results[1]); assert(!result.ok); assert.match(result.error.message, /private local terminal/);
});
