import test from 'node:test';
import assert from 'node:assert/strict';
import { writeFile, readFile, symlink, chmod } from 'node:fs/promises';
import { join } from 'node:path';
import { Bridge, PREVIEW_TTL_MS, fingerprint } from '../src/bridge.js';
import { configSchema, inputs, outputs } from '../src/schemas.js';
import { childEnvironment, discoverBinary, parseVersion, redact, Runner } from '../src/process.js';
import { inspectWorkspace } from '../src/workspace.js';
import { fixture } from './helpers.js';

const range = { from: '2026-04-01', to: '2026-04-07' };
test('strict configuration, calendar bounds, history and confirmation validation', () => {
  for (const value of [{ apiKey: 'no' }, { timeoutMs: 0 }, { timeoutMs: 600001 }, { profile: '--profile' }, { workspace: 'a\0b' }]) assert.equal(configSchema.safeParse(value).success, false);
  for (const value of [{}, { from: '2026-02-30', to: '2026-03-01' }, { from: range.to, to: range.from }, { from: '2026-01-01', to: '2026-04-01' }, { ...range, prune: true }]) assert.equal(inputs.preview_workout_sync.safeParse(value).success, false);
  assert(inputs.preview_workout_sync.safeParse({ from: '2026-01-01', to: '2026-03-31' }).success);
  assert.equal(inputs.refresh.parse({}).since, '90d');
  for (const since of ['0d', '367d', '90d; echo x', '2y', '-1d']) assert.equal(inputs.refresh.safeParse({ since }).success, false);
  assert.equal(inputs.sync_workouts.safeParse({ ...range, confirmationToken: 'a'.repeat(64), confirmed: false }).success, false);
});

test('binary discovery resolves symlinks and checks execution; strict stable versions', async (t) => {
  const f = await fixture(t);
  const link = join(f.root, 'binary-link'); await symlink(f.binary, link);
  assert.equal(await discoverBinary(link), f.binary);
  assert.equal(await discoverBinary(undefined, f.root), f.binary);
  await assert.rejects(discoverBinary('relative'), /Install/);
  await chmod(f.binary, 0o644); await assert.rejects(discoverBinary(f.binary), /Install/);
  for (const version of ['dev', '0.0.9', '0.1.0-beta.1', '0.1.0\nsecret']) assert.throws(() => parseVersion(version));
  assert.equal(parseVersion('v0.1.0\n'), '0.1.0');
});

test('environment and error redaction never forward credentials or endpoint overrides', () => {
  const env = childEnvironment({ HOME: '/home/test', FIT_AGENT_API_KEY: 'private-canary', OPENAI_API_KEY: 'private-canary', ICU_BASE_URL: 'http://bad', NODE_OPTIONS: '--import bad' });
  assert.deepEqual(env, { HOME: '/home/test' });
  assert(!redact('API_KEY=private-canary\nAuthorization: Basic abc123\npassword: hunter2').includes('private-canary'));
});

test('missing CLI reports install remediation; missing workspace doctor never creates files', async (t) => {
  const f = await fixture(t, false);
  const missing = outputs.doctor.parse(await new Bridge({ binaryPath: join(f.root, 'missing') }).call('doctor', {}, f.session));
  assert(!missing.ok); assert.match(missing.error.message, /brew tap jogvan-k\/tap/);
  const doctor = outputs.doctor.parse(await new Bridge({ binaryPath: f.binary }).call('doctor', {}, f.session));
  assert(doctor.ok && !doctor.data.ready);
  assert(!(await inspectWorkspace(f.workspace)).ready);
});

test('doctor/status/refresh success uses exact argv and returns only compact results', async (t) => {
  const f = await fixture(t), bridge = new Bridge({ binaryPath: f.binary, profile: 'test-profile' });
  const doctor = outputs.doctor.parse(await bridge.call('doctor', {}, f.session)); assert(doctor.ok && doctor.data.ready);
  const status = outputs.status.parse(await bridge.call('status', {}, f.session)); assert(status.ok && status.data.plannedWorkoutCount === 2);
  const refresh = outputs.refresh.parse(await bridge.call('refresh', {}, f.session)); assert(refresh.ok && refresh.data.cache.Added === 3);
  assert.deepEqual((await f.calls()).find((c) => c.args[0] === 'fetch')?.args, ['fetch', '--json', '--workspace', f.workspace, '--profile', 'test-profile', '--since', '90d']);
  assert((await f.calls()).every((c) => !c.secretEnvPresent));
});

test('feature gate rejects old protocol and missing required features before mutations', async (t) => {
  for (const config of [{ protocol: 2 }, { features: [] }, { version: 'dev' }]) {
    const f = await fixture(t); Object.assign(f.state, config); await f.save();
    const result = outputs.refresh.parse(await new Bridge({ binaryPath: f.binary }).call('refresh', {}, f.session));
    assert(!result.ok); assert(!(await f.calls()).some((c) => c.args[0] === 'fetch'));
  }
});

for (const tool of ['doctor', 'status', 'refresh', 'preview_workout_sync'] as const) {
  for (const mode of ['invalid', 'missing', 'unexpected', 'error', 'exit'] as const) {
    test(`${tool} rejects ${mode} CLI output without disclosing content`, async (t) => {
      const f = await fixture(t);
      Object.assign(f.state, { command: { doctor: 'doctor', status: 'status', refresh: 'fetch', preview_workout_sync: 'sync-workouts' }[tool], mode }); await f.save();
      const result = outputs[tool].parse(await new Bridge({ binaryPath: f.binary }).call(tool, tool === 'preview_workout_sync' ? range : {}, f.session));
      assert(!result.ok); assert(!JSON.stringify(result).includes('private-canary'));
    });
  }
}

