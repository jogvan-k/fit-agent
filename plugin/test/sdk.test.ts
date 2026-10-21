import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import type { OpenClawPluginApi, OpenClawPluginToolFactory } from 'openclaw/plugin-sdk/plugin-entry';
import plugin from '../src/index.js';
import { fixture } from './helpers.js';
import { outputs } from '../src/schemas.js';

test('minimum pinned real OpenClaw SDK registers six tools with schemas and session isolation', async (t) => {
  const f = await fixture(t);
  const registered: { factory: OpenClawPluginToolFactory; optional: boolean }[] = [];
  const api = {
    pluginConfig: { binaryPath: f.binary },
    registerTool: (factory: OpenClawPluginToolFactory, opts?: { optional?: boolean }) => registered.push({ factory, optional: opts?.optional ?? false }),
  } as unknown as OpenClawPluginApi;
  await plugin.register?.(api);
  assert.equal(registered.length, 6);
  const manifest = JSON.parse(await readFile(new URL('../openclaw.plugin.json', import.meta.url), 'utf8'));
  const names = [];
  for (const { factory, optional } of registered) {
    assert.equal(factory({ sandboxed: true }), null);
    const tool = factory({ workspaceDir: f.workspace, sessionId: 'session', agentId: 'agent' });
    assert(tool && !Array.isArray(tool));
    names.push(tool.name); assert(tool.parameters);
    assert.equal(optional, tool.name !== 'fit_agent_doctor' && tool.name !== 'fit_agent_status');
    if (tool.name === 'fit_agent_doctor') {
      const result = await tool.execute('test-id', {}); assert(outputs.doctor.parse(result.details).ok);
    }
  }
  assert.deepEqual(names, manifest.contracts.tools);
});
