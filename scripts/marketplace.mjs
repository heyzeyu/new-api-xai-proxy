import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { meta } from '../plugin.js';

const indexPath = new URL('../marketplace.json', import.meta.url);
const source = readFileSync(new URL('../plugin.js', import.meta.url));
const index = {
  indexVersion: 1,
  name: 'new-api-xai-proxy',
  plugins: [{
    key: meta.key, name: meta.name, icon: meta.icon, website: meta.website,
    description: meta.description, models: meta.models, protocols: meta.protocols,
    latest: meta.version,
    versions: [{
      version: meta.version,
      path: `https://raw.githubusercontent.com/heyzeyu/new-api-xai-proxy/v${meta.version}/plugin.js`,
      sha256: createHash('sha256').update(source).digest('hex'),
      minApiVersion: meta.apiVersion, kind: 'task', baseUrl: meta.baseUrl,
      auth: meta.auth, allowedHosts: meta.allowedHosts,
    }],
  }],
};
const content = JSON.stringify(index, null, 2) + '\n';
if (process.argv[2] === '--check') {
  if (readFileSync(indexPath, 'utf8') !== content) throw new Error('marketplace.json is stale; run npm run marketplace');
} else writeFileSync(indexPath, content);
