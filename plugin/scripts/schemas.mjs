import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { z } from 'zod';
import { configSchema, inputs, outputs, cliEnvelope, cliSyncSchema, cliDoctorSchema, cliFetchSchema, infoSchema, statusData } from '../dist/schemas.js';

await mkdir('schemas', { recursive: true });
const emit = (name, schema, io = 'output') => writeFile(`schemas/${name}.json`, JSON.stringify(z.toJSONSchema(schema, { io }), null, 2) + '\n');
await emit('config', configSchema, 'input');
for (const tool of Object.keys(inputs)) {
  await emit(`${tool}.input`, inputs[tool], 'input');
  await emit(`${tool}.result`, outputs[tool]);
}
for (const [name, schema] of Object.entries({ sync: cliSyncSchema, doctor: cliDoctorSchema, fetch: cliFetchSchema, info: infoSchema, status: statusData })) {
  await emit(`cli-${name}.result`, cliEnvelope(schema));
}
const pkg = JSON.parse(await readFile('package.json', 'utf8'));
const manifest = {
  id: 'fit-agent', name: 'FitAgent', version: pkg.version,
  description: 'Typed CLI-backed fitness workspace and confirmed workout-calendar synchronization',
  categories: ['scheduling'],
  contracts: { tools: Object.keys(inputs).map((tool) => `fit_agent_${tool}`) },
  activation: { onStartup: true },
  toolMetadata: Object.fromEntries(Object.keys(inputs).map((tool) => [`fit_agent_${tool}`, { optional: tool !== 'doctor' && tool !== 'status' }])),
  configSchema: z.toJSONSchema(configSchema, { io: 'input' }),
  uiHints: { binaryPath: { label: 'Installed fit-agent binary (absolute path)' }, workspace: { label: 'FitAgent workspace (defaults to active agent workspace)' },
    profile: { label: 'CLI profile (optional)' }, timeoutMs: { label: 'Command timeout in milliseconds' }, diagnosticVerbosity: { label: 'Diagnostic verbosity' } },
};
await writeFile('openclaw.plugin.json', JSON.stringify(manifest, null, 2) + '\n');