test('preview and confirmed sync bind the range and plan, re-preview, and return created event IDs', async (t) => {
  const f = await fixture(t), bridge = new Bridge({ binaryPath: f.binary });
  const preview = outputs.preview_workout_sync.parse(await bridge.call('preview_workout_sync', range, f.session));
  assert(preview.ok && preview.data.confirmationToken); assert.equal(preview.data.counts.create, 1);
  const args = { ...range, confirmed: true, confirmationToken: preview.data.confirmationToken };
  const sync = outputs.sync_workouts.parse(await bridge.call('sync_workouts', args, f.session));
  assert(sync.ok); assert.equal(sync.data.actions[0]?.eventId, 123);
  const calls = (await f.calls()).filter((c) => c.args[0] === 'sync-workouts');
  assert.equal(calls.length, 3); assert(calls[0]?.args.includes('--dry-run')); assert(calls[1]?.args.includes('--dry-run'));
  assert(calls[2]?.args.includes('--expect-preview')); assert(!calls[2]?.args.includes('--dry-run'));
  assert.equal(outputs.sync_workouts.parse(await bridge.call('sync_workouts', args, f.session)).ok, false);
});

for (const change of ['file', 'remote', 'version', 'expired', 'range', 'session', 'restart', 'push-race'] as const) {
  test(`confirmed sync rejects ${change} mismatch without calendar writes`, async (t) => {
    const f = await fixture(t); let now = Date.now(); let bridge = new Bridge({ binaryPath: f.binary }, () => now);
    const preview = outputs.preview_workout_sync.parse(await bridge.call('preview_workout_sync', range, f.session)); assert(preview.ok);
    if (change === 'file') await writeFile(join(f.workspace, 'fit-agent/planned-workouts/2026-04-01.md'), 'changed', { mode: 0o644 });
    if (change === 'remote') f.state.remoteRevision = 1;
    if (change === 'version') f.state.version = '0.2.0';
    if (change === 'expired') now += PREVIEW_TTL_MS;
    if (change === 'restart') bridge = new Bridge({ binaryPath: f.binary });
    if (change === 'push-race') f.state.rejectPush = true;
    await f.save();
    const args = { ...range, ...(change === 'range' ? { to: '2026-04-08' } : {}), confirmed: true, confirmationToken: preview.data.confirmationToken };
    const result = outputs.sync_workouts.parse(await bridge.call('sync_workouts', args, { ...f.session, scope: change === 'session' ? 'other' : f.session.scope }));
    assert(!result.ok); assert.equal(result.error.layer, 'sync conflict');
    await assert.rejects(readFile(join(f.root, 'writes.jsonl')), { code: 'ENOENT' });
  });
}

test('conflicts do not issue approval tokens; unexpected preview range/dryRun/action fails closed', async (t) => {
  for (const change of [{ conflict: true }, { badRange: true }, { badDryRun: true }, { actions: [{ kind: 'approve', date: range.from, eventId: 0 }] }]) {
    const f = await fixture(t); Object.assign(f.state, change); await f.save();
    const result = outputs.preview_workout_sync.parse(await new Bridge({ binaryPath: f.binary }).call('preview_workout_sync', range, f.session));
    if ('conflict' in change) { assert(result.ok); assert.equal(result.data.confirmationToken, null); }
    else assert(!result.ok);
  }
});

test('workspace symlink escapes and unsafe writable modes fail without command execution', async (t) => {
  const f = await fixture(t);
  // Standard 664/775 workspace permissions are safe when the writable group is
  // the current user's primary group.
  await chmod(join(f.workspace, 'fit-agent/ATHLETE-PROFILE.md'), 0o664);
  await chmod(join(f.workspace, 'fit-agent/planned-workouts'), 0o775);
  assert((await inspectWorkspace(f.workspace)).ready);
  await symlink(f.root, join(f.workspace, 'fit-agent/.cache/escape'));
  await assert.rejects(inspectWorkspace(f.workspace), /symlinks/);
});

test('profile workspace mismatch is refused before fetch', async (t) => {
  const f = await fixture(t); f.state.wrongWorkspace = f.root; await f.save();
  const result = outputs.refresh.parse(await new Bridge({ binaryPath: f.binary }).call('refresh', {}, f.session));
  assert(!result.ok && result.error.layer === 'workspace'); assert(!(await f.calls()).some((c) => c.args[0] === 'fetch'));
});

test('timeout, oversized output and cancellation terminate the spawned CLI', async (t) => {
  for (const mode of ['hang', 'flood']) {
    const f = await fixture(t); Object.assign(f.state, { command: 'doctor', mode }); await f.save();
    const runner = new Runner(configSchema.parse({ timeoutMs: 1000 }));
    await assert.rejects(runner.run(f.binary, ['doctor'], f.workspace), /timed out|safe limit/);
  }
  const f = await fixture(t), signal = AbortSignal.abort();
  await assert.rejects(new Runner(configSchema.parse({})).run(f.binary, [], undefined, signal), /cancelled/);
});

test('fingerprint deterministic and sensitive to range, version, workspace, profile and CLI content digest', () => {
  const original = fingerprint(range, '0.1.0', '/ws', 'default', 'abc');
  assert.equal(original, fingerprint(range, '0.1.0', '/ws', 'default', 'abc'));
  for (const other of [fingerprint({ ...range, to: '2026-04-08' }, '0.1.0', '/ws', 'default', 'abc'), fingerprint(range, '0.2.0', '/ws', 'default', 'abc'), fingerprint(range, '0.1.0', '/other', 'default', 'abc'), fingerprint(range, '0.1.0', '/ws', 'other', 'abc'), fingerprint(range, '0.1.0', '/ws', 'default', 'def')]) assert.notEqual(original, other);
});
