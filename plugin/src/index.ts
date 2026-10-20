import { definePluginEntry } from 'openclaw/plugin-sdk/plugin-entry';
import type { AnyAgentTool } from 'openclaw/plugin-sdk/plugin-entry';
import { z } from 'zod';
import { Bridge } from './bridge.js';
import { inputs, outputs } from './schemas.js';
import type { Tool } from './schemas.js';

export const descriptions: Record<Tool, string> = {
  doctor: 'Read-only local diagnosis: installed CLI, profile/credential availability and workspace ownership/shape. No remote reads, local writes, or credential output.',
  init: 'Interactive local writes and remote credential validation: run CLI setup on the host controlling terminal. Headless hosts return terminal instructions. Never request API keys in chat or pass them as arguments. Supports dryRun.',
  refresh: 'Remote reads + local machine-owned writes: fetch training data via fit-agent (default 90d, maximum 366d). Returns counts only. No remote writes.',
  status: 'Read-only local workspace summary: data recency, planned-workout count/upcoming dates and coaching file availability. No coaching advice or activity content.',
  preview_workout_sync: 'Remote reads, no remote or local writes: preview workout calendar changes for an explicit bounded date range (maximum 90 inclusive days). Present the diff and conflicts to the user before requesting confirmation. Returns a single-use token valid for 10 minutes.',
  sync_workouts: 'REMOTE WRITES + remote reads + local writes: synchronize exactly the previewed date range. Set confirmed:true and the matching confirmationToken ONLY after presenting that preview and receiving explicit user confirmation, or honoring an existing explicit standing preference to push after a successful preview. Stale plans and conflicts are refused. Never retry a write without a new preview.',
};

export default definePluginEntry({
  id: 'fit-agent', name: 'FitAgent', description: 'Typed, guarded tools for the installed fit-agent Go CLI',
  register(api) {
    const bridge = new Bridge(api.pluginConfig);
    for (const tool of Object.keys(inputs) as Tool[]) {
      const name = `fit_agent_${tool}`;
      api.registerTool((ctx) => {
        // Never bypass an active sandbox by spawning on the gateway host.
        if (ctx.sandboxed) return null;
        return {
          name, label: name, description: descriptions[tool],
          parameters: z.toJSONSchema(inputs[tool], { io: 'input' }) as AnyAgentTool['parameters'],
          outputSchema: z.toJSONSchema(outputs[tool]),
          async execute(_id: string, params: unknown, signal?: AbortSignal) {
            ctx.assertInvocationCurrent?.();
            const details = (!ctx.sessionId && tool === 'sync_workouts')
              ? { ok: false, error: { layer: 'sync conflict', message: 'A trusted OpenClaw session ID is required for confirmed sync.' } }
              : await bridge.call(tool, params, { workspace: ctx.workspaceDir,
                scope: JSON.stringify([ctx.agentId, ctx.sessionKey, ctx.sessionId, ctx.requesterSenderId]), assertCurrent: ctx.assertInvocationCurrent }, signal);
            if (bridge.config.diagnosticVerbosity === 'normal') {
              const result = outputs[tool].parse(details);
              api.logger.info(`${name}: ${result.ok ? 'success' : result.error.layer}`);
            }
            const text = JSON.stringify(details);
            if (Buffer.byteLength(text) > 64 * 1024) {
              const failure = { ok: false, error: { layer: 'validation', message: 'FitAgent result exceeds 64 KiB. Inspect the CLI locally; preview again before any write.' } };
              return { content: [{ type: 'text' as const, text: JSON.stringify(failure) }], details: failure };
            }
            return { content: [{ type: 'text' as const, text }], details };
          },
        };
      }, { name, optional: tool !== 'doctor' && tool !== 'status' });
    }
  },
});
